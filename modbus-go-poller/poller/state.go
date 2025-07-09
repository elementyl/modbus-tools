package poller

import (
	"database/sql"
	"fmt"
	"math"
	"modbus-go-poller/config"
	"modbus-go-poller/database"
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
type TimingInfo struct { RoundTripTimeMs float64 }
type ActiveAlarm struct { Severity string; Message  string }

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

func LoadConfigurationFromDB(db *sql.DB) (*config.AppConfig, error) {
	cfg := &config.AppConfig{
		PointsByAddress: make(map[uint16][]*config.PointDefinition),
		PointsByName:    make(map[string]*config.PointDefinition),
		AlarmsByPoint:   make(map[string][]*config.AlarmDefinition),
	}
	rows, err := db.Query("SELECT point_name, modbus_address, modbus_bit, point_type, data_type, units, normal_state, state_on, state_off, scaling_raw_low, scaling_raw_high, scaling_eng_low, scaling_eng_high, log_events FROM point_definitions")
	if err != nil {
		return nil, fmt.Errorf("failed to query point_definitions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		p := &config.PointDefinition{}
		var bit, normalState sql.NullInt64
		var unit, stateOn, stateOff, dataType sql.NullString
		var sc_rl, sc_rh, sc_el, sc_eh sql.NullFloat64
		if err := rows.Scan(&p.PointName, &p.Address, &bit, &p.Type, &dataType, &unit, &normalState, &stateOn, &stateOff, &sc_rl, &sc_rh, &sc_el, &sc_eh, &p.LogEvents); err != nil {
			return nil, err
		}
		p.DataType = dataType.String
		if bit.Valid {
			b := uint(bit.Int64)
			p.Bit = &b
		}
		if normalState.Valid {
			ns := uint(normalState.Int64)
			p.NormalState = &ns
		}
		p.Unit = unit.String
		p.StateOn = stateOn.String
		p.StateOff = stateOff.String
		if sc_rl.Valid && sc_rh.Valid && sc_el.Valid && sc_eh.Valid {
			p.Scaling = &config.ScalingParams{
				RawLow:  sc_rl.Float64,
				RawHigh: sc_rh.Float64,
				EngLow:  sc_el.Float64,
				EngHigh: sc_eh.Float64,
			}
		}
		cfg.PointsByAddress[p.Address] = append(cfg.PointsByAddress[p.Address], p)
		cfg.PointsByName[p.PointName] = p
	}
	rows.Close()
	rows, err = db.Query("SELECT point_name, alarm_type, limit_value, severity, message FROM alarm_definitions")
	if err != nil {
		return nil, fmt.Errorf("failed to query alarm_definitions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		a := &config.AlarmDefinition{}
		var limit sql.NullFloat64
		if err := rows.Scan(&a.PointName, &a.Type, &limit, &a.Severity, &a.Message); err != nil {
			return nil, err
		}
		if limit.Valid {
			a.Limit = limit.Float64
		}
		cfg.AlarmsByPoint[a.PointName] = append(cfg.AlarmsByPoint[a.PointName], a)
	}
	return cfg, nil
}

func ScaleValue(rawVal uint16, p *config.PointDefinition) float64 {
	if p.Scaling == nil {
		if p.DataType == "signed" {
			return float64(int16(rawVal))
		}
		return float64(rawVal)
	}
	var rawFloat float64
	if p.DataType == "signed" {
		rawFloat = float64(int16(rawVal))
	} else {
		rawFloat = float64(rawVal)
	}
	s := p.Scaling
	rawRange := s.RawHigh - s.RawLow
	if rawRange == 0 {
		return s.EngLow
	}
	return s.EngLow + ((rawFloat - s.RawLow) / rawRange) * (s.EngHigh - s.EngLow)
}

func UnscaleValue(engVal float64, p *config.PointDefinition) uint16 {
	if p.Scaling == nil {
		return uint16(engVal)
	}
	s := p.Scaling
	engRange := s.EngHigh - s.EngLow
	if engRange == 0 {
		return uint16(s.RawLow)
	}
	rawVal := s.RawLow + ((engVal - s.EngLow) / engRange) * (s.RawHigh - s.RawLow)
	if p.DataType == "signed" {
		if rawVal < -32768 {
			rawVal = -32768
		}
		if rawVal > 32767 {
			rawVal = 32767
		}
	} else {
		if rawVal < 0 {
			rawVal = 0
		}
		if rawVal > 65535 {
			rawVal = 65535
		}
	}
	return uint16(math.Round(rawVal))
}

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

// THIS METHOD WAS MISSING AND IS NOW RESTORED
func (ps *PollerState) SetAlarms(newAlarms map[string]ActiveAlarm) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.ActiveAlarms = newAlarms
}