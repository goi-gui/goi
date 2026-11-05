package ui

import (
	"os"
	"testing"

	"github.com/ksckaan1/logger"
)

// TestMain silences the framework's own log by default: the tests build and
// reload windows constantly, and the log is meant for an app's operator.
func TestMain(m *testing.M) {
	l, err := logger.New(&logger.Config{
		ServiceName: "goi-test",
		Output:      []logger.Output{logger.OutputStdout},
		Level:       logger.LevelDisabled,
	})
	if err == nil {
		SetLogger(l)
	}
	os.Exit(m.Run())
}
