package ui

import (
	"context"
	"os"
	"strings"
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
// to go back to the default, which is silent unless GOI_DEBUG asks for logs and
// otherwise reads the LOGGER_* variables.
//
// It is safe to call before NewApp; doing it after is fine too. A logger passed
// here is used as given; GOI_DEBUG only gates the default.
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
// of failing the app. The framework stays quiet unless GOI_DEBUG is on, so an
// app's own output is not mixed with startup and reload lines.
func newLogger() *logger.Logger {
	l, err := logger.New(loggerConfig())
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

// loggerConfig is the default logger's configuration. Quiet is the normal state:
// only GOI_DEBUG turns the framework's own lines on.
func loggerConfig() *logger.Config {
	cfg := logger.ParseLoggerEnvVar()
	if os.Getenv("LOGGER_SERVICE_NAME") == "" {
		cfg.ServiceName = "goi"
	}
	if !goiDebug() {
		return &logger.Config{
			ServiceName: "goi",
			Output:      []logger.Output{logger.OutputStdout},
			Level:       logger.LevelDisabled,
		}
	}
	return cfg
}

// goiDebug reports whether GOI_DEBUG asks for the framework's own logs. goi run
// sets it to TRUE, and a built app can set it when its logs are wanted.
func goiDebug() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GOI_DEBUG"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
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
