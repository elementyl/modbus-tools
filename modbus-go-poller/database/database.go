package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Event represents a single loggable action or state change in the system.
type Event struct {
	Timestamp     time.Time
	PointName     string
	PreviousValue string
	NewValue      string
	Units         string
	EventType     string
}

const createTableSQL = `
CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,
    point_name TEXT NOT NULL,
    previous_value TEXT,
    new_value TEXT,
    units TEXT,
    event_type TEXT NOT NULL
);`

// DatabaseWriter is a long-running goroutine that listens for events and writes them to a daily SQLite database.
func DatabaseWriter(ctx context.Context, wg *sync.WaitGroup, eventChan <-chan Event, logger *log.Logger) {
	defer wg.Done()
	logger.Println("Database Writer Goroutine Started.")
	dbConnections := make(map[string]*sql.DB)
	defer func() {
		for _, db := range dbConnections {
			db.Close()
		}
		logger.Println("Database Writer Goroutine Shutting Down.")
	}()

	// writeEvent is a helper closure to avoid duplicating code.
	writeEvent := func(event Event) {
		dateStr := event.Timestamp.Format("2006-01-02")
		db, db_ok := dbConnections[dateStr]
		if !db_ok {
			var err error
			fileName := fmt.Sprintf("events_%s.db", dateStr)
			db, err = sql.Open("sqlite", fileName)
			if err != nil {
				logger.Printf("FATAL: Could not open/create database %s: %v", fileName, err)
				return // Can't write if we can't open DB
			}
			dbConnections[dateStr] = db

			_, err = db.Exec(createTableSQL)
			if err != nil {
				logger.Printf("FATAL: Could not create table in %s: %v", fileName, err)
				db.Close()
				delete(dbConnections, dateStr)
				return
			}
			logger.Printf("Successfully opened and verified database: %s", fileName)
		}

		stmt, err := db.Prepare("INSERT INTO events(timestamp, point_name, previous_value, new_value, units, event_type) VALUES(?, ?, ?, ?, ?, ?)")
		if err != nil {
			logger.Printf("ERROR: Failed to prepare SQL statement: %v", err)
			return
		}
		defer stmt.Close()

		timestampStr := event.Timestamp.Format("2006-01-02 15:04:05.000")
		_, err = stmt.Exec(timestampStr, event.PointName, event.PreviousValue, event.NewValue, event.Units, event.EventType)
		if err != nil {
			logger.Printf("ERROR: Failed to insert event into database: %v", err)
		}
	}

	for {
		select {
		case event, ok := <-eventChan:
			if !ok { // Channel has been closed from a clean shutdown
				return
			}
			writeEvent(event)

		case <-ctx.Done(): // Context was cancelled (e.g., by Ctrl-C)
			logger.Println("Shutdown signal received. Writing remaining events to database...")
			// Process any remaining events in the channel buffer before shutting down
			for len(eventChan) > 0 {
				event := <-eventChan
				writeEvent(event)
			}
			return
		}
	}
}