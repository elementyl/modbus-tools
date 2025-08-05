// ===== C:\Projects\modbus-tools\modbus-go-poller\config\config.go =====
package config

type PollGroup struct {
	StartAddress uint16
	Count        uint16
}

type PointDefinition struct {
	PointName            string
	Acronym              string
	IOType               string
	IONumber             int
	Address              uint16
	Bit                  *uint
	Type                 string
	DataType             string
	Unit                 string
	NormalState          *uint
	StateOn              string
	StateOff             string
	Scaling              *ScalingParams
	LogEvents            bool
	PulseOnStart         bool  // NEW: True if start should pulse
	PulseDurationTenths  int   // NEW: Pulse duration in tenths of second (0 = default 10)
}

type ScalingParams struct {
	RawLow, RawHigh, EngLow, EngHigh float64
}

type AlarmDefinition struct {
	PointName string
	Type      string
	Limit     float64
	Severity  string
	Message   string
}

type AppConfig struct {
	PointsByAddress map[uint16][]*PointDefinition
	PointsByName    map[string]*PointDefinition
	PointsByAcronym map[string][]*PointDefinition
	AlarmsByPoint   map[string][]*AlarmDefinition
	ByIOTypeNumber  map[string]map[int]*PointDefinition
}

const (
	DefaultSerialPort     = "COM2"
	DefaultTCPServerHost  = "127.0.0.1"
	DefaultTCPServerPort  = 5020
	DefaultSlaveID        = 2
	DefaultPollIntervalS  = 1.0
)