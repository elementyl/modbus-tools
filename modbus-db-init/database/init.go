package database

import (
	"database/sql"
	"log"
)

// --- Structs ---
type PointDefinition struct {
	PointName   string
	Acronym     string
	IOType      string
	IONumber    int
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

// --- SQL Schema ---
const createPointDefsSQL = `
CREATE TABLE point_definitions (
    point_name TEXT PRIMARY KEY,
    acronym TEXT,
    io_type TEXT,
    io_number INTEGER,
    modbus_address INTEGER NOT NULL,
    modbus_bit INTEGER,
    point_type TEXT NOT NULL,
    data_type TEXT NOT NULL DEFAULT 'unsigned',
    units TEXT,
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

// --- Data Map ---
// In modbus-db-init/database/init.go

var REG_MAP = []struct {
	Address  uint16
	Type     string
	Name     string
	Acronym  string
	IOType   string
	IONumber int
	DataType string
	Unit     string
	Scaling  *ScalingParams
	Points   map[uint]PointDefinition
	Alarms   []AlarmDefinition
}{
	// --- CONTROL REGISTERS ---
	{Address: 40001, Type: "bitmap", Points: map[uint]PointDefinition{
		0: {PointName: "Start", Acronym: "PW1-START", IOType: "DO", IONumber: 1, NormalState: uintPtr(0), StateOn: "ON", StateOff: "OFF"},
		1: {PointName: "Stop", Acronym: "PW1-STOP", IOType: "DO", IONumber: 2, NormalState: uintPtr(0), StateOn: "ON", StateOff: "OFF"},
		2: {PointName: "CMD: Auto Mode", Acronym: "PW1-AUTO-ENA", IOType: "DO", IONumber: 3, NormalState: uintPtr(0), StateOn: "AUTO", StateOff: "MANUAL"},
	}},
	{Address: 40002, Type: "bitmap", Points: map[uint]PointDefinition{
		0: {PointName: "Chlorinator Pump Enabled", Acronym: "PW1-TS-DOS-SS", IOType: "DO", IONumber: 17, NormalState: uintPtr(0), StateOn: "ENABLED", StateOff: "DISABLED"},
	}},
	// ... Analog points are unchanged ...
	{Address: 40003, Type: "analog", Name: "Tank Level", Acronym: "PW1-TANK-SP", IOType: "AO", IONumber: 3, DataType: "unsigned", Unit: "ft", Scaling: &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 231}},
	{Address: 40004, Type: "analog", Name: "Start SP", Acronym: "PW1-START-SP", IOType: "AO", IONumber: 4, DataType: "unsigned", Unit: "ft", Scaling: &ScalingParams{RawLow: 0, RawHigh: 231, EngLow: 0, EngHigh: 231}},
	{Address: 40005, Type: "analog", Name: "Stop SP", Acronym: "PW1-STOP-SP", IOType: "AO", IONumber: 5, DataType: "unsigned", Unit: "ft", Scaling: &ScalingParams{RawLow: 0, RawHigh: 231, EngLow: 0, EngHigh: 231}},
	{Address: 40008, Type: "analog", Name: "SCADA Clock", IOType: "AO", IONumber: 8, DataType: "unsigned"},
	{Address: 40009, Type: "analog", Name: "SCADA Clock (low)", IOType: "AO", IONumber: 9, DataType: "unsigned"},

	// --- STATUS REGISTERS ---
	{Address: 41001, Type: "bitmap", Points: map[uint]PointDefinition{
		0:  {PointName: "Motor Running", Acronym: "PW1-RUN", IOType: "DI", IONumber: 1, NormalState: uintPtr(0), StateOn: "Running", StateOff: "Stopped"},
		1:  {PointName: "STATUS: Auto Mode", Acronym: "PW1-AUTO-ENA", IOType: "DI", IONumber: 2, NormalState: uintPtr(0), StateOn: "Auto", StateOff: "Manual"},
		2:  {PointName: "Lockout", Acronym: "PW1-LOCKED", IOType: "DI", IONumber: 3, NormalState: uintPtr(0), StateOn: "LOCKOUT", StateOff: "OK"},
		3:  {PointName: "Backspin", Acronym: "PW1-BACKSPIN", IOType: "DI", IONumber: 4, NormalState: uintPtr(0), StateOn: "Active", StateOff: "OK"},
		4:  {PointName: "Drawdown Level Alarm", Acronym: "PW1-DRAW-ALM", IOType: "DI", IONumber: 5, NormalState: uintPtr(0), StateOn: "ALARM", StateOff: "OK"},
		5:  {PointName: "No Flow", Acronym: "PW1-FL-ALM", IOType: "DI", IONumber: 6, NormalState: uintPtr(1), StateOn: "Flow OK", StateOff: "No Flow"},
		6:  {PointName: "High Discharge Pressure", Acronym: "PW1-HPRS-ALM", IOType: "DI", IONumber: 7, NormalState: uintPtr(0), StateOn: "High Pressure", StateOff: "OK"},
		7:  {PointName: "Building Intrusion", Acronym: "PW1-INTRUSION", IOType: "DI", IONumber: 8, NormalState: uintPtr(1), StateOn: "Secure", StateOff: "INTRUSION"},
		8:  {PointName: "PLC Intrusion", Acronym: "PW1-DOOR", IOType: "DI", IONumber: 9, NormalState: uintPtr(1), StateOn: "Secure", StateOff: "INTRUSION"},
		9:  {PointName: "AC Power Fail", Acronym: "PW1-PWR-FAIL", IOType: "DI", IONumber: 10, NormalState: uintPtr(1), StateOn: "OK", StateOff: "FAILED"},
		10: {PointName: "PLC Fault", Acronym: "PW1-PLC-FAULT", IOType: "DI", IONumber: 11, NormalState: uintPtr(0), StateOn: "FAULT", StateOff: "OK"},
		11: {PointName: "Starter Fault", Acronym: "PW1-STARTER-FAULT", IOType: "DI", IONumber: 12, NormalState: uintPtr(0), StateOn: "FAULT", StateOff: "OK"},
		12: {PointName: "Flow Transmitter Alarm", Acronym: "PW1-FLOW-ALARM", IOType: "DI", IONumber: 13, NormalState: uintPtr(0), StateOn: "ALARM", StateOff: "OK"},
	}},
	{Address: 41002, Type: "bitmap", Points: map[uint]PointDefinition{
		4:  {PointName: "East Door Intrusion", Acronym: "PW1-TS-DOOR1", IOType: "DI", IONumber: 21, NormalState: uintPtr(0), StateOn: "Secure", StateOff: "INTRUSION"},
		5:  {PointName: "North Door Intrusion", Acronym: "PW1-TS-DOOR2", IOType: "DI", IONumber: 22, NormalState: uintPtr(0), StateOn: "Secure", StateOff: "INTRUSION"},
		6:  {PointName: "MicroChlor Running", Acronym: "PW1-TS-CL2-RUN", IOType: "DI", IONumber: 23, NormalState: uintPtr(0), StateOn: "Running", StateOff: "Stopped"},
		7:  {PointName: "MicroChlor Estop", Acronym: "PW1-WELL-ESTOP", IOType: "DI", IONumber: 24, NormalState: uintPtr(0), StateOn: "E-STOP", StateOff: "OK"},
		8:  {PointName: "Chlorinator Pump Running", Acronym: "PW1-TS-DOS-RUN", IOType: "DI", IONumber: 25, NormalState: uintPtr(0), StateOn: "Running", StateOff: "Stopped"},
		13: {PointName: "Pump ON Command", Acronym: "PW1-START", IOType: "DI", IONumber: 14, NormalState: uintPtr(0), StateOn: "Active", StateOff: "Inactive"},
		14: {PointName: "Pump OFF Command", Acronym: "PW1-STOP", IOType: "DI", IONumber: 15, NormalState: uintPtr(0), StateOn: "Active", StateOff: "Inactive"},
		15: {PointName: "Pump Auto Command", Acronym: "PW1-AUTO-ENA", IOType: "DI", IONumber: 16, NormalState: uintPtr(0), StateOn: "Active", StateOff: "Inactive"},
	}},
	// ... rest of analog points are unchanged ...
	{Address: 41003, Type: "analog", Name: "Flow", Acronym: "PW1-FLOW", IOType: "AI", IONumber: 3, DataType: "signed", Unit: "gpm", Scaling: &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 1500}},
	{Address: 41004, Type: "analog", Name: "Drawdown", Acronym: "PW1-DRAW-LVL", IOType: "AI", IONumber: 4, DataType: "signed", Unit: "ft", Scaling: &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 231}},
	{Address: 41005, Type: "analog", Name: "Discharge Pressure", Acronym: "PW1-DIS-PRES", IOType: "AI", IONumber: 5, DataType: "signed", Unit: "psi", Scaling: &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 200}},
	{Address: 41006, Type: "analog", Name: "Chlorine Flow", Acronym: "PW1-TS-FLOW", IOType: "AI", IONumber: 6, DataType: "signed", Unit: "gpm", Scaling: &ScalingParams{RawLow: 0, RawHigh: 30840, EngLow: 0, EngHigh: 4000}},
	{Address: 41007, Type: "analog", Name: "Pump Start SP", Acronym: "PW1-START-SP", IOType: "AI", IONumber: 7, DataType: "unsigned", Unit: "ft", Scaling: &ScalingParams{RawLow: 0, RawHigh: 231, EngLow: 0, EngHigh: 231}},
	{Address: 41008, Type: "analog", Name: "Pump Stop SP", Acronym: "PW1-STOP-SP", IOType: "AI", IONumber: 8, DataType: "unsigned", Unit: "ft", Scaling: &ScalingParams{RawLow: 0, RawHigh: 231, EngLow: 0, EngHigh: 231}},
	{Address: 41009, Type: "analog", Name: "PLC Heartbeat", Acronym: "PW1-HBEAT", IOType: "AI", IONumber: 9, DataType: "unsigned", Unit: "seconds", Scaling: &ScalingParams{RawLow: 0, RawHigh: 59, EngLow: 0, EngHigh: 59}},
}

func uintPtr(u uint) *uint {
	return &u
}

// CreateAndPopulate creates the database schema and fills it with the default configuration.
func CreateAndPopulate(db *sql.DB) error {
	log.Println("Creating database schema...")
	if _, err := db.Exec(createPointDefsSQL); err != nil {
		return err
	}
	if _, err := db.Exec(createAlarmDefsSQL); err != nil {
		return err
	}
	log.Println("Schema created successfully.")

	log.Println("Populating database with default point and alarm definitions...")
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	pointStmt, err := tx.Prepare(`INSERT INTO point_definitions(point_name, acronym, io_type, io_number, modbus_address, modbus_bit, point_type, data_type, units, normal_state, state_on, state_off, scaling_raw_low, scaling_raw_high, scaling_eng_low, scaling_eng_high, log_events) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer pointStmt.Close()

	alarmStmt, err := tx.Prepare(`INSERT INTO alarm_definitions(point_name, alarm_type, limit_value, severity, message) VALUES(?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer alarmStmt.Close()

	pointCount := 0
	for _, regDef := range REG_MAP {
		if regDef.Type == "analog" {
			var sc_rl, sc_rh, sc_el, sc_eh *float64
			if regDef.Scaling != nil {
				sc_rl, sc_rh, sc_el, sc_eh = &regDef.Scaling.RawLow, &regDef.Scaling.RawHigh, &regDef.Scaling.EngLow, &regDef.Scaling.EngHigh
			}
			_, err := pointStmt.Exec(
				regDef.Name,       // 1-point_name
				regDef.Acronym,    // 2-acronym
				regDef.IOType,     // 3-io_type
				regDef.IONumber,   // 4-io_number
				regDef.Address,    // 5-modbus_address
				nil,               // 6-modbus_bit
				"analog",          // 7-point_type
				regDef.DataType,   // 8-data_type
				regDef.Unit,       // 9-units
				nil,               // 10-normal_state
				nil,               // 11-state_on
				nil,               // 12-state_off
				sc_rl,             // 13-scaling_raw_low
				sc_rh,             // 14-scaling_raw_high
				sc_el,             // 15-scaling_eng_low
				sc_eh,             // 16-scaling_eng_high
				true,              // 17-log_events
			)
			if err != nil {
				log.Printf("WARNING: Failed to insert analog point %s: %v. Skipping.", regDef.Name, err)
				continue
			}
			pointCount++
		} else { // bitmap
			for bit, pointDef := range regDef.Points {
				_, err := pointStmt.Exec(
					pointDef.PointName,      // 1-point_name
					pointDef.Acronym,        // 2-acronym
					pointDef.IOType,         // 3-io_type
					pointDef.IONumber,       // 4-io_number
					regDef.Address,          // 5-modbus_address
					bit,                     // 6-modbus_bit
					"bitmap",                // 7-point_type
					"unsigned",              // 8-data_type
					nil,                     // 9-units
					pointDef.NormalState,    // 10-normal_state
					pointDef.StateOn,        // 11-state_on
					pointDef.StateOff,       // 12-state_off
					nil,                     // 13-scaling_raw_low
					nil,                     // 14-scaling_raw_high
					nil,                     // 15-scaling_eng_low
					nil,                     // 16-scaling_eng_high
					true,                    // 17-log_events
				)
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
		return err
	}
	log.Printf("Database population completed. Inserted %d points.", pointCount)
	return nil
}