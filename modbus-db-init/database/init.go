package database

import (
	"database/sql"
	"fmt"
	"log"
)

// --- Structs moved from poller's config package ---
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

// --- SQL Schema moved from poller's database/init.go ---
const createPointDefsSQL = `
CREATE TABLE point_definitions (
    point_name TEXT PRIMARY KEY,
    modbus_address INTEGER NOT NULL,
    modbus_bit INTEGER,
    point_type TEXT NOT NULL,
    data_type TEXT NOT NULL DEFAULT 'unsigned',
    units TEXT,
    cos_tolerance REAL DEFAULT 0.0,
    report_interval_seconds INTEGER DEFAULT 0,
    normal_state INTEGER,
    state_on TEXT,
    state_off TEXT,
    scaling_raw_low REAL,
    scaling_raw_high REAL,
    scaling_eng_low REAL,
    scaling_eng_high REAL,
    log_events INTEGER NOT NULL DEFAULT 1
);`

const createAlarmDefsSQL = `
CREATE TABLE alarm_definitions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    point_name TEXT NOT NULL,
    alarm_type TEXT NOT NULL,
    limit_value REAL,
    severity TEXT NOT NULL,
    message TEXT NOT NULL,
    FOREIGN KEY (point_name) REFERENCES point_definitions (point_name)
);`

// --- Data moved from poller's config/config.go ---
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
	// --- MODIFIED REGISTER 40001 ---
	{Address: 40001, Type: "bitmap", Points: map[uint]PointDefinition{
		0: {PointName: "Start", NormalState: uintPtr(0), StateOff: "Inactive", StateOn: "Active", LogEvents: true},
		1: {PointName: "Stop", NormalState: uintPtr(0), StateOff: "Inactive", StateOn: "Active", LogEvents: true},
		2: {PointName: "Mode Auto/Manual", NormalState: uintPtr(1), StateOff: "Manual", StateOn: "Auto", LogEvents: true},
		// 3: {PointName: "Chlorinator Pump Enabled", ...} // -- REMOVED from here
	}},

	// --- NEW BITMAP REGISTER 40002 ---
	{Address: 40002, Type: "bitmap", Points: map[uint]PointDefinition{
		0: {PointName: "Chlorinator Pump Enabled", NormalState: uintPtr(1), StateOff: "Disabled", StateOn: "Enabled", LogEvents: true},
	}},

	// --- SHIFTED ANALOG REGISTERS ---
	{Address: 40003, Type: "analog", Name: "Tank Level SP", DataType: "unsigned", Unit: "ft", Scaling: &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 44}, LogEvents: true}, // Was 40002
	{Address: 40004, Type: "analog", Name: "Start SP", DataType: "unsigned", Unit: "ft", LogEvents: true},                                                                                         // Was 40003
	{Address: 40005, Type: "analog", Name: "Stop SP", DataType: "unsigned", Unit: "ft", LogEvents: true},                                                                                          // Was 40004
	{Address: 40008, Type: "analog", Name: "Poller Heartbeat High", DataType: "unsigned", LogEvents: false},
	{Address: 40009, Type: "analog", Name: "Poller Heartbeat Low", DataType: "unsigned", LogEvents: false},
	{
		Address: 41001, Type: "bitmap", Points: map[uint]PointDefinition{
			0:  {PointName: "Motor Running", NormalState: uintPtr(0), StateOff: "Stopped", StateOn: "Running", LogEvents: true},
			1:  {PointName: "Status Auto/Manual", NormalState: uintPtr(1), StateOff: "Manual", StateOn: "Auto", LogEvents: true},
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

// CreateAndPopulate creates the database schema and fills it with the default configuration.
func CreateAndPopulate(db *sql.DB) error {
	log.Println("Creating database schema...")
	if _, err := db.Exec(createPointDefsSQL); err != nil {
		return fmt.Errorf("could not create point_definitions table: %w", err)
	}
	if _, err := db.Exec(createAlarmDefsSQL); err != nil {
		return fmt.Errorf("could not create alarm_definitions table: %w", err)
	}
	log.Println("Schema created successfully.")

	log.Println("Populating database with default point and alarm definitions...")
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("could not begin transaction: %w", err)
	}

	pointStmt, err := tx.Prepare(`INSERT INTO point_definitions(point_name, modbus_address, modbus_bit, point_type, data_type, units, normal_state, state_on, state_off, scaling_raw_low, scaling_raw_high, scaling_eng_low, scaling_eng_high, log_events) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("could not prepare point statement: %w", err)
	}
	defer pointStmt.Close()

	alarmStmt, err := tx.Prepare(`INSERT INTO alarm_definitions(point_name, alarm_type, limit_value, severity, message) VALUES(?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("could not prepare alarm statement: %w", err)
	}
	defer alarmStmt.Close()

	pointCount := 0
	for _, regDef := range REG_MAP {
		if regDef.Type == "analog" {
			var sc_rl, sc_rh, sc_el, sc_eh *float64
			if regDef.Scaling != nil {
				// NOTE: Fixed a typo from the original code (&regDef -> regDef)
				sc_rl, sc_rh, sc_el, sc_eh = &regDef.Scaling.RawLow, &regDef.Scaling.RawHigh, &regDef.Scaling.EngLow, &regDef.Scaling.EngHigh
			}
			_, err := pointStmt.Exec(regDef.Name, regDef.Address, nil, "analog", regDef.DataType, regDef.Unit, nil, nil, nil, sc_rl, sc_rh, sc_el, sc_eh, regDef.LogEvents)
			if err != nil {
				log.Printf("WARNING: Failed to insert analog point %s: %v. Skipping.", regDef.Name, err)
				continue
			}
			pointCount++
		} else { // bitmap
			for bit, pointDef := range regDef.Points {
				// Bitmaps are always unsigned
				_, err := pointStmt.Exec(pointDef.PointName, regDef.Address, bit, "bitmap", "unsigned", nil, pointDef.NormalState, pointDef.StateOn, pointDef.StateOff, nil, nil, nil, nil, pointDef.LogEvents)
				if err != nil {
					log.Printf("WARNING: Failed to insert bitmap point %s: %v. Skipping.", pointDef.PointName, err)
					continue
				}
				pointCount++
			}
		}

		for _, alarm := range regDef.Alarms {
			pointName := alarm.PointName
			_, err := alarmStmt.Exec(pointName, alarm.Type, alarm.Limit, alarm.Severity, alarm.Message)
			if err != nil {
				log.Printf("WARNING: Failed to insert alarm for point %s: %v. Skipping.", pointName, err)
				continue
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	log.Printf("Database population completed. Inserted %d points.", pointCount)
	return nil
}