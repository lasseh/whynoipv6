package logger

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Singleton pattern is used to ensure only one instance of zerolog.Logger
var singleton sync.Once

// Instance of zerolog.Logger
var loggerInstance zerolog.Logger

// GetLogger initializes a zerolog.Logger instance if it has not been initialized
// already and returns the same instance for subsequent calls.
func GetLogger() zerolog.Logger {
	singleton.Do(func() {
		// Set zerolog to use pkgerrors for marshaling errors with stack trace
		zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
		// Set the time format and field name for timestamps
		zerolog.TimeFieldFormat = time.RFC3339Nano
		zerolog.TimestampFieldName = "dt" // Custom time key for BetterStack

		// Default log level is Info
		logLevel := zerolog.InfoLevel
		// Check if log level is set in environment variable
		levelEnv := os.Getenv("LOG_LEVEL")
		if levelEnv != "" {
			levelFromEnv, err := zerolog.ParseLevel(levelEnv)
			if err != nil {
				log.Println(fmt.Errorf("defaulting to Info: %w", err))
			}

			logLevel = levelFromEnv
		}
		// Configure an auto rotating file for storing JSON-formatted records
		fileLogger := &lumberjack.Logger{
			Filename:   "logs/crawler.log",
			MaxSize:    10, // Max size in megabytes before the file is rotated
			MaxBackups: 2,  // Max number of old log files to keep
			MaxAge:     14, // Max number of days to retain the log files
		}

		// Configure console logging in a human-friendly and colorized format
		consoleLogger := zerolog.ConsoleWriter{
			Out:           os.Stdout,
			TimeFormat:    "15:04:05", // 24-hour time format for console
			NoColor:       false,      // Enable color
			FieldsExclude: []string{
				// "service",    // Exclude service from console logs
				// "nameserver", // Exclude nameserver from console logs
			},
		}

		// Allows logging to multiple destinations at once
		multiLevelOutput := zerolog.MultiLevelWriter(consoleLogger, fileLogger)

		// Create a global logger instance
		loggerInstance = zerolog.New(multiLevelOutput).
			Level(zerolog.Level(logLevel)).
			With().
			Timestamp().
			Logger()

		// Set the default logger context
		zerolog.DefaultContextLogger = &loggerInstance
	})

	return loggerInstance
}
