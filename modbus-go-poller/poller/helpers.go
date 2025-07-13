package poller

import (
	"database/sql"
	"fmt"
	"math"
	"modbus-tools/modbus-go-poller/config"
)

func LoadConfigurationFromDB(db *sql.DB) (*config.AppConfig, error) {
	cfg := &config.AppConfig{
		PointsByAddress: make(map[uint16][]*config.PointDefinition),
		PointsByName:    make(map[string]*config.PointDefinition),
		PointsByAcronym: make(map[string][]*config.PointDefinition),
		AlarmsByPoint:   make(map[string][]*config.AlarmDefinition),
	}
	rows, err := db.Query("SELECT point_name, acronym, io_type, io_number, modbus_address, modbus_bit, point_type, data_type, units, normal_state, state_on, state_off, scaling_raw_low, scaling_raw_high, scaling_eng_low, scaling_eng_high, log_events FROM point_definitions")
	if err != nil {
		return nil, fmt.Errorf("failed to query point_definitions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		p := &config.PointDefinition{}
		var bit, normalState, ioNum sql.NullInt64
		var acronym, ioType, unit, stateOn, stateOff, dataType sql.NullString
		var sc_rl, sc_rh, sc_el, sc_eh sql.NullFloat64
		if err := rows.Scan(&p.PointName, &acronym, &ioType, &ioNum, &p.Address, &bit, &p.Type, &dataType, &unit, &normalState, &stateOn, &stateOff, &sc_rl, &sc_rh, &sc_el, &sc_eh, &p.LogEvents); err != nil {
			return nil, err
		}
		p.Acronym = acronym.String
		p.IOType = ioType.String
		if ioNum.Valid {
			p.IONumber = int(ioNum.Int64)
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
		if p.Acronym != "" {
			cfg.PointsByAcronym[p.Acronym] = append(cfg.PointsByAcronym[p.Acronym], p)
		}
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
	if p == nil || p.Scaling == nil {
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