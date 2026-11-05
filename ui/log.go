package ui

import (
	"context"
	"os"
	"sync"

	"github.com/ksckaan1/logger"
)

// logCtx is the context every ui log line carries. The library reads trace
// information from it; ui has none, so it is the background context.
var logCtx = context.Background()

var (
	logMu sync.RWMutex
	uiLog *logger.Logger
)

// SetLogger replaces where ui writes its logs: startup, a window load and a hot
// reload report how long they took, and colour problems are warnings. Pass nil
// to go back to the default, which reads the LOGGER_* variables.
//
// It is safe to call before NewApp; doing it after is fine too.
func SetLogger(l *logger.Logger) {
	logMu.Lock()
	uiLog = l
	logMu.Unlock()
}

// logg returns the logger ui writes to, creating the default on first use.
func logg() *logger.Logger {
	logMu.RLock()
	l := uiLog
	logMu.RUnlock()
	if l != nil {
		return l
	}
	logMu.Lock()
	defer logMu.Unlock()
	if uiLog == nil {
		uiLog = newLogger()
	}
	return uiLog
}

// newLogger builds the default logger from the environment. Logging is a
// side path: a bad configuration falls back to the library's defaults instead
// of failing the app.
func newLogger() *logger.Logger {
	cfg := logger.ParseLoggerEnvVar()
	if os.Getenv("LOGGER_SERVICE_NAME") == "" {
		cfg.ServiceName = "goi"
	}
	l, err := logger.New(cfg)
	if err == nil {
		return l
	}
	l, err = logger.New(logger.DefaultConfig())
	if err != nil {
		// Nothing left to log to; a nil logger would panic on use, so keep a
		// disabled one.
		l, _ = logger.New(&logger.Config{
			ServiceName: "goi",
			Output:      []logger.Output{logger.OutputStdout},
			Level:       logger.LevelDisabled,
		})
	}
	return l
}

// debugUI reports whether DEBUG_UI is on, which logs every input event except
// pointer motion.
func debugUI() bool {
	switch os.Getenv("DEBUG_UI") {
	case "1", "true", "TRUE", "True", "yes", "on":
		return true
	}
	return false
}
