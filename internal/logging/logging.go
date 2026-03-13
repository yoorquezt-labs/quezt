// Package logging provides structured logging for the yqmev TUI.
// Logs are written to ~/.yqmev/logs/ by default using uber-go/zap.
package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	log  *zap.SugaredLogger
	raw  *zap.Logger
	once sync.Once
)

// Init initializes the global zap logger writing to ~/.yqmev/logs/.
// Call once at startup. If logDir is empty, defaults to ~/.yqmev/logs/.
func Init(logDir string) error {
	var initErr error
	once.Do(func() {
		if logDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				initErr = fmt.Errorf("get home dir: %w", err)
				return
			}
			logDir = filepath.Join(home, ".yqmev", "logs")
		}

		if err := os.MkdirAll(logDir, 0o755); err != nil {
			initErr = fmt.Errorf("create log dir: %w", err)
			return
		}

		filename := filepath.Join(logDir, time.Now().Format("2006-01-02")+".log")
		f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			initErr = fmt.Errorf("open log file: %w", err)
			return
		}

		encoderCfg := zapcore.EncoderConfig{
			TimeKey:        "ts",
			LevelKey:       "level",
			MessageKey:     "msg",
			CallerKey:      "caller",
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeLevel:    zapcore.CapitalLevelEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
			EncodeDuration: zapcore.StringDurationEncoder,
		}

		core := zapcore.NewCore(
			zapcore.NewConsoleEncoder(encoderCfg),
			zapcore.AddSync(f),
			zapcore.DebugLevel,
		)

		raw = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
		log = raw.Sugar()
	})
	return initErr
}

// Close flushes and closes the logger.
func Close() {
	if raw != nil {
		_ = raw.Sync()
	}
}

// LogDir returns the default log directory path.
func LogDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".yqmev", "logs")
}

// L returns the underlying sugared logger for structured logging.
// Returns a no-op logger if Init hasn't been called.
func L() *zap.SugaredLogger {
	if log == nil {
		return zap.NewNop().Sugar()
	}
	return log
}

// Info logs an informational message.
func Info(msg string, keysAndValues ...any) {
	if log != nil {
		log.Infow(msg, keysAndValues...)
	}
}

// Debug logs a debug message.
func Debug(msg string, keysAndValues ...any) {
	if log != nil {
		log.Debugw(msg, keysAndValues...)
	}
}

// Warn logs a warning message.
func Warn(msg string, keysAndValues ...any) {
	if log != nil {
		log.Warnw(msg, keysAndValues...)
	}
}

// Error logs an error message.
func Error(msg string, keysAndValues ...any) {
	if log != nil {
		log.Errorw(msg, keysAndValues...)
	}
}
