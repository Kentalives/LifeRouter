package log

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/Kentalives/LifeRouter/pkg/config"
)

const (
	errCol   = "\033[0;31m"
	warnCol  = "\033[0;33m"
	resetCol = "\033[0m"
)

// Error logs v at error level using the service logger.
func Error(v ...any) {
	l.Print(errCol, fmt.Sprint(v...), resetCol)
}

// Errorf formats and logs an error-level message using the service logger.
func Errorf(format string, v ...any) {
	newFormat := errCol + format + resetCol
	l.Printf(newFormat, v...)
}

// Warn logs v at warning level using the service logger.
func Warn(v ...any) {
	l.Print(warnCol, fmt.Sprint(v...), resetCol)
}

// Warnf formats and logs a warning-level message using the service logger.
func Warnf(format string, v ...any) {
	newFormat := warnCol + format + resetCol
	l.Printf(newFormat, v...)
}

// Print logs v using the service logger.
func Print(v ...any) {
	l.Print(v...)
}

// Printf formats and logs a message using the service logger.
func Printf(format string, v ...any) {
	l.Printf(format, v...)
}

var l *log.Logger

func init() {
	l = log.New(os.Stderr, "[PATHFINDING] ", log.Ldate|log.Ltime|log.Lshortfile)
}

// Setup configures the logger outputs from AppConfig.
func Setup(cfg config.AppConfig) {
	var writers []io.Writer

	if cfg.LogStderr {
		writers = append(writers, os.Stderr)
	}

	if cfg.LogFileout {
		f, err := os.OpenFile(cfg.Logfile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			Errorf("-LogSetup- %s", err)
		} else {
			writers = append(writers, f)
		}
	}

	if len(writers) > 0 {
		mw := io.MultiWriter(writers...)
		l.SetOutput(mw)
	} else {
		l.SetOutput(io.Discard)
	}
}

// SetOutput replaces the logger output writer.
func SetOutput(w io.Writer) {
	l.SetOutput(w)
}
