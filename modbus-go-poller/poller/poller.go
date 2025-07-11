package poller

import (
	"context"
	"encoding/binary"
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
			if (crc & 0x0001) != 0 {
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

func RunIO(ctx context.Context, wg *sync.WaitGroup, state *PollerState, log *log.Logger, mode, target string) {
	defer wg.Done()
	log.Println("I/O Goroutine Started.")

	const maxRegsPerPoll = 120
	const maxGap = 10
	var pollGroups []config.PollGroup
	var allAddrs []int
	for addr := range state.Config.PointsByAddress {
		allAddrs = append(allAddrs, int(addr))
	}
	sort.Ints(allAddrs)

	if len(allAddrs) > 0 {
		currentGroup := config.PollGroup{
			StartAddress: uint16(allAddrs[0]),
			Count:        1,
		}
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
		log.Fatalln("FATAL: No poll groups could be generated from configured points. Exiting.")
	}
	log.Printf("Dynamically calculated %d poll groups.", len(pollGroups))
	for i, g := range pollGroups {
		log.Printf("  Group %d: Start=%d, Count=%d", i+1, g.StartAddress, g.Count)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("I/O Goroutine shutting down before connection attempt.")
			return
		default:
		}

		var conn io.ReadWriteCloser
		var err error
		state.SetStatus(fmt.Sprintf("Connecting to %s target %s...", mode, target))
		if mode == "tcp" {
			conn, err = net.DialTimeout("tcp", target, 5*time.Second)
		} else {
			serialMode := &serial.Mode{BaudRate: 9600, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit}
			conn, err = serial.Open(target, serialMode)
		}
		if err != nil {
			log.Printf("Connection failed: %v. Retrying.", err)
			state.SetStatus(fmt.Sprintf("Connection failed: %v", err))
			time.Sleep(5 * time.Second)
			continue
		}

		state.SetStatus(fmt.Sprintf("Connected to %s", target))
		log.Printf("Connected to %s", target)

		for {
			cycleStartTime := time.Now()

			select {
			case <-ctx.Done():
				log.Println("I/O Goroutine shutting down.")
				conn.Close()
				return
			case cmd := <-state.CommandChan:
				var packet []byte
				var dbEvent database.Event
				dbEvent.EventType = "USER_COMMAND"
				dbEvent.Timestamp = time.Now()
				switch c := cmd.(type) {
				case SetBitCmd:
					current, _, _, _, _, _, _ := state.GetSnapshot()
					currentVal := current[c.Addr]
					var newVal uint16
					if c.Val {
						newVal = currentVal | (1 << c.Bit)
					} else {
						newVal = currentVal & ^(1 << c.Bit)
					}
					packet = buildWriteRequest(config.DefaultSlaveID, c.Addr, newVal)
					var pointDef *config.PointDefinition
					if points, ok := state.Config.PointsByAddress[c.Addr]; ok {
						for _, p := range points {
							if p.Bit != nil && *p.Bit == c.Bit {
								pointDef = p
								break
							}
						}
					}
					if pointDef == nil {
						log.Printf("ERROR: Could not find point definition for addr %d bit %d", c.Addr, c.Bit)
						continue
					}
					var newStateText string
					if c.Val {
						newStateText = pointDef.StateOn
					} else {
						newStateText = pointDef.StateOff
					}
					log.Printf("SOE: [USER_COMMAND] %s set to %v (%s)", pointDef.PointName, c.Val, newStateText)
					dbEvent.PointName = pointDef.PointName
					dbEvent.NewValue = newStateText
					state.DBEventChan <- dbEvent
					state.mu.Lock()
					state.WriteSuppressions[pointDef.PointName] = newStateText
					state.mu.Unlock()

				case WriteEngCmd:
					var pointDef *config.PointDefinition
					if points, ok := state.Config.PointsByAddress[c.Addr]; ok {
						for _, p := range points {
							if p.Type == "analog" {
								pointDef = p
								break
							}
						}
					}
					if pointDef == nil {
						log.Printf("ERROR: Could not find analog point definition for addr %d", c.Addr)
						continue
					}
					rawVal := UnscaleValue(c.EngVal, pointDef)
					packet = buildWriteRequest(config.DefaultSlaveID, c.Addr, rawVal)
					log.Printf("SOE: [USER_COMMAND] %s written to %.2f (Raw: %d)", pointDef.PointName, c.EngVal, rawVal)
					dbEvent.PointName = pointDef.PointName
					dbEvent.NewValue = fmt.Sprintf("%.2f", c.EngVal)
					dbEvent.Units = pointDef.Unit
					state.DBEventChan <- dbEvent
					state.mu.Lock()
					state.WriteSuppressions[pointDef.PointName] = fmt.Sprintf("%.2f", c.EngVal)
					state.mu.Unlock()
				}
				if packet != nil {
					txTime := time.Now()
					state.UpdateTx(packet, txTime)
					if _, err := conn.Write(packet); err != nil {
						log.Printf("CMD Write error: %v", err)
						goto reconnect
					}
					time.Sleep(200 * time.Millisecond)
					ackBuffer := make([]byte, 256)
					n, _ := conn.Read(ackBuffer)
					if n > 0 {
						rtt := float64(time.Since(txTime).Microseconds()) / 1000.0
						state.UpdateRx(ackBuffer[:n], rtt, txTime)
					}
				}
			default:
				// No command, proceed to poll
			}

			polledData := make(map[uint16]uint16)
			successfulPoll := true
			for _, group := range pollGroups {
				request := buildReadRequest(config.DefaultSlaveID, group.StartAddress, group.Count)
				txTime := time.Now()
				state.UpdateTx(request, txTime)
				_, err := conn.Write(request)
				if err != nil {
					log.Printf("Poll Write error: %v", err)
					successfulPoll = false
					goto reconnect
				}
				time.Sleep(250 * time.Millisecond)
				respBuffer := make([]byte, 256)
				n, err := conn.Read(respBuffer)
				if err != nil {
					log.Printf("Poll Read error: %v", err)
					successfulPoll = false
					goto reconnect
				}
				if n > 0 {
					rtt := float64(time.Since(txTime).Microseconds()) / 1000.0
					response := respBuffer[:n]
					state.UpdateRx(response, rtt, txTime)
					if n > 2 && response[1] > 0x80 {
						log.Printf("ERROR: Modbus exception response received: %X", response)
						successfulPoll = false
						continue
					}
					if n > 5 && response[0] == config.DefaultSlaveID && response[1] == 3 {
						byteCount := int(response[2])
						if n >= 5+byteCount {
							for i := 0; i < byteCount/2; i++ {
								addr := group.StartAddress + uint16(i)
								value := binary.BigEndian.Uint16(response[3+i*2:])
								polledData[addr] = value
							}
						}
					}
				}
			}

			if len(polledData) > 0 {
				state.UpdateFromPoll(polledData)
			}

			if successfulPoll {
				nowTime := uint32(time.Now().Unix())
				var packet []byte
				if nowTime != state.lastHeartbeatTime {
					lastHigh := uint16(state.lastHeartbeatTime >> 16)
					nowHigh := uint16(nowTime >> 16)
					nowLow := uint16(nowTime & 0xFFFF)
					state.UpdateHeartbeat(nowHigh, nowLow, nowTime)
					if nowHigh != lastHigh {
						packet = buildWriteMultipleRequest(config.DefaultSlaveID, 40008, []uint16{nowHigh, nowLow})
					} else {
						packet = buildWriteRequest(config.DefaultSlaveID, 40009, nowLow)
					}
					if packet != nil {
						if _, err := conn.Write(packet); err != nil {
							log.Printf("Heartbeat Write error: %v", err)
							goto reconnect
						}
					}
				}
			}

			elapsed := time.Since(cycleStartTime)
			pollInterval := time.Duration(config.DefaultPollIntervalS * float64(time.Second))
			if elapsed < pollInterval {
				time.Sleep(pollInterval - elapsed)
			}
		}

	reconnect:
		conn.Close()
		log.Println("Connection closed due to error. Reconnecting...")
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func RunStateProcessor(ctx context.Context, wg *sync.WaitGroup, state *PollerState, log *log.Logger) {
	defer wg.Done()
	log.Println("State Processor Goroutine Started.")
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	hasProcessedInitialState := false

	for {
		select {
		case <-ticker.C:
			current, previous, oldAlarms, _, _, _, _ := state.GetSnapshot()
			if len(current) == 0 {
				continue
			}
			isFirstPoll := !hasProcessedInitialState
			isDifferent := false
			if isFirstPoll {
				for _, v := range current {
					if v != 0 {
						isDifferent = true
						break
					}
				}
			} else {
				for addr, val := range current {
					if prevVal, ok := previous[addr]; !ok || val != prevVal {
						isDifferent = true
						break
					}
				}
			}
			if !isDifferent {
				continue
			}

			changeTime := time.Now()
			newActiveAlarms := make(map[string]ActiveAlarm)
			var sortedAddresses []uint16
			for addr := range state.Config.PointsByAddress {
				sortedAddresses = append(sortedAddresses, addr)
			}
			sort.Slice(sortedAddresses, func(i, j int) bool { return sortedAddresses[i] < sortedAddresses[j] })

			for _, addr := range sortedAddresses {
				pointDefs, ok := state.Config.PointsByAddress[addr]
				if !ok {
					continue
				}
				currentVal := current[addr]
				prevVal := previous[addr]
				for _, pointDef := range pointDefs {
					if isFirstPoll {
						if pointDef.Type == "analog" {
							if currentVal != 0 {
								scaledVal := ScaleValue(currentVal, pointDef)
								var rawDisplay interface{}
								if pointDef.DataType == "signed" {
									rawDisplay = int16(currentVal)
								} else {
									rawDisplay = currentVal
								}
								log.Printf("SOE: [INITIAL_STATE] %s is %.2f %s (Raw: %v)", pointDef.PointName, scaledVal, pointDef.Unit, rawDisplay)
								if pointDef.LogEvents {
									state.DBEventChan <- database.Event{Timestamp: changeTime, PointName: pointDef.PointName, NewValue: fmt.Sprintf("%.2f", scaledVal), Units: pointDef.Unit, EventType: "INITIAL_STATE"}
								}
							}
						} else {
							bitIsOn := (currentVal>>*pointDef.Bit)&1 == 1
							if pointDef.NormalState != nil && ((bitIsOn && *pointDef.NormalState == 0) || (!bitIsOn && *pointDef.NormalState == 1)) {
								stateText := pointDef.StateOff
								if bitIsOn {
									stateText = pointDef.StateOn
								}
								log.Printf("SOE: [INITIAL_STATE] -> %s is %s", pointDef.PointName, stateText)
								if pointDef.LogEvents {
									state.DBEventChan <- database.Event{Timestamp: changeTime, PointName: pointDef.PointName, NewValue: stateText, EventType: "INITIAL_STATE"}
								}
							}
						}
					} else if currentVal != prevVal {
						if pointDef.Type == "analog" {
							currEng := ScaleValue(currentVal, pointDef)
							prevEng := ScaleValue(prevVal, pointDef)
							currEngStr := fmt.Sprintf("%.2f", currEng)
							prevEngStr := fmt.Sprintf("%.2f", prevEng)
							state.mu.Lock()
							suppressedVal, isSuppressed := state.WriteSuppressions[pointDef.PointName]
							state.mu.Unlock()
							if isSuppressed && suppressedVal == currEngStr {
								state.mu.Lock()
								delete(state.WriteSuppressions, pointDef.PointName)
								state.mu.Unlock()
								continue
							}
							log.Printf("SOE: [FIELD_CHANGE] %s changed from %s to %s %s", pointDef.PointName, prevEngStr, currEngStr, pointDef.Unit)
							if pointDef.LogEvents {
								state.DBEventChan <- database.Event{Timestamp: changeTime, PointName: pointDef.PointName, PreviousValue: prevEngStr, NewValue: currEngStr, Units: pointDef.Unit, EventType: "FIELD_CHANGE"}
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
								state.mu.Lock()
								suppressedVal, isSuppressed := state.WriteSuppressions[pointDef.PointName]
								state.mu.Unlock()
								if isSuppressed && suppressedVal == newStateText {
									state.mu.Lock()
									delete(state.WriteSuppressions, pointDef.PointName)
									state.mu.Unlock()
									continue
								}
								log.Printf("SOE: [FIELD_CHANGE]  -> %s changed from %s to %s", pointDef.PointName, oldStateText, newStateText)
								if pointDef.LogEvents {
									state.DBEventChan <- database.Event{Timestamp: changeTime, PointName: pointDef.PointName, PreviousValue: oldStateText, NewValue: newStateText, EventType: "FIELD_CHANGE"}
								}
							}
						}
					}
					alarmDefs, hasAlarms := state.Config.AlarmsByPoint[pointDef.PointName]
					if !hasAlarms {
						continue
					}
					for _, alarmDef := range alarmDefs {
						isAlarmActive := false
						if pointDef.Type == "analog" {
							scaledVal := ScaleValue(currentVal, pointDef)
							if (alarmDef.Type == "high" && scaledVal >= alarmDef.Limit) || (alarmDef.Type == "low" && scaledVal <= alarmDef.Limit) {
								isAlarmActive = true
							}
						} else if pointDef.Type == "bitmap" {
							currentBitVal := (currentVal >> *pointDef.Bit) & 1
							if pointDef.NormalState != nil {
								if alarmDef.Type == "on" && currentBitVal != uint16(*pointDef.NormalState) {
									isAlarmActive = true
								}
								if alarmDef.Type == "off" && currentBitVal == uint16(*pointDef.NormalState) {
									isAlarmActive = true
								}
							}
						}
						if isAlarmActive {
							key := fmt.Sprintf("%d-%s", addr, alarmDef.Message)
							newActiveAlarms[key] = ActiveAlarm{Severity: alarmDef.Severity, Message: alarmDef.Message}
						}
					}
				}
			}
			for key, alarm := range newActiveAlarms {
				if _, exists := oldAlarms[key]; !exists {
					log.Printf("SOE: [ALARM_RAISED] %s: %s", alarm.Severity, alarm.Message)
					state.DBEventChan <- database.Event{Timestamp: changeTime, PointName: alarm.Message, NewValue: "RAISED", EventType: "ALARM_RAISED", PreviousValue: alarm.Severity}
				}
			}
			for key, alarm := range oldAlarms {
				if _, exists := newActiveAlarms[key]; !exists {
					log.Printf("SOE: [ALARM_CLEARED] %s", alarm.Message)
					state.DBEventChan <- database.Event{Timestamp: changeTime, PointName: alarm.Message, NewValue: "CLEARED", EventType: "ALARM_CLEARED", PreviousValue: "RAISED"}
				}
			}
			state.SetAlarms(newActiveAlarms)
			state.CommitState()
			if isFirstPoll {
				hasProcessedInitialState = true
			}
		case <-ctx.Done():
			log.Println("State Processor Goroutine shutting down.")
			return
		}
	}
}