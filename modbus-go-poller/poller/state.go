// ===== C:\Projects\modbus-tools\modbus-go-poller\poller\state.go =====
package poller

import (
	"fmt"
	"modbus-tools/modbus-go-poller/config"
	"modbus-tools/modbus-go-poller/database"
	"sync"
	"time"
)

// IO_Result carries the raw result of an I/O operation to avoid locking the state in the IO loop.
type IO_Result struct {
	TxTime time.Time
	RTT    float64
	Tx     []byte
	Rx     []byte
}

type SetBitCmd struct{ Addr uint16; Bit uint; Val bool }
type WriteEngCmd struct{ Addr uint16; EngVal float64 }
type WriteRawCmd struct{ Addr uint16; Value uint16 }
type PulseDOUCmd struct {
	Addr     uint16
	Bit      uint
	Duration time.Duration
	Acronym  string
}

type TxRxInfo struct {
	Timestamp string
	Hex       string
	Count     uint64
}
type TimingInfo struct{ RoundTripTimeMs float64 }
type ActiveAlarm struct{ Severity string; Message string }

type PollerState struct {
	mu                sync.Mutex
	Config            *config.AppConfig
	CurrentRegisters  map[uint16]uint16
	PreviousRegisters map[uint16]uint16
	LastChange        map[uint16]time.Time
	ActiveAlarms      map[string]ActiveAlarm
	LastTx            TxRxInfo
	LastRx            TxRxInfo
	Timing            TimingInfo
	Status            string
	CommandChan       chan interface{}
	DBEventChan       chan<- database.Event
	IOResultChan      chan IO_Result // Bidirectional for sender (IO) and receiver (StateProcessor)
	lastHeartbeatTime uint32
}

func NewPollerState(dbEventChan chan<- database.Event, ioResultChan chan IO_Result, appConfig *config.AppConfig) *PollerState {
	ps := &PollerState{
		Config:            appConfig,
		CurrentRegisters:  make(map[uint16]uint16),
		PreviousRegisters: make(map[uint16]uint16),
		LastChange:        make(map[uint16]time.Time),
		ActiveAlarms:      make(map[string]ActiveAlarm),
		Status:            "Initializing...",
		CommandChan:       make(chan interface{}, 10),
		DBEventChan:       dbEventChan,
		IOResultChan:      ioResultChan,
		lastHeartbeatTime: 0,
	}
	for addr := range appConfig.PointsByAddress {
		ps.CurrentRegisters[addr] = 0
		ps.PreviousRegisters[addr] = 0
	}
	return ps
}

func (ps *PollerState) SendCommand(cmd interface{}) {
	ps.CommandChan <- cmd
}

func (ps *PollerState) UpdateHeartbeat(high, low uint16, t uint32) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.CurrentRegisters[40008] = high
	ps.CurrentRegisters[40009] = low
	ps.lastHeartbeatTime = t
}

func (ps *PollerState) GetSnapshot() (map[uint16]uint16, map[uint16]uint16, map[uint16]time.Time, map[string]ActiveAlarm, TxRxInfo, TxRxInfo, TimingInfo, string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	current := make(map[uint16]uint16)
	for k, v := range ps.CurrentRegisters {
		current[k] = v
	}
	previous := make(map[uint16]uint16)
	for k, v := range ps.PreviousRegisters {
		previous[k] = v
	}
	lastChange := make(map[uint16]time.Time)
	for k, v := range ps.LastChange {
		lastChange[k] = v
	}
	alarms := make(map[string]ActiveAlarm)
	for k, v := range ps.ActiveAlarms {
		alarms[k] = v
	}
	return current, previous, lastChange, alarms, ps.LastTx, ps.LastRx, ps.Timing, ps.Status
}

func (ps *PollerState) UpdateFromPoll(newData map[uint16]uint16) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for k, v := range newData {
		ps.CurrentRegisters[k] = v
	}
}

func (ps *PollerState) SetStatus(status string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.Status = status
}

func (ps *PollerState) CommitState() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for k, v := range ps.CurrentRegisters {
		ps.PreviousRegisters[k] = v
	}
}

func (ps *PollerState) UpdateTx(data []byte, txTime time.Time) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.LastTx.Timestamp = txTime.Format("15:04:05.000")
	ps.LastTx.Hex = fmt.Sprintf("%X", data)
	ps.LastTx.Count++
}

func (ps *PollerState) UpdateRx(data []byte, rtt float64, txTime time.Time) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	rxTime := txTime.Add(time.Duration(rtt * float64(time.Millisecond)))
	ps.LastRx.Timestamp = rxTime.Format("15:04:05.000")
	ps.LastRx.Hex = fmt.Sprintf("%X", data)
	ps.LastRx.Count++
	ps.Timing.RoundTripTimeMs = rtt
}

func (ps *PollerState) SetAlarms(newAlarms map[string]ActiveAlarm) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.ActiveAlarms = newAlarms
}