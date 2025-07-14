// ===== C:\Projects\modbus-tools\modbus-go-poller\poller\poller.go =====
package poller

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"modbus-tools/modbus-go-poller/config"
	"modbus-tools/modbus-go-poller/database"
	"net"
	"sort"
	"sync"
	"time"

	"go.bug.st/serial"
)

func computeModbusCRC(data []byte) uint16 {
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

func buildReadRequest(slaveID byte, startAddr, count uint16) []byte {
	pduAddr := startAddr - 40001
	pdu := []byte{slaveID, 3, byte(pduAddr >> 8), byte(pduAddr), byte(count >> 8), byte(count)}
	crc := computeModbusCRC(pdu)
	return append(pdu, byte(crc&0xFF), byte(crc>>8))
}

func buildWriteRequest(slaveID byte, addr, value uint16) []byte {
	pduAddr := addr - 40001
	pdu := []byte{slaveID, 6, byte(pduAddr >> 8), byte(pduAddr), byte(value >> 8), byte(value)}
	crc := computeModbusCRC(pdu)
	return append(pdu, byte(crc&0xFF), byte(crc>>8))
}

func buildWriteMultipleRequest(slaveID byte, startAddr uint16, values []uint16) []byte {
	pduAddr := startAddr - 40001
	count := uint16(len(values))
	byteCount := byte(count * 2)

	pdu := []byte{slaveID, 16, byte(pduAddr >> 8), byte(pduAddr), byte(count >> 8), byte(count), byteCount}
	for _, v := range values {
		pdu = append(pdu, byte(v>>8), byte(v))
	}
	crc := computeModbusCRC(pdu)
	return append(pdu, byte(crc&0xFF), byte(crc>>8))
}

// executeIO performs the raw I/O and returns the results for later processing.
func executeIO(conn io.ReadWriter, request []byte, txLogger *log.Logger) ([]byte, float64, time.Time, bool) {
	txTime := time.Now()
	txLogger.Printf("TX: %X", request)

	if _, err := conn.Write(request); err != nil {
		txLogger.Printf("TX ERROR: %v", err)
		return nil, 0, txTime, false
	}

	header := make([]byte, 3)
	n, err := io.ReadFull(conn, header)
	if err != nil {
		txLogger.Printf("RX HEADER ERROR: %v (bytes read: %d)", err, n)
		return nil, 0, txTime, false
	}

	var expectedBodyLength int
	funcCode := header[1]
	if funcCode == 3 || funcCode == 4 {
		byteCount := int(header[2])
		expectedBodyLength = byteCount + 2
	} else if funcCode == 6 || funcCode == 16 {
		expectedBodyLength = 5
	} else if funcCode > 0x80 {
		expectedBodyLength = 2
	} else {
		txLogger.Printf("RX UNEXPECTED FRAME: %X", header)
		return nil, 0, txTime, false
	}

	body := make([]byte, expectedBodyLength)
	n, err = io.ReadFull(conn, body)
	if err != nil {
		txLogger.Printf("RX BODY ERROR: %v (bytes read: %d)", err, n)
		return nil, 0, txTime, false
	}

	fullResponse := append(header, body...)
	rtt := float64(time.Since(txTime).Microseconds()) / 1000.0
	txLogger.Printf("RX: %X", fullResponse)

	return fullResponse, rtt, txTime, true
}

func RunIO(ctx context.Context, wg *sync.WaitGroup, state *PollerState, soeLogger, txLogger *log.Logger, polledDataChan chan<- map[uint16]uint16, commandPacketChan <-chan []byte, mode, target string) {
	defer wg.Done()
	soeLogger.Println("I/O Goroutine Started.")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, pollGroups := connectAndPrepare(state, soeLogger, mode, target)
		if conn == nil {
			time.Sleep(2 * time.Second)
			continue
		}

		pollInterval := time.Duration(config.DefaultPollIntervalS * float64(time.Second))
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		
		var loopErr error

	innerLoop:
		for {
			select {
			case <-ctx.Done():
				break innerLoop
			case packet := <-commandPacketChan:
				response, rtt, txTime, ok := executeIO(conn, packet, txLogger)
				state.IOResultChan <- IO_Result{TxTime: txTime, RTT: rtt, Tx: packet, Rx: response}
				if !ok {
					loopErr = errors.New("command I/O failed")
					break innerLoop
				}
			case <-ticker.C:
				allPolledData := make(map[uint16]uint16)
				for _, group := range pollGroups {
					request := buildReadRequest(config.DefaultSlaveID, group.StartAddress, group.Count)
					response, rtt, txTime, ok := executeIO(conn, request, txLogger)
					state.IOResultChan <- IO_Result{TxTime: txTime, RTT: rtt, Tx: request, Rx: response}
					if !ok {
						loopErr = errors.New("poll I/O failed")
						break innerLoop
					}

					if response != nil {
						if len(response) > 2 && response[1] > 0x80 {
							soeLogger.Printf("ERROR: Modbus exception response received: %X", response)
						} else if len(response) > 5 && response[0] == config.DefaultSlaveID && response[1] == 3 {
							byteCount := int(response[2])
							if len(response) >= 3+byteCount {
								r := bytes.NewReader(response[3:])
								for j := 0; j < byteCount/2; j++ {
									var value uint16
									err := binary.Read(r, binary.BigEndian, &value)
									if err != nil {
										break
									}
									addr := group.StartAddress + uint16(j)
									allPolledData[addr] = value
								}
							}
						}
					}
				}
				if loopErr != nil {
					break innerLoop
				}
				
				nowTime := uint32(time.Now().Unix())
				state.UpdateHeartbeat(uint16(nowTime>>16), uint16(nowTime&0xFFFF), nowTime)
				packet := buildWriteMultipleRequest(config.DefaultSlaveID, 40008, []uint16{uint16(nowTime >> 16), uint16(nowTime & 0xFFFF)})
				response, rtt, txTime, ok := executeIO(conn, packet, txLogger)
				state.IOResultChan <- IO_Result{TxTime: txTime, RTT: rtt, Tx: packet, Rx: response}
				if !ok {
					loopErr = errors.New("heartbeat I/O failed")
					break innerLoop
				}

				if len(allPolledData) > 0 {
					select {
					case polledDataChan <- allPolledData:
					case <-ctx.Done():
						break innerLoop
					}
				}
			}
		}

		ticker.Stop()
		conn.Close()
		if loopErr != nil {
			soeLogger.Printf("Connection loop failed: %v. Reconnecting...", loopErr)
			state.SetStatus(fmt.Sprintf("Connection failed: %v", loopErr))
		} else {
			soeLogger.Println("Connection loop exited gracefully. Shutting down I/O.")
			return
		}
		time.Sleep(2 * time.Second)
	}
}

