package main

import (
	"flag"
	"fmt"
	"log"
	"modbus-go-server/config"
	"modbus-go-server/server"
	"modbus-go-server/tui"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	logFile, err := os.OpenFile("server_transaction.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()
	logger := log.New(logFile, "", log.LstdFlags|log.Lmicroseconds)

	mode := flag.String("mode", "serial", "Communication mode: 'serial' or 'tcp'")
	serialPort := flag.String("port", config.SerialServerPort, "Serial port to use (e.g., COM5)")
	scenarioPath := flag.String("scenario", "", "Path to a scenario script file to run.")
	flag.Parse()

	modbusServer := server.NewServer(logger)

	go modbusServer.RunCommandProcessor()
	go modbusServer.HeartbeatLoop() 

	if *scenarioPath != "" {
		go modbusServer.RunScenario(*scenarioPath)
	} else {
		modbusServer.SetHeartbeat(true)
	}

	if *mode == "tcp" {
		go modbusServer.RunTCP()
	} else if *mode == "serial" {
		go modbusServer.RunSerial(*serialPort)
	} else {
		log.Fatalf("Invalid mode: %s. Choose 'tcp' or 'serial'", *mode)
	}
	
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		logger.Println("Ctrl+C detected, stopping server...")
		modbusServer.Stop()
	}()

	// --- THIS IS THE FIX ---
	// Pass the logger to the TUI model, as required by its constructor.
	p := tea.NewProgram(tui.NewModel(modbusServer, logger), tea.WithAltScreen())
	// --- END OF FIX ---
	
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running TUI: %v", err)
		os.Exit(1)
	}
	
	logger.Println("Application exiting.")
}