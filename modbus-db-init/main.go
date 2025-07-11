package main

import (
	"database/sql"
	"flag"
	"log"
	"modbus-tools/modbus-db-init/database"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	dbFile := flag.String("db", "poller.db", "Path to the SQLite database file to create.")
	force := flag.Bool("force", false, "Force overwrite if the database file already exists.")
	flag.Parse()

	log.SetFlags(0) // Keep the output clean
	log.Printf("Initializing database at %s...", *dbFile)

	if !*force {
		if _, err := os.Stat(*dbFile); err == nil {
			log.Fatalf("FATAL: Database file '%s' already exists. Use --force to overwrite.", *dbFile)
		}
	}

	// If force is true, we should remove the old file first to ensure a clean slate.
	if *force {
		if err := os.Remove(*dbFile); err != nil && !os.IsNotExist(err) {
			log.Fatalf("FATAL: Could not remove existing database file '%s': %v", *dbFile, err)
		}
		log.Printf("Removed existing database file due to --force flag.")
	}

	dbConn, err := sql.Open("sqlite", *dbFile)
	if err != nil {
		log.Fatalf("FATAL: Could not create database file %s: %v", *dbFile, err)
	}
	defer dbConn.Close()

	if err := database.CreateAndPopulate(dbConn); err != nil {
		// Attempt to clean up the partially created file on failure
		dbConn.Close()
		os.Remove(*dbFile)
		log.Fatalf("FATAL: Failed to initialize schema and populate data: %v", err)
	}

	log.Printf("Successfully created and populated database '%s'.", *dbFile)
}