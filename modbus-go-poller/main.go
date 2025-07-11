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
	"os"
	"os/signal"
	"sync"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	_ "modernc.org/sqlite"
)

func main() {
	// --- Argument Parsing ---
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

	// --- Logging Setup ---
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

	// --- Database Initialization and Config Loading ---
	dbConn, err := sql.Open("sqlite", *dbFile)
	if err != nil {
		log.Fatalf("FATAL: Could not open database %s: %v", *dbFile, err)
	}
	defer dbConn.Close()

	// The one-time migration logic has been removed. The poller now expects a fully initialized database.
	// if err := database.InitDatabase(dbConn, dbLogger); err != nil {
	// 	log.Fatalf("FATAL: Could not initialize database schema: %v", err)
	// }

	appConfig, err := poller.LoadConfigurationFromDB(dbConn)
	if err != nil {
		log.Fatalf("FATAL: Could not load configuration from database: %v.\nHINT: Please ensure '%s' exists and is a valid poller database. You can create it using the 'modbus-db-init' tool.", err, *dbFile)
	}
	log.Printf("Successfully loaded %d points from database.", len(appConfig.PointsByName))

	// --- Coordinated Shutdown Setup ---
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup // Use a WaitGroup to ensure goroutines finish

	// --- Channel and State Initialization ---
	dbEventChan := make(chan database.Event, 100)
	state := poller.NewPollerState(dbEventChan, appConfig)

	// --- Start Goroutines ---
	wg.Add(3) // We are launching 3 long-running goroutines
	go poller.RunIO(ctx, &wg, state, soeLogger, *mode, target)
	go poller.RunStateProcessor(ctx, &wg, state, soeLogger)
	go database.DatabaseWriter(ctx, &wg, dbEventChan, dbLogger)

	// --- Start TUI ---
	tuiModel := tui.NewModel(state, soeLogger)
	p := tea.NewProgram(tuiModel, tea.WithAltScreen())

	// This goroutine waits for the TUI to exit.
	go func() {
		if err := p.Start(); err != nil {
			log.Fatalf("Alas, there's been an error: %v", err)
		}
		// When TUI exits for any reason, trigger the shutdown.
		cancel()
	}()

	// --- Graceful Shutdown Handling ---
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-shutdownChan: // Triggered by Ctrl-C
		log.Println("Shutdown signal received. Cleaning up.")
		p.Quit() // This will cause the TUI goroutine to exit, which in turn calls cancel().
	case <-ctx.Done(): // Triggered if TUI exits first
		log.Println("TUI exited. Shutting down other processes.")
	}

	// Wait for all goroutines to acknowledge shutdown and finish their work.
	log.Println("Waiting for goroutines to finish...")
	wg.Wait()
	log.Println("All goroutines finished. Exiting.")
	close(dbEventChan) // Now it's safe to close the channel
}