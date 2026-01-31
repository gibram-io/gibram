// Package logging provides structured logging for GibRAM
package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level represents log severity
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ParseLevel parses a string into a Level
func ParseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// Format represents log output format
type Format int

const (
	FormatText Format = iota
	FormatJSON
)

// Config holds logger configuration
type Config struct {
	Level  string // debug, info, warn, error
	Format string // text, json
	Output string // stdout, stderr, file
	File   string // file path if Output is "file"
}

// DefaultConfig returns default logging configuration
func DefaultConfig() Config {
	return Config{
		Level:  "info",
		Format: "text",
		Output: "stdout",
		File:   "",
	}
}

// Logger is a structured logger
type Logger struct {
	mu     sync.Mutex
	level  Level
	format Format
	output io.Writer
	file   *os.File // keep reference for closing
	fields map[string]interface{}
	prefix string
}

// logEntry represents a single log entry for JSON output
type logEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Caller    string                 `json:"caller,omitempty"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

// New creates a new Logger from config
func New(cfg Config) (*Logger, error) {
	l := &Logger{
		level:  ParseLevel(cfg.Level),
		fields: make(map[string]interface{}),
	}

	// Set format
	switch strings.ToLower(cfg.Format) {
	case "json":
		l.format = FormatJSON
	default:
		l.format = FormatText
	}

	// Set output
	switch strings.ToLower(cfg.Output) {
	case "stderr":
		l.output = os.Stderr
	case "file":
		if cfg.File == "" {
			return nil, fmt.Errorf("log file path required when output is 'file'")
		}
		// Ensure directory exists
		dir := filepath.Dir(cfg.File)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create log directory: %w", err)
		}
		f, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		l.file = f
		l.output = f
	default: // stdout
		l.output = os.Stdout
	}

	return l, nil
}

// Close closes the logger (and any open file)
func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// WithField returns a new logger with the given field
func (l *Logger) WithField(key string, value interface{}) *Logger {
	newLogger := &Logger{
		level:  l.level,
		format: l.format,
		output: l.output,
		file:   l.file,
		prefix: l.prefix,
		fields: make(map[string]interface{}),
	}
	// Copy existing fields
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	newLogger.fields[key] = value
	return newLogger
}

// WithFields returns a new logger with the given fields
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	newLogger := &Logger{
		level:  l.level,
		format: l.format,
		output: l.output,
		file:   l.file,
		prefix: l.prefix,
		fields: make(map[string]interface{}),
	}
	// Copy existing fields
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	// Add new fields
	for k, v := range fields {
		newLogger.fields[k] = v
	}
	return newLogger
}

// WithPrefix returns a new logger with a prefix
func (l *Logger) WithPrefix(prefix string) *Logger {
	newLogger := &Logger{
		level:  l.level,
		format: l.format,
		output: l.output,
		file:   l.file,
		prefix: prefix,
		fields: make(map[string]interface{}),
	}
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	return newLogger
}

// SetLevel changes the log level
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// log writes a log entry
func (l *Logger) log(level Level, msg string, args ...interface{}) {
	if level < l.level {
		return
	}

	// Format message if args provided
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}

	timestamp := time.Now().Format(time.RFC3339Nano)

	// Get caller info
	_, file, line, ok := runtime.Caller(2)
	caller := ""
	if ok {
		// Get just the filename, not full path
		file = filepath.Base(file)
		caller = fmt.Sprintf("%s:%d", file, line)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.format == FormatJSON {
		l.logJSON(timestamp, level, msg, caller)
	} else {
		l.logText(timestamp, level, msg, caller)
	}
}

func (l *Logger) logJSON(timestamp string, level Level, msg, caller string) {
	entry := logEntry{
		Timestamp: timestamp,
		Level:     level.String(),
		Message:   msg,
		Caller:    caller,
	}
	if len(l.fields) > 0 {
		entry.Fields = l.fields
	}

	data, err := json.Marshal(entry)
	if err != nil {
		if _, writeErr := fmt.Fprintf(l.output, `{"error":"failed to marshal log entry: %s"}`+"\n", err); writeErr != nil {
			return
		}
		return
	}
	if _, err := fmt.Fprintln(l.output, string(data)); err != nil {
		return
	}
}

func (l *Logger) logText(timestamp string, level Level, msg, caller string) {
	// Format: 2024-01-11T10:30:00Z [INFO] [server.go:123] message key=value
	var sb strings.Builder

	// Timestamp (shorter format for text)
	sb.WriteString(time.Now().Format("2006-01-02 15:04:05"))
	sb.WriteString(" ")

	// Level with color support (for terminals)
	levelStr := fmt.Sprintf("[%-5s]", level.String())
	sb.WriteString(levelStr)
	sb.WriteString(" ")

	// Caller
	if caller != "" {
		sb.WriteString("[")
		sb.WriteString(caller)
		sb.WriteString("] ")
	}

	// Prefix
	if l.prefix != "" {
		sb.WriteString("[")
		sb.WriteString(l.prefix)
		sb.WriteString("] ")
	}

	// Message
	sb.WriteString(msg)

	// Fields
	if len(l.fields) > 0 {
		for k, v := range l.fields {
			sb.WriteString(" ")
			sb.WriteString(k)
			sb.WriteString("=")
			sb.WriteString(fmt.Sprintf("%v", v))
		}
	}

	if _, err := fmt.Fprintln(l.output, sb.String()); err != nil {
		return
	}
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, args ...interface{}) {
	l.log(LevelDebug, msg, args...)
}

// Info logs an info message
func (l *Logger) Info(msg string, args ...interface{}) {
	l.log(LevelInfo, msg, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, args ...interface{}) {
	l.log(LevelWarn, msg, args...)
}

// Error logs an error message
func (l *Logger) Error(msg string, args ...interface{}) {
	l.log(LevelError, msg, args...)
}

// Printf implements standard log.Printf interface for compatibility
func (l *Logger) Printf(format string, args ...interface{}) {
	l.Info(format, args...)
}

// Println implements standard log.Println interface for compatibility
func (l *Logger) Println(args ...interface{}) {
	l.Info(fmt.Sprint(args...))
}

// =============================================================================
// Global Logger
// =============================================================================

var (
	globalLogger *Logger
	globalMu     sync.RWMutex
)

func init() {
	// Initialize with default config
	globalLogger, _ = New(DefaultConfig())
}

// Init initializes the global logger with config
func Init(cfg Config) error {
	logger, err := New(cfg)
	if err != nil {
		return err
	}
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalLogger != nil {
		if err := globalLogger.Close(); err != nil {
			return err
		}
	}
	globalLogger = logger
	return nil
}

// Global returns the global logger
func Global() *Logger {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalLogger
}

// Debug logs a debug message to the global logger
func Debug(msg string, args ...interface{}) {
	Global().Debug(msg, args...)
}

// Info logs an info message to the global logger
func Info(msg string, args ...interface{}) {
	Global().Info(msg, args...)
}

// Warn logs a warning message to the global logger
func Warn(msg string, args ...interface{}) {
	Global().Warn(msg, args...)
}

// Error logs an error message to the global logger
func Error(msg string, args ...interface{}) {
	Global().Error(msg, args...)
}

// WithField returns a new logger from global with a field
func WithField(key string, value interface{}) *Logger {
	return Global().WithField(key, value)
}

// WithFields returns a new logger from global with fields
func WithFields(fields map[string]interface{}) *Logger {
	return Global().WithFields(fields)
}

// WithPrefix returns a new logger from global with a prefix
func WithPrefix(prefix string) *Logger {
	return Global().WithPrefix(prefix)
}
