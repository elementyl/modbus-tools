package config

// --- Configuration Structs ---
type PollGroup struct {
	StartAddress uint16
	Count        uint16
}

type PointDefinition struct {
	PointName   string
	Address     uint16
	Bit         *uint
	Type        string
	DataType    string // 'unsigned' or 'signed'
	Unit        string
	NormalState *uint
	StateOn     string
	StateOff    string
	Scaling     *ScalingParams
	LogEvents   bool
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
	AlarmsByPoint   map[string][]*AlarmDefinition
}

// --- Configuration Constants ---
const (
	DefaultSerialPort    = "COM2"
	DefaultTCPServerHost = "127.0.0.1"
	DefaultTCPServerPort = 5020
	DefaultSlaveID       = 2
	DefaultPollIntervalS = 1.0
)

// The REG_MAP and uintPtr function have been moved to the new modbus-db-init tool.