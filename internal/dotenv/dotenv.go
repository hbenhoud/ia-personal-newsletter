// Package dotenv loads a .env file into the process environment. It mirrors the
// loader in cmd/newsletter so the new ingest/server binaries pick up local
// secrets the same way, without importing the frozen CLI.
package dotenv

import (
	"os"
	"strings"
)

// Load reads path and sets any environment variable that is not already
// defined. A missing file is not an error (.env is optional).
func Load(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if os.Getenv(key) == "" {
			os.Setenv(key, value) //nolint:errcheck
		}
	}
}