func connectAndPrepare(state *PollerState, soeLogger *log.Logger, mode, target string) (io.ReadWriteCloser, []config.PollGroup) {
	const maxRegsPerPoll = 120
	const maxGap = 10

	state.mu.Lock()
	allAddrs := make([]int, 0, len(state.Config.PointsByAddress))
	for addr := range state.Config.PointsByAddress {
		allAddrs = append(allAddrs, int(addr))
	}
	state.mu.Unlock()
	sort.Ints(allAddrs)
	var pollGroups []config.PollGroup
	if len(allAddrs) > 0 {
		currentGroup := config.PollGroup{StartAddress: uint16(allAddrs[0]), Count: 1}
		for i := 1; i < len(allAddrs); i++ {
			addr := uint16(allAddrs[i])
			lastAddrInGroup := currentGroup.StartAddress + currentGroup.Count - 1
			gap := addr - lastAddrInGroup
			if (addr-currentGroup.StartAddress+1) > maxRegsPerPoll || gap > maxGap {
				pollGroups = append(pollGroups, currentGroup)
				currentGroup = config.PollGroup{StartAddress: addr, Count: 1}
			} else {
				currentGroup.Count = (addr - currentGroup.StartAddress) + 1
			}
		}
		pollGroups = append(pollGroups, currentGroup)
	}
	if len(pollGroups) == 0 {
		soeLogger.Println("No poll groups defined.")
		return nil, nil
	}
	soeLogger.Printf("Dynamically calculated %d poll groups.", len(pollGroups))

	state.SetStatus(fmt.Sprintf("Connecting to %s target %s...", mode, target))
	var conn io.ReadWriteCloser
	var err error
	if mode == "tcp" {
		conn, err = net.DialTimeout("tcp", target, 5*time.Second)
	} else {
		serialMode := &serial.Mode{BaudRate: 9600}
		port, openErr := serial.Open(target, serialMode)
		if openErr == nil {
			port.SetReadTimeout(500 * time.Millisecond)
			if err := port.ResetInputBuffer(); err != nil {
				soeLogger.Printf("Failed to reset serial input buffer: %v", err)
			}
		}
		conn, err = port, openErr
	}
	if err != nil {
		soeLogger.Printf("Connection failed: %v.", err)
		state.SetStatus(fmt.Sprintf("Connection failed: %v", err))
		return nil, nil
	}
	state.SetStatus(fmt.Sprintf("Connected to %s", target))
	soeLogger.Printf("Connected to %s", target)
	return conn, pollGroups
}


