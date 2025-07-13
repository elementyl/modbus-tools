package version

// Version holds the application's version string.
// This is set at build time.
var Version = "dev" // Default value for when running `go run`

// BuildDate holds the date the binary was built.
// This is set at build time.
var BuildDate = "not set"