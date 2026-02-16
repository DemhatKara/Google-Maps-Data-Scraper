package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	once    sync.Once
	Logger  *slog.Logger
	logFile *os.File
)

// Init initializes the global logger
func Init(dataFolder string) error {
	var err error
	once.Do(func() {
		err = initLogger(dataFolder)
	})
	return err
}

func initLogger(dataFolder string) error {
	logDir := filepath.Join(dataFolder, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	today := time.Now().Format("2006-01-02")
	logFilePath := filepath.Join(logDir, fmt.Sprintf("scraper_%s.log", today))

	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	logFile = file

	// Write to both file and stdout
	multiWriter := io.MultiWriter(os.Stdout, file)

	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	handler := slog.NewTextHandler(multiWriter, opts)
	Logger = slog.New(handler)

	slog.SetDefault(Logger)

	Logger.Info("Logger initialized", "path", logFilePath)
	return nil
}

// Close closes the log file handle
func Close() {
	if logFile != nil {
		logFile.Close()
	}
}

func Info(msg string, args ...any) {
	if Logger != nil {
		Logger.Info(msg, args...)
	}
}

func Error(msg string, args ...any) {
	if Logger != nil {
		Logger.Error(msg, args...)
	}
}

func Warn(msg string, args ...any) {
	if Logger != nil {
		Logger.Warn(msg, args...)
	}
}
