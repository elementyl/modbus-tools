//go:build ignore

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"
)

func main() {
	log.Println("Building modbus-go-poller...")

	// 1. Define the version and build date
	version := "0.2.0" // Or read from a file
	buildDate := time.Now().UTC().Format(time.RFC3339)

	// 2. Construct the linker flags
	ldflags := fmt.Sprintf("-X 'modbus-tools/modbus-go-poller/version.Version=%s' -X 'modbus-tools/modbus-go-poller/version.BuildDate=%s'", version, buildDate)

	// 3. Prepare the `go build` command
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "./modbus-go-poller")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// 4. Run the command
	err := cmd.Run()
	if err != nil {
		log.Fatalf("Build failed: %v", err)
	}

	log.Println("Build successful.")
}