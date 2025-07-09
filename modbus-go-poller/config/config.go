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

// --- MIGRATION-ONLY DATA ---
var REG_MAP = []struct {
	Address   uint16
	Type      string
	Name      string
	DataType  string
	Unit      string
	Scaling   *ScalingParams
	Points    map[uint]PointDefinition
	Alarms    []AlarmDefinition
	LogEvents bool
}{
	{Address: 40001, Type: "bitmap", Points: map[uint]PointDefinition{
		0: {PointName: "Start", NormalState: uintPtr(0), StateOff: "Inactive", StateOn: "Active", LogEvents: true},
		1: {PointName: "Stop", NormalState: uintPtr(0), StateOff: "Inactive", StateOn: "Active", LogEvents: true},
		2: {PointName: "Auto", NormalState: uintPtr(1), StateOff: "Manual", StateOn: "Auto", LogEvents: true},
		3: {PointName: "Chlorinator Pump Enabled", NormalState: uintPtr(1), StateOff: "Disabled", StateOn: "Enabled", LogEvents: true},
	}},
	{Address: 40002, Type: "analog", Name: "Tank Level SP", DataType: "unsigned", Unit: "ft", Scaling: &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 44}, LogEvents: true},
	{Address: 40003, Type: "analog", Name: "Start SP", DataType: "unsigned", Unit: "ft", LogEvents: true},
	{Address: 40004, Type: "analog", Name: "Stop SP", DataType: "unsigned", Unit: "ft", LogEvents: true},
	{Address: 40008, Type: "analog", Name: "Poller Heartbeat High", DataType: "unsigned", LogEvents: false},
	{Address: 40009, Type: "analog", Name: "Poller Heartbeat Low", DataType: "unsigned", LogEvents: false},
	{
		Address: 41001, Type: "bitmap", Points: map[uint]PointDefinition{
			0:  {PointName: "Motor Running", NormalState: uintPtr(0), StateOff: "Stopped", StateOn: "Running", LogEvents: true},
			1:  {PointName: "Auto", NormalState: uintPtr(1), StateOff: "Manual", StateOn: "Auto", LogEvents: true},
			2:  {PointName: "Lockout", NormalState: uintPtr(0), StateOff: "OK", StateOn: "LOCKOUT", LogEvents: true},
			3:  {PointName: "Backspin", NormalState: uintPtr(0), StateOff: "OK", StateOn: "Active", LogEvents: true},
			4:  {PointName: "Drawdown Level Alarm", NormalState: uintPtr(0), StateOff: "OK", StateOn: "ALARM", LogEvents: true},
			5:  {PointName: "No Flow", NormalState: uintPtr(1), StateOff: "No Flow", StateOn: "Flow OK", LogEvents: true},
			6:  {PointName: "High Discharge Pressure", NormalState: uintPtr(0), StateOff: "OK", StateOn: "High Pressure", LogEvents: true},
			7:  {PointName: "Building Intrusion", NormalState: uintPtr(1), StateOff: "INTRUSION", StateOn: "Secure", LogEvents: true},
			8:  {PointName: "PLC Intrusion", NormalState: uintPtr(1), StateOff: "INTRUSION", StateOn: "Secure", LogEvents: true},
			9:  {PointName: "AC Power Fail", NormalState: uintPtr(1), StateOff: "FAILED", StateOn: "OK", LogEvents: true},
			10: {PointName: "PLC Alarm", NormalState: uintPtr(0), StateOff: "OK", StateOn: "ALARM", LogEvents: true},
			11: {PointName: "MCC Alarm", NormalState: uintPtr(0), StateOff: "OK", StateOn: "ALARM", LogEvents: true},
		},
		Alarms: []AlarmDefinition{
			{PointName: "Lockout", Type: "on", Severity: "CRITICAL", Message: "PUMP LOCKOUT ACTIVE"},
			{PointName: "PLC Alarm", Type: "on", Severity: "WARNING", Message: "PLC Fault Detected"},
			{PointName: "AC Power Fail", Type: "on", Severity: "CRITICAL", Message: "AC Power Failure"},
		},
	},
	{Address: 41002, Type: "bitmap", Points: map[uint]PointDefinition{
		4:  {PointName: "East Door Intrusion", NormalState: uintPtr(1), StateOff: "INTRUSION", StateOn: "Secure", LogEvents: true},
		5:  {PointName: "North Door Intrusion", NormalState: uintPtr(1), StateOff: "INTRUSION", StateOn: "Secure", LogEvents: true},
		6:  {PointName: "MicroChlor Running", NormalState: uintPtr(0), StateOff: "Stopped", StateOn: "Running", LogEvents: true},
		7:  {PointName: "MicroChlor Estop", NormalState: uintPtr(0), StateOff: "OK", StateOn: "E-STOP", LogEvents: true},
		8:  {PointName: "Chlorinator Pump Running", NormalState: uintPtr(0), StateOff: "Stopped", StateOn: "Running", LogEvents: true},
		13: {PointName: "PanelView Stop Command", NormalState: uintPtr(0), StateOff: "Inactive", StateOn: "Active", LogEvents: true},
		14: {PointName: "PanelView Start Command", NormalState: uintPtr(0), StateOff: "Inactive", StateOn: "Active", LogEvents: true},
		15: {PointName: "PanelView Auto Command", NormalState: uintPtr(0), StateOff: "Inactive", StateOn: "Active", LogEvents: true},
	}},
	{Address: 41003, Type: "analog", Name: "Flow", DataType: "signed", Unit: "GPM", Scaling: &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 1500}, LogEvents: true},
	{Address: 41004, Type: "analog", Name: "Drawdown", DataType: "signed", Unit: "ft", Scaling: &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 231}, LogEvents: true},
	{
		Address:   41005, Type: "analog", Name: "Discharge Pressure", DataType: "signed", Unit: "PSI",
		Scaling:   &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 200},
		Alarms: []AlarmDefinition{
			{PointName: "Discharge Pressure", Type: "high", Limit: 190.0, Severity: "CRITICAL", Message: "Discharge Pressure Critically High"},
			{PointName: "Discharge Pressure", Type: "high", Limit: 175.0, Severity: "WARNING", Message: "Discharge Pressure High Warning"},
			{PointName: "Discharge Pressure", Type: "low", Limit: 50.0, Severity: "WARNING", Message: "Discharge Pressure Low Warning"},
		},
		LogEvents: true,
	},
	{Address: 41006, Type: "analog", Name: "Chlorine Flow", DataType: "signed", Unit: "GPM", Scaling: &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 4000}, LogEvents: true},
	{Address: 41007, Type: "analog", Name: "PanelView Start SP", DataType: "unsigned", Unit: "ft", LogEvents: true},
	{Address: 41008, Type: "analog", Name: "PanelView Stop SP", DataType: "unsigned", Unit: "ft", LogEvents: true},
	{Address: 41009, Type: "analog", Name: "Heartbeat", DataType: "unsigned", Unit: "counts", LogEvents: false},
}

func uintPtr(u uint) *uint {
	return &u
}