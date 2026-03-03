package backup

import "os"

// getEnvVar is a thin wrapper around os.Getenv used in dumper.go.
func getEnvVar(key string) string { return os.Getenv(key) }
