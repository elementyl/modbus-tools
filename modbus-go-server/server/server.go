// ===== C:\Projects\modbus-tools\modbus-go-server\server\server.go =====
package server

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"modbus-tools/modbus-go-server/config"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

// Command structs
type SetBitCmd struct{ Addr uint16; Bit uint; Val bool }
type WriteEngCmd struct{ Addr uint16; EngVal float64 }
type WriteRawCmd struct{ Addr uint16; RawVal uint16 }

type Server struct {
	mu           sync.Mutex
	datastore    map[uint16]uint16
	log          *log.Logger
	port         serial.Port
	CommandChan  chan interface{}
	shutdownChan chan struct{}
	heartbeatOn  bool
}

func NewServer(logger *log.Logger) *Server {
	s := &Server{
		datastore:    make(map[uint16]uint16),
		log:          logger,
		CommandChan:  make(chan interface{}),
		shutdownChan: make(chan struct{}),
		heartbeatOn:  false,
	}
	for _, reg := range config.REG_MAP {
		s.datastore[reg.Address] = 0
	}
	return s
}

func (s *Server) GetDatastoreSnapshot() map[uint16]uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := make(map[uint16]uint16)
	for k, v := range s.datastore {
		snapshot[k] = v
	}
	return snapshot
}

func (s *Server) SetHeartbeat(enable bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeatOn = enable
}

func (s *Server) RunCommandProcessor() {
	s.log.Println("Command processor goroutine started.")
	for {
		select {
		case cmd := <-s.CommandChan:
			s.mu.Lock()
			switch c := cmd.(type) {
			case SetBitCmd:
				if c.Val {
					s.datastore[c.Addr] |= (1 << c.Bit)
				} else {
					s.datastore[c.Addr] &= ^(1 << c.Bit)
				}
			case WriteEngCmd:
				regDef := config.GetRegisterDefinition(c.Addr)
				if regDef != nil && regDef.Type == "analog" {
					if regDef.Scaling != nil {
						s.datastore[c.Addr] = config.UnscaleValue(c.EngVal, regDef.Scaling)
					} else {
						s.datastore[c.Addr] = uint16(c.EngVal)
					}
				}
			case WriteRawCmd:
				s.datastore[c.Addr] = c.RawVal
			}
			s.mu.Unlock()
		case <-s.shutdownChan:
			s.log.Println("Command processor shutting down.")
			return
		}
	}
}

func (s *Server) HeartbeatLoop() {
	s.log.Println("Heartbeat goroutine started.")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.mu.Lock()
			if s.heartbeatOn {
				currentVal, ok := s.datastore[41009]
				if ok {
					s.datastore[41009] = currentVal + 1
				}
			}
			s.mu.Unlock()
		case <-s.shutdownChan:
			s.log.Println("Heartbeat goroutine shutting down.")
			return
		}
	}
}

