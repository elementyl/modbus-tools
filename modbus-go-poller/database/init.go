package database

import (
	"database/sql"
	"log"
	"modbus-go-poller/config"
)

const createPointDefsSQL = `
CREATE TABLE IF NOT EXISTS point_definitions (
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
CREATE TABLE IF NOT EXISTS alarm_definitions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    point_name TEXT NOT NULL,
    alarm_type TEXT NOT NULL,
    limit_value REAL,
    severity TEXT NOT NULL,
    message TEXT NOT NULL,
    FOREIGN KEY (point_name) REFERENCES point_definitions (point_name)
);`

// InitDatabase ensures the database schema is up to date and populates it on first run.
func InitDatabase(db *sql.DB, logger *log.Logger) error {
	logger.Println("Initializing database schema...")
	if _, err := db.Exec(createPointDefsSQL); err != nil {
		return err
	}
	if _, err := db.Exec(createAlarmDefsSQL); err != nil {
		return err
	}

	var pointCount int
	err := db.QueryRow("SELECT COUNT(*) FROM point_definitions").Scan(&pointCount)
	if err != nil {
		return err
	}

	if pointCount == 0 {
		logger.Println("No point definitions found in database. Performing one-time migration from config file...")
		tx, err := db.Begin()
		if err != nil {
			return err
		}

		pointStmt, err := tx.Prepare(`INSERT INTO point_definitions(point_name, modbus_address, modbus_bit, point_type, data_type, units, normal_state, state_on, state_off, scaling_raw_low, scaling_raw_high, scaling_eng_low, scaling_eng_high, log_events) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
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

		for _, regDef := range config.REG_MAP {
			if regDef.Type == "analog" {
				var sc_rl, sc_rh, sc_el, sc_eh *float64
				if regDef.Scaling != nil {
					sc_rl, sc_rh, sc_el, sc_eh = &regDef.Scaling.RawLow, &regDef.Scaling.RawHigh, &regDef.Scaling.EngLow, &regDef.Scaling.EngHigh
				}
				_, err := pointStmt.Exec(regDef.Name, regDef.Address, nil, "analog", regDef.DataType, regDef.Unit, nil, nil, nil, sc_rl, sc_rh, sc_el, sc_eh, regDef.LogEvents)
				if err != nil {
					logger.Printf("Failed to insert analog point %s: %v", regDef.Name, err)
					continue
				}
			} else { // bitmap
				for bit, pointDef := range regDef.Points {
					// Bitmaps are always unsigned
					_, err := pointStmt.Exec(pointDef.PointName, regDef.Address, bit, "bitmap", "unsigned", nil, pointDef.NormalState, pointDef.StateOn, pointDef.StateOff, nil, nil, nil, nil, pointDef.LogEvents)
					if err != nil {
						logger.Printf("Failed to insert bitmap point %s: %v", pointDef.PointName, err)
						continue
					}
				}
			}

			for _, alarm := range regDef.Alarms {
				pointName := alarm.PointName
				_, err := alarmStmt.Exec(pointName, alarm.Type, alarm.Limit, alarm.Severity, alarm.Message)
				if err != nil {
					logger.Printf("Failed to insert alarm for point %s: %v", pointName, err)
					continue
				}
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		logger.Println("Point and alarm migration completed successfully.")
	}

	logger.Println("Database schema is up to date.")
	return nil
}