func RunStateProcessor(ctx context.Context, wg *sync.WaitGroup, state *PollerState, log *log.Logger, polledDataChan <-chan map[uint16]uint16, commandPacketChan chan<- []byte) {
	defer wg.Done()
	log.Println("State Processor Goroutine Started.")
	hasProcessedInitialState := false

	for {
		select {
		case result := <-state.IOResultChan:
			state.UpdateTx(result.Tx, result.TxTime)
			if len(result.Rx) > 0 {
				state.UpdateRx(result.Rx, result.RTT, result.TxTime)
			}
		case newData, ok := <-polledDataChan:
			if !ok {
				return
			}
			if !hasProcessedInitialState {
				log.Println("--- PROCESSING INITIAL STATE ---")
				state.UpdateFromPoll(newData)
				hasProcessedInitialState = true
			}
			
			state.CommitState()
			state.UpdateFromPoll(newData)
			
			processStateChanges(state, log)
		case cmd := <-state.CommandChan:
			handleCommand(ctx, state, cmd, commandPacketChan, log)

		case <-ctx.Done():
			log.Println("State Processor Goroutine shutting down.")
			return
		}
	}
}

func handleCommand(ctx context.Context, state *PollerState, cmd interface{}, commandPacketChan chan<- []byte, soeLogger *log.Logger) {
	var packet []byte
	dbEvent := database.Event{EventType: "USER_COMMAND", Timestamp: time.Now()}

	switch c := cmd.(type) {
	case SetBitCmd:
		current, _, _, _, _, _, _, _ := state.GetSnapshot()
		val, ok := current[c.Addr]
		if !ok {
			soeLogger.Printf("Cannot SET bit on unknown register %d", c.Addr)
			return
		}
		if c.Val {
			val |= (1 << c.Bit)
		} else {
			val &= ^(1 << c.Bit)
		}
		packet = buildWriteRequest(config.DefaultSlaveID, c.Addr, val)
	case WriteRawCmd:
		packet = buildWriteRequest(config.DefaultSlaveID, c.Addr, c.Value)
		soeLogger.Printf("SOE: [USER_COMMAND] Register %d raw written to %d", c.Addr, c.Value)
		dbEvent.PointName = fmt.Sprintf("Register %d", c.Addr)
		dbEvent.NewValue = fmt.Sprintf("%d (raw)", c.Value)
		state.DBEventChan <- dbEvent
	case WriteEngCmd:
		var pd *config.PointDefinition
		if pds, ok := state.Config.PointsByAddress[c.Addr]; ok {
			for _, p := range pds {
				if p.Type == "analog" {
					pd = p
					break
				}
			}
		}
		rawVal := UnscaleValue(c.EngVal, pd)
		packet = buildWriteRequest(config.DefaultSlaveID, c.Addr, rawVal)
		pointName := fmt.Sprintf("Register %d", c.Addr)
		if pd != nil {
			pointName = pd.PointName
		}
		soeLogger.Printf("SOE: [USER_COMMAND] %s written to %.2f (Raw: %d)", pointName, c.EngVal, rawVal)
		dbEvent.PointName = pointName
		dbEvent.NewValue = fmt.Sprintf("%.2f", c.EngVal)
		if pd != nil {
			dbEvent.Units = pd.Unit
		}
		state.DBEventChan <- dbEvent
	case PulseDOUCmd:
		soeLogger.Printf("SOE: [USER_COMMAND] Raising %s for %v", c.Acronym, c.Duration)
		state.DBEventChan <- database.Event{Timestamp: time.Now(), PointName: c.Acronym, NewValue: fmt.Sprintf("PULSE %v", c.Duration), EventType: "USER_COMMAND"}
		onCmd := SetBitCmd{Addr: c.Addr, Bit: c.Bit, Val: true}
		current, _, _, _, _, _, _, _ := state.GetSnapshot()
		onVal := current[onCmd.Addr] | (1 << onCmd.Bit)
		packet = buildWriteRequest(config.DefaultSlaveID, onCmd.Addr, onVal)
		go func(gCtx context.Context) {
			select {
			case <-time.After(c.Duration):
				state.SendCommand(SetBitCmd{Addr: c.Addr, Bit: c.Bit, Val: false})
				soeLogger.Printf("SOE: [AUTO] Pulse complete for %s", c.Acronym)
			case <-gCtx.Done():
				soeLogger.Printf("SOE: [AUTO] Pulse for %s cancelled due to shutdown.", c.Acronym)
				return
			}
		}(ctx)
	}

	if packet != nil {
		commandPacketChan <- packet
	}
}


