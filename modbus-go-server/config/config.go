package config

import (
	"math"
	"strings"
)

// --- Configuration Constants ---
const (
	TCPServerHost    = "127.0.0.1"
	TCPServerPort    = 5020
	SerialServerPort = "COM5" // Default serial port for the server
	SlaveID          = 2
)

// --- Register Map Definition ---

type ScalingParams struct {
	RawLow  float64
	RawHigh float64
	EngLow  float64
	EngHigh float64
}

type RegisterDefinition struct {
	Address uint16
	Type    string
	Name    string
	Unit    string
	Scaling *ScalingParams
	Points  map[uint]string
}

var REG_MAP = []RegisterDefinition{
	{Address: 40001, Type: "bitmap", Name: "Pump Station Commands", Points: map[uint]string{0: "Start", 1: "Stop", 2: "Auto", 3: "Chlorinator Pump Enabled"}},
	{Address: 40002, Type: "analog", Name: "Tank Level SP", Unit: "ft", Scaling: &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 44}},
	{Address: 40003, Type: "analog", Name: "Start SP", Unit: "ft"},
	{Address: 40004, Type: "analog", Name: "Stop SP", Unit: "ft"},
	{Address: 41001, Type: "bitmap", Name: "Pump Station Status", Points: map[uint]string{0: "Motor Running", 1: "Auto", 2: "Lockout", 3: "Backspin", 4: "Drawdown Level Alarm", 5: "No Flow", 6: "High Discharge Pressure", 7: "Building Intrusion", 8: "PLC Intrusion", 9: "AC Power Fail", 10: "PLC Alarm", 11: "MCC Alarm"}},
	{Address: 41002, Type: "bitmap", Name: "Chlorinator Status", Points: map[uint]string{4: "East Door Intrusion", 5: "North Door Intrusion", 6: "MicroChlor Running", 7: "MicroChlor Estop", 8: "Chlorinator Pump Running", 13: "PanelView Stop Command", 14: "PanelView Start Command", 15: "PanelView Auto Command"}},
	{Address: 41003, Type: "analog", Name: "Flow", Unit: "GPM", Scaling: &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 1500}},
	{Address: 41004, Type: "analog", Name: "Drawdown", Unit: "ft", Scaling: &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 231}},
	{Address: 41005, Type: "analog", Name: "Discharge Pressure", Unit: "PSI", Scaling: &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 200}},
	{Address: 41006, Type: "analog", Name: "Chlorine Flow", Unit: "GPM", Scaling: &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 4000}},
	{Address: 41007, Type: "analog", Name: "PanelView Start SP", Unit: "ft"},
	{Address: 41008, Type: "analog", Name: "PanelView Stop SP", Unit: "ft"},
	{Address: 41009, Type: "analog", Name: "Heartbeat", Unit: "counts"},
}

// --- Helper Functions ---

func GetRegisterDefinition(addr uint16) *RegisterDefinition {
	for _, r := range REG_MAP {
		if r.Address == addr {
			return &r
		}
	}
	return nil
}

func FindPointByName(name string) (uint16, uint, bool) {
	for _, r := range REG_MAP {
		if r.Type == "bitmap" {
			for bit, pName := range r.Points {
				if strings.EqualFold(pName, name) {
					return r.Address, bit, true
				}
			}
		}
	}
	return 0, 0, false
}

func ScaleValue(rawVal uint16, p *ScalingParams) float64 {
	if p == nil {
		return float64(rawVal) // No scaling defined
	}
	rawRange := p.RawHigh - p.RawLow
	if rawRange == 0 {
		return p.EngLow
	}
	return p.EngLow + ((float64(rawVal) - p.RawLow) / rawRange) * (p.EngHigh - p.EngLow)
}

func UnscaleValue(engVal float64, p *ScalingParams) uint16 {
	if p == nil {
		return uint16(engVal)
	}
	engRange := p.EngHigh - p.EngLow
	if engRange == 0 {
		return uint16(p.RawLow)
	}
	return uint16(math.Round(p.RawLow + ((engVal - p.EngLow) / engRange) * (p.RawHigh - p.RawLow)))
}