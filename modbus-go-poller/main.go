// ===== C:\Projects\modbus-tools\modbus-go-poller\main.go =====
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"modbus-tools/modbus-go-poller/config"
	"modbus-tools/modbus-go-poller/database"
	"modbus-tools/modbus-go-poller/poller"
	"modbus-tools/modbus-go-poller/tui"
	"modbus-tools/modbus-go-poller/version"
	"os"
	"os/signal"
	"sync"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	_ "modernc.org/sqlite"
)

func main() {
	log.Printf("--- Starting modbus-go-poller Version: %s (Build Date: %s) ---", version.Version, version.BuildDate)
	mode := flag.String("mode", "tcp", "Connection mode: 'tcp' or 'serial'")
	targetTCP := flag.String("target-tcp", fmt.Sprintf("%s:%d", config.DefaultTCPServerHost, config.DefaultTCPServerPort), "TCP target address (e.g., 127.0.0.1:5020)")
	targetSerial := flag.String("target-serial", config.DefaultSerialPort, "Serial port (e.g., COM3 or /dev/ttyUSB0)")
	dbFile := flag.String("db", "poller.db", "Path to the main SQLite database file")
	flag.Parse()

	var target string
	if *mode == "tcp" {
		target = *targetTCP
	} else if *mode == "serial" {
		target = *targetSerial
	} else {
		fmt.Println("Invalid mode. Use 'tcp' or 'serial'.")
		os.Exit(1)
	}

	soeLogFile, err := os.OpenFile("poller_events.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open SOE log file: %v", err)
	}
	defer soeLogFile.Close()
	soeLogger := log.New(soeLogFile, "", log.LstdFlags|log.Lmicroseconds)

	dbLogFile, err := os.OpenFile("poller_database.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open database log file: %v", err)
	}
	defer dbLogFile.Close()
	dbLogger := log.New(dbLogFile, "DB: ", log.LstdFlags|log.Lmicroseconds)

	txLogFile, err := os.OpenFile("poller_transaction.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open transaction log file: %v", err)
	}
	defer txLogFile.Close()
	txLogger := log.New(txLogFile, "", log.LstdFlags|log.Lmicroseconds)
	txLogger.Println("--- NEW SESSION ---")

	dbConn, err := sql.Open("sqlite", *dbFile)
	if err != nil {
		log.Fatalf("FATAL: Could not open database %s: %v", *dbFile, err)
	}
	defer dbConn.Close()

	appConfig, err := poller.LoadConfigurationFromDB(dbConn)
	if err != nil {
		log.Fatalf("FATAL: Could not load configuration from database: %v.\nHINT: Please ensure '%s' exists and is a valid poller database. You can create it using the 'modbus-db-init' tool.", err, *dbFile)
	}
	log.Printf("Successfully loaded %d points from database.", len(appConfig.PointsByName))

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	dbEventChan := make(chan database.Event, 100)
	polledDataChan := make(chan map[uint16]uint16, 10)
	ioResultChan := make(chan poller.IO_Result, 10)
	commandPacketChan := make(chan []byte, 10) // Create the command packet channel
	state := poller.NewPollerState(dbEventChan, ioResultChan, appConfig)

	wg.Add(3)
	go poller.RunIO(ctx, &wg, state, soeLogger, txLogger, polledDataChan, commandPacketChan, *mode, target)
	go poller.RunStateProcessor(ctx, &wg, state, soeLogger, polledDataChan, commandPacketChan)
	go database.DatabaseWriter(ctx, &wg, dbEventChan, dbLogger)

	tuiModel := tui.NewModel(state, soeLogger)
	p := tea.NewProgram(tuiModel, tea.WithAltScreen())

	go func() {
		if err := p.Start(); err != nil {
			log.Fatalf("Alas, there's been an error: %v", err)
		}
		cancel()
	}()

	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-shutdownChan:
		log.Println("Shutdown signal received. Cleaning up.")
		p.Quit()
	case <-ctx.Done():
		log.Println("TUI exited. Shutting down other processes.")
	}

	log.Println("Waiting for producer goroutines to finish...")
	wg.Wait()
	log.Println("All goroutines finished. Closing database event channel.")
	close(dbEventChan)
}