func processStateChanges(state *PollerState, log *log.Logger) {
	current, previous, _, _, _, _, _, _ := state.GetSnapshot()
	if len(previous) == 0 {
		return
	}

	changeTime := time.Now()
	newActiveAlarms := make(map[string]ActiveAlarm)
	
	var sortedAddresses []uint16
	for addr := range current {
		sortedAddresses = append(sortedAddresses, addr)
	}
	sort.Slice(sortedAddresses, func(i, j int) bool { return sortedAddresses[i] < sortedAddresses[j] })

	for _, addr := range sortedAddresses {
		currentVal := current[addr]
		prevVal, ok := previous[addr]
		if !ok || currentVal == prevVal {
			continue
		}

		state.mu.Lock()
		state.LastChange[addr] = changeTime
		state.mu.Unlock()

		if pointDefs, ok := state.Config.PointsByAddress[addr]; ok {
			for _, pointDef := range pointDefs {
				if pointDef.Type == "analog" {
					currEng := ScaleValue(currentVal, pointDef)
					prevEng := ScaleValue(prevVal, pointDef)
					log.Printf("SOE: [FIELD_CHANGE] %s changed from %.2f to %.2f %s", pointDef.PointName, prevEng, currEng, pointDef.Unit)
					if pointDef.LogEvents {
						state.DBEventChan <- database.Event{Timestamp: changeTime, PointName: pointDef.PointName, PreviousValue: fmt.Sprintf("%.2f", prevEng), NewValue: fmt.Sprintf("%.2f", currEng), Units: pointDef.Unit, EventType: "FIELD_CHANGE"}
					}
				} else {
					if (currentVal>>*pointDef.Bit)&1 != (prevVal>>*pointDef.Bit)&1 {
						newStateIsOn := (currentVal>>*pointDef.Bit)&1 == 1
						newStateText := pointDef.StateOff
						oldStateText := pointDef.StateOn
						if newStateIsOn {
							newStateText = pointDef.StateOn
							oldStateText = pointDef.StateOff
						}
						log.Printf("SOE: [FIELD_CHANGE]  -> %s changed from %s to %s", pointDef.PointName, oldStateText, newStateText)
						if pointDef.LogEvents {
							state.DBEventChan <- database.Event{Timestamp: changeTime, PointName: pointDef.PointName, PreviousValue: oldStateText, NewValue: newStateText, EventType: "FIELD_CHANGE"}
						}
					}
				}
			}
		}
	}
	state.SetAlarms(newActiveAlarms)
}