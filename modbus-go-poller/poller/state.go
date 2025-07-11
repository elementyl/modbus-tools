package poller

import (
	"fmt"
	"modbus-tools/modbus-go-poller/config" // Ensure this path is correct!
	"modbus-tools/modbus-go-poller/database" // Ensure this path is correct!
	"sync"
	"time"
)

// Command structs
type SetBitCmd struct{ Addr uint16; Bit uint; Val bool }
type WriteEngCmd struct{ Addr uint16; EngVal float64 }

// Data-only structs
type TxRxInfo struct {
	Timestamp string
	Hex       string
	Count     uint64
}
type TimingInfo struct{ RoundTripTimeMs float64 }
type ActiveAlarm struct{ Severity string; Message string }

// PollerState holds the application's live data.
type PollerState struct {
	mu                sync.Mutex
	Config            *config.AppConfig
	CurrentRegisters  map[uint16]uint16
	PreviousRegisters map[uint16]uint16
	ActiveAlarms      map[string]ActiveAlarm
	LastTx            TxRxInfo
	LastRx            TxRxInfo
	Timing            TimingInfo
	Status            string
	CommandChan       chan interface{}
	DBEventChan       chan<- database.Event
	WriteSuppressions map[string]string
	lastHeartbeatTime uint32
}

func NewPollerState(dbEventChan chan<- database.Event, appConfig *config.AppConfig) *PollerState {
	ps := &PollerState{
		Config:            appConfig,
		CurrentRegisters:  make(map[uint16]uint16),
		PreviousRegisters: make(map[uint16]uint16),
		ActiveAlarms:      make(map[string]ActiveAlarm),
		Status:            "Initializing...",
		CommandChan:       make(chan interface{}, 10),
		DBEventChan:       dbEventChan,
		WriteSuppressions: make(map[string]string),
		lastHeartbeatTime: 0,
	}
	for addr := range appConfig.PointsByAddress {
		ps.CurrentRegisters[addr] = 0
		ps.PreviousRegisters[addr] = 0
	}
	return ps
}

// NOTE: LoadConfigurationFromDB, ScaleValue, and UnscaleValue have been removed from this file.
// They now live in loader.go and utils.go respectively.

func (ps *PollerState) SendCommand(cmd interface{}) { ps.CommandChan <- cmd }

func (ps *PollerState) UpdateHeartbeat(high, low uint16, t uint32) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.CurrentRegisters[40008] = high
	ps.CurrentRegisters[40009] = low
	ps.lastHeartbeatTime = t
}

func (ps *PollerState) GetSnapshot() (map[uint16]uint16, map[uint16]uint16, map[string]ActiveAlarm, TxRxInfo, TxRxInfo, TimingInfo, string) {
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
	alarms := make(map[string]ActiveAlarm)
	for k, v := range ps.ActiveAlarms {
		alarms[k] = v
	}
	return current, previous, alarms, ps.LastTx, ps.LastRx, ps.Timing, ps.Status
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