func (s *Server) RunScenario(filePath string) {
	s.log.Printf("SCENARIO: Starting script '%s'", filePath)
	file, err := os.Open(filePath)
	if err != nil {
		s.log.Printf("SCENARIO ERROR: Could not open file: %v", err)
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		command := strings.ToUpper(parts[0])
		args := parts[1:]
		s.log.Printf("SCENARIO: Executing line %d: %s", lineNumber, line)
		switch command {
		case "WAIT":
			duration, _ := strconv.ParseFloat(args[0], 64)
			time.Sleep(time.Duration(duration * float64(time.Second)))
		case "HEARTBEAT":
			s.SetHeartbeat(len(args) > 0 && strings.ToUpper(args[0]) == "ON")
		case "SET":
			pointName := strings.Trim(strings.Join(args, " "), "\"")
			addr, bit, found := config.FindPointByName(pointName)
			if found {
				s.CommandChan <- SetBitCmd{Addr: addr, Bit: bit, Val: true}
			}
		case "CLEAR":
			pointName := strings.Trim(strings.Join(args, " "), "\"")
			addr, bit, found := config.FindPointByName(pointName)
			if found {
				s.CommandChan <- SetBitCmd{Addr: addr, Bit: bit, Val: false}
			}
		case "WRITE":
			addrInt, _ := strconv.ParseUint(args[0], 10, 16)
			valFloat, _ := strconv.ParseFloat(args[1], 64)
			s.CommandChan <- WriteEngCmd{Addr: uint16(addrInt), EngVal: valFloat}
		case "RAMP":
			addrInt, _ := strconv.ParseUint(args[0], 10, 16)
			startVal, _ := strconv.ParseFloat(args[1], 64)
			endVal, _ := strconv.ParseFloat(args[2], 64)
			duration, _ := strconv.ParseFloat(args[3], 64)
			steps := int(duration * 20)
			if steps == 0 {
				steps = 1
			}
			for i := 0; i <= steps; i++ {
				progress := float64(i) / float64(steps)
				currentEngVal := startVal + (endVal-startVal)*progress
				s.CommandChan <- WriteEngCmd{Addr: uint16(addrInt), EngVal: currentEngVal}
				time.Sleep(50 * time.Millisecond)
			}
		default:
			s.log.Printf("SCENARIO WARNING: Unknown command '%s' on line %d", command, lineNumber)
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.log.Println("SCENARIO: Script finished.")
}

func (s *Server) computeModbusCRC(data []byte) uint16 {
	var crc uint16 = 0xFFFF
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if (crc&0x0001) != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func (s *Server) processRequestFrame(request []byte) []byte {
	s.log.Printf("SRV RX: %X", request)
	if len(request) < 8 {
		s.log.Printf("Short frame received: len %d", len(request))
		return nil
	}
	if request[0] != config.SlaveID {
		s.log.Printf("Request for wrong slave ID: %d", request[0])
		return nil
	}
	receivedCRC := binary.LittleEndian.Uint16(request[len(request)-2:])
	calculatedCRC := s.computeModbusCRC(request[:len(request)-2])
	if receivedCRC != calculatedCRC {
		s.log.Printf("CRC Error: Rcvd 0x%X, Calc 0x%X", receivedCRC, calculatedCRC)
		return nil
	}
	funcCode := request[1]
	pduAddr := binary.BigEndian.Uint16(request[2:4])
	humanAddr := pduAddr + 40001
	var responsePDU []byte
	switch funcCode {
	case 3:
		count := binary.BigEndian.Uint16(request[4:6])
		s.log.Printf("RX FC3: Addr %d, Count %d", humanAddr, count)
		if count > 125 {
			return nil
		}
		s.mu.Lock()
		values := make([]uint16, count)
		for i := 0; i < int(count); i++ {
			values[i] = s.datastore[humanAddr+uint16(i)]
		}
		s.mu.Unlock()
		byteCount := byte(len(values) * 2)
		responsePDU = make([]byte, 1+byteCount)
		responsePDU[0] = byteCount
		for i, v := range values {
			binary.BigEndian.PutUint16(responsePDU[1+i*2:], v)
		}
	case 6:
		value := binary.BigEndian.Uint16(request[4:6])
		s.log.Printf("RX FC6: Addr %d, Value %d", humanAddr, value)
		s.CommandChan <- WriteRawCmd{Addr: humanAddr, RawVal: value}
		responsePDU = request[2:6]
	case 16:
		count := binary.BigEndian.Uint16(request[4:6])
		byteCount := int(request[6])
		if len(request) >= 7+byteCount {
			s.log.Printf("RX FC16: Addr %d, Count %d", humanAddr, count)
			for i := 0; i < int(count); i++ {
				value := binary.BigEndian.Uint16(request[7+i*2:])
				s.CommandChan <- WriteRawCmd{Addr: humanAddr + uint16(i), RawVal: value}
			}
			responsePDU = request[2:6]
		} else {
			s.log.Printf("Malformed FC16: Not enough data for byte count %d", byteCount)
			return nil
		}
	default:
		s.log.Printf("Unsupported function code: %d", funcCode)
		responseFrame := []byte{config.SlaveID, funcCode | 0x80, 0x01}
		crc := s.computeModbusCRC(responseFrame)
		return append(responseFrame, byte(crc&0xFF), byte(crc>>8))
	}

	if responsePDU != nil {
		responseFrame := append([]byte{config.SlaveID, funcCode}, responsePDU...)
		crc := s.computeModbusCRC(responseFrame)
		return append(responseFrame, byte(crc&0xFF), byte(crc>>8))
	}
	return nil
}

func (s *Server) handleConnection(conn io.ReadWriter) {
    defer func() {
        if c, ok := conn.(io.Closer); ok {
            c.Close()
        }
    }()

    const maxPacketSize = 256
    buffer := make([]byte, 0, maxPacketSize)
    temp := make([]byte, maxPacketSize)
    lastRead := time.Now()
    interFrameTimeout := 4 * time.Millisecond // ~3.5 char times at 9600 baud

    for {
        // Set a short read timeout to allow checking for inter-frame gaps
        if serialPort, ok := conn.(serial.Port); ok {
            serialPort.SetReadTimeout(1 * time.Millisecond)
        }

        n, err := conn.Read(temp)
        if err != nil {
            if err != io.EOF && !os.IsTimeout(err) {
                s.log.Printf("Connection read error: %v", err)
                return
            }
        }

        if n > 0 {
            buffer = append(buffer, temp[:n]...)
            lastRead = time.Now()
        }

        // Process buffer if we have enough data or an inter-frame gap has occurred
        for len(buffer) > 0 && (n == 0 || time.Since(lastRead) >= interFrameTimeout) {
            // Minimum Modbus RTU packet: 1 (slave ID) + 1 (func code) + 2 (CRC) = 4 bytes
            if len(buffer) < 4 {
                s.log.Printf("Short frame received: len %d", len(buffer))
                buffer = buffer[:0] // Clear buffer
                continue
            }

            // Check slave ID
            if buffer[0] != config.SlaveID {
                s.log.Printf("Request for wrong slave ID: %d", buffer[0])
                buffer = buffer[1:] // Slide window
                continue
            }

            // Determine expected packet length based on function code
            funcCode := buffer[1]
            expectedLen := 0
            switch funcCode {
            case 3, 4: // Read Holding/Input Registers
                if len(buffer) >= 6 {
                    expectedLen = 8 // SlaveID(1) + FuncCode(1) + Addr(2) + Count(2) + CRC(2)
                }
            case 6: // Write Single Register
                expectedLen = 8 // SlaveID(1) + FuncCode(1) + Addr(2) + Value(2) + CRC(2)
            case 16: // Write Multiple Registers
                if len(buffer) >= 7 {
                    byteCount := int(buffer[6])
                    expectedLen = 7 + byteCount + 2 // SlaveID(1) + FuncCode(1) + Addr(2) + Count(2) + ByteCount(1) + Data + CRC(2)
                }
            default:
                // Unsupported function code: return exception
                responseFrame := []byte{config.SlaveID, funcCode | 0x80, 0x01}
                crc := s.computeModbusCRC(responseFrame)
                responseFrame = append(responseFrame, byte(crc&0xFF), byte(crc>>8))
                s.log.Printf("SRV TX: %X", responseFrame)
                conn.Write(responseFrame)
                buffer = buffer[1:] // Slide window
                continue
            }

            // Wait for more data if packet is incomplete
            if expectedLen == 0 || len(buffer) < expectedLen {
                if n == 0 && time.Since(lastRead) >= interFrameTimeout {
                    s.log.Printf("Incomplete frame received: len %d, expected %d", len(buffer), expectedLen)
                    buffer = buffer[:0] // Clear buffer
                }
                break
            }

            // Process complete packet
            request := buffer[:expectedLen]
            buffer = buffer[expectedLen:] // Remove processed packet
            response := s.processRequestFrame(request)
            if response != nil {
                s.log.Printf("SRV TX: %X", response)
                _, err := conn.Write(response)
                if err != nil {
                    s.log.Printf("Connection write error: %v", err)
                    return
                }
            }
        }

        // Reset buffer if it grows too large (e.g., garbage data)
        if len(buffer) >= maxPacketSize {
            s.log.Printf("Buffer overflow, clearing buffer")
            buffer = buffer[:0]
        }
    }
}

func (s *Server) RunTCP() {
	s.log.Println("Starting Manual TCP server...")
	address := fmt.Sprintf("%s:%d", config.TCPServerHost, config.TCPServerPort)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		s.log.Fatalf("Failed to start TCP listener: %v", err)
	}
	go func() {
		<-s.shutdownChan
		s.log.Println("TCP listener shutting down.")
		listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-s.shutdownChan:
				return
			default:
				if !errors.Is(err, net.ErrClosed) {
					s.log.Printf("Failed to accept connection: %v", err)
				}
			}
			continue
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) RunSerial(portName string) {
	s.log.Println("Starting Manual Serial server on", portName)
	mode := &serial.Mode{BaudRate: 9600}
	for {
		select {
		case <-s.shutdownChan:
			return
		default:
		}
		port, err := serial.Open(portName, mode)
		if err != nil {
			s.log.Printf("Failed to open serial port %s: %v. Retrying in 5s...", portName, err)
			time.Sleep(5 * time.Second)
			continue
		}
		s.port = port
		port.SetReadTimeout(1 * time.Second)
		s.log.Printf("Serial port %s opened successfully.", portName)
		s.handleConnection(s.port)
		s.log.Printf("Serial port %s connection closed, reopening...", portName)
	}
}

func (s *Server) Stop() {
	s.log.Println("Stopping server...")
	close(s.shutdownChan)
	if s.port != nil {
		s.port.Close()
	}
}