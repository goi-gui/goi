package ui

import (
	"os"
	"time"
)

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func sleepMS(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }
