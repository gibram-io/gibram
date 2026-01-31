package logging

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
	}{
		{"debug", LevelDebug},
		{"DEBUG", LevelDebug},
		{"info", LevelInfo},
		{"INFO", LevelInfo},
		{"warn", LevelWarn},
		{"WARN", LevelWarn},
		{"warning", LevelWarn},
		{"error", LevelError},
		{"ERROR", LevelError},
		{"unknown", LevelInfo}, // default
		{"", LevelInfo},        // default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseLevel(tt.input)
			if result != tt.expected {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		{Level(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.level.String(); got != tt.expected {
				t.Errorf("Level.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestLoggerTextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:  LevelDebug,
		format: FormatText,
		output: &buf,
		fields: make(map[string]interface{}),
	}

	logger.Info("test message")

	output := buf.String()
	if !strings.Contains(output, "[INFO ]") {
		t.Errorf("expected INFO level in output, got: %s", output)
	}
	if !strings.Contains(output, "test message") {
		t.Errorf("expected message in output, got: %s", output)
	}
}

func TestLoggerJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:  LevelDebug,
		format: FormatJSON,
		output: &buf,
		fields: make(map[string]interface{}),
	}

	logger.Info("test message")

	output := buf.String()
	var entry logEntry
	if err := json.Unmarshal([]byte(output), &entry); err != nil {
		t.Fatalf("failed to parse JSON log: %v", err)
	}

	if entry.Level != "INFO" {
		t.Errorf("expected level INFO, got %s", entry.Level)
	}
	if entry.Message != "test message" {
		t.Errorf("expected message 'test message', got %s", entry.Message)
	}
}

func TestLoggerLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:  LevelWarn,
		format: FormatText,
		output: &buf,
		fields: make(map[string]interface{}),
	}

	// These should be filtered out
	logger.Debug("debug message")
	logger.Info("info message")

	// These should appear
	logger.Warn("warn message")
	logger.Error("error message")

	output := buf.String()
	if strings.Contains(output, "debug message") {
		t.Error("debug message should be filtered")
	}
	if strings.Contains(output, "info message") {
		t.Error("info message should be filtered")
	}
	if !strings.Contains(output, "warn message") {
		t.Error("warn message should appear")
	}
	if !strings.Contains(output, "error message") {
		t.Error("error message should appear")
	}
}

func TestLoggerWithField(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:  LevelDebug,
		format: FormatJSON,
		output: &buf,
		fields: make(map[string]interface{}),
	}

	logger.WithField("key", "value").Info("test")

	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if entry.Fields["key"] != "value" {
		t.Errorf("expected field key=value, got %v", entry.Fields)
	}
}

func TestLoggerWithFields(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:  LevelDebug,
		format: FormatJSON,
		output: &buf,
		fields: make(map[string]interface{}),
	}

	logger.WithFields(map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	}).Info("test")

	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if entry.Fields["key1"] != "value1" {
		t.Errorf("expected field key1=value1, got %v", entry.Fields)
	}
	if entry.Fields["key2"] != float64(42) { // JSON numbers are float64
		t.Errorf("expected field key2=42, got %v", entry.Fields)
	}
}

func TestLoggerWithPrefix(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:  LevelDebug,
		format: FormatText,
		output: &buf,
		fields: make(map[string]interface{}),
	}

	logger.WithPrefix("myprefix").Info("test message")

	output := buf.String()
	if !strings.Contains(output, "[myprefix]") {
		t.Errorf("expected prefix in output, got: %s", output)
	}
}

func TestLoggerPrintf(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:  LevelDebug,
		format: FormatText,
		output: &buf,
		fields: make(map[string]interface{}),
	}

	logger.Printf("formatted %s %d", "message", 42)

	output := buf.String()
	if !strings.Contains(output, "formatted message 42") {
		t.Errorf("expected formatted message, got: %s", output)
	}
}

func TestNewLoggerWithFile(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	logger, err := New(Config{
		Level:  "info",
		Format: "text",
		Output: "file",
		File:   logFile,
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer func() {
		if err := logger.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	logger.Info("test message to file")

	// Read file content
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	if !strings.Contains(string(content), "test message to file") {
		t.Errorf("expected message in log file, got: %s", string(content))
	}
}

func TestNewLoggerFileError(t *testing.T) {
	_, err := New(Config{
		Level:  "info",
		Format: "text",
		Output: "file",
		File:   "", // empty file path should error
	})
	if err == nil {
		t.Error("expected error for empty file path")
	}
}

func TestGlobalLogger(t *testing.T) {
	// Test global functions don't panic
	var buf bytes.Buffer

	// Create custom logger
	logger := &Logger{
		level:  LevelDebug,
		format: FormatText,
		output: &buf,
		fields: make(map[string]interface{}),
	}

	// Replace global logger
	globalMu.Lock()
	oldLogger := globalLogger
	globalLogger = logger
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		globalLogger = oldLogger
		globalMu.Unlock()
	}()

	Debug("debug")
	Info("info")
	Warn("warn")
	Error("error")

	output := buf.String()
	if !strings.Contains(output, "debug") {
		t.Error("expected debug in output")
	}
	if !strings.Contains(output, "info") {
		t.Error("expected info in output")
	}
	if !strings.Contains(output, "warn") {
		t.Error("expected warn in output")
	}
	if !strings.Contains(output, "error") {
		t.Error("expected error in output")
	}
}

func TestSetLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:  LevelError,
		format: FormatText,
		output: &buf,
		fields: make(map[string]interface{}),
	}

	logger.Info("should not appear")
	if buf.Len() > 0 {
		t.Error("info should be filtered at error level")
	}

	logger.SetLevel(LevelInfo)
	logger.Info("should appear")
	if !strings.Contains(buf.String(), "should appear") {
		t.Error("info should appear after level change")
	}
}

// =============================================================================
// Additional Coverage Tests
// =============================================================================

func TestLoggerPrintln(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:  LevelDebug,
		format: FormatText,
		output: &buf,
		fields: make(map[string]interface{}),
	}

	logger.Println("test", "println", "message")

	output := buf.String()
	// fmt.Sprint joins with space, so check for individual words
	if !strings.Contains(output, "test") || !strings.Contains(output, "println") || !strings.Contains(output, "message") {
		t.Errorf("expected 'test println message' in output, got: %s", output)
	}
}

func TestInit(t *testing.T) {
	// Test Init with valid config
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "init.log")

	err := Init(Config{
		Level:  "debug",
		Format: "json",
		Output: "file",
		File:   logFile,
	})
	if err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	// Verify global logger is updated
	logger := Global()
	if logger.level != LevelDebug {
		t.Errorf("expected level debug, got %v", logger.level)
	}
	if logger.format != FormatJSON {
		t.Errorf("expected format JSON, got %v", logger.format)
	}

	// Reset to default
	if err := Init(DefaultConfig()); err != nil {
		t.Fatalf("Init() reset failed: %v", err)
	}
}

func TestInitError(t *testing.T) {
	// Test Init with invalid config (file output but no file path)
	err := Init(Config{
		Level:  "info",
		Format: "text",
		Output: "file",
		File:   "",
	})
	if err == nil {
		t.Error("Init() should fail with empty file path")
	}
}

func TestGlobalWithField(t *testing.T) {
	var buf bytes.Buffer

	// Set up test logger
	logger := &Logger{
		level:  LevelDebug,
		format: FormatJSON,
		output: &buf,
		fields: make(map[string]interface{}),
	}

	globalMu.Lock()
	oldLogger := globalLogger
	globalLogger = logger
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		globalLogger = oldLogger
		globalMu.Unlock()
	}()

	// Test global WithField
	WithField("component", "test").Info("with field message")

	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if entry.Fields["component"] != "test" {
		t.Errorf("expected field component=test, got %v", entry.Fields)
	}
}

func TestGlobalWithFields(t *testing.T) {
	var buf bytes.Buffer

	logger := &Logger{
		level:  LevelDebug,
		format: FormatJSON,
		output: &buf,
		fields: make(map[string]interface{}),
	}

	globalMu.Lock()
	oldLogger := globalLogger
	globalLogger = logger
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		globalLogger = oldLogger
		globalMu.Unlock()
	}()

	// Test global WithFields
	WithFields(map[string]interface{}{
		"request_id": "123",
		"user_id":    456,
	}).Info("with fields message")

	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if entry.Fields["request_id"] != "123" {
		t.Errorf("expected field request_id=123, got %v", entry.Fields)
	}
}

func TestGlobalWithPrefix(t *testing.T) {
	var buf bytes.Buffer

	logger := &Logger{
		level:  LevelDebug,
		format: FormatText,
		output: &buf,
		fields: make(map[string]interface{}),
	}

	globalMu.Lock()
	oldLogger := globalLogger
	globalLogger = logger
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		globalLogger = oldLogger
		globalMu.Unlock()
	}()

	// Test global WithPrefix
	WithPrefix("server").Info("with prefix message")

	output := buf.String()
	if !strings.Contains(output, "[server]") {
		t.Errorf("expected prefix [server] in output, got: %s", output)
	}
}

func TestNewLoggerStderr(t *testing.T) {
	logger, err := New(Config{
		Level:  "info",
		Format: "text",
		Output: "stderr",
	})
	if err != nil {
		t.Fatalf("failed to create stderr logger: %v", err)
	}
	defer func() {
		if err := logger.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	if logger.output != os.Stderr {
		t.Error("expected output to be stderr")
	}
}

func TestLoggerCloseNoFile(t *testing.T) {
	logger, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	// Close logger without file (should return nil)
	if err := logger.Close(); err != nil {
		t.Errorf("Close() without file should return nil, got: %v", err)
	}
}

func TestLoggerWithFieldNilFields(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:  LevelDebug,
		format: FormatJSON,
		output: &buf,
		fields: nil, // nil fields
	}

	// Should not panic
	newLogger := logger.WithField("key", "value")
	if newLogger == nil {
		t.Error("WithField should return a logger")
	}
}

func TestLoggerWithFieldsNilFields(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:  LevelDebug,
		format: FormatJSON,
		output: &buf,
		fields: nil, // nil fields
	}

	// Should not panic
	newLogger := logger.WithFields(map[string]interface{}{"key": "value"})
	if newLogger == nil {
		t.Error("WithFields should return a logger")
	}
}

func TestLoggerWithPrefixExisting(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:  LevelDebug,
		format: FormatText,
		output: &buf,
		fields: make(map[string]interface{}),
		prefix: "existing",
	}

	// WithPrefix creates new prefix, doesn't chain
	newLogger := logger.WithPrefix("new")
	newLogger.Info("test")

	output := buf.String()
	if !strings.Contains(output, "[new]") {
		t.Errorf("expected prefix [new] in output, got: %s", output)
	}
}

func TestLogJSONWithError(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:  LevelDebug,
		format: FormatJSON,
		output: &buf,
		fields: map[string]interface{}{
			"error": "test error",
		},
	}

	logger.Info("message with error field")

	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if entry.Fields["error"] != "test error" {
		t.Errorf("expected error field, got %v", entry.Fields)
	}
}

func TestLogTextWithFields(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:  LevelDebug,
		format: FormatText,
		output: &buf,
		fields: map[string]interface{}{
			"request_id": "abc123",
			"duration":   42.5,
		},
	}

	logger.Info("text with fields")

	output := buf.String()
	if !strings.Contains(output, "request_id=abc123") {
		t.Errorf("expected request_id field in output, got: %s", output)
	}
}

func TestNewLoggerWithFileCannotCreateDir(t *testing.T) {
	// Try to create log in a path that can't be created (null char)
	_, err := New(Config{
		Level:  "info",
		Format: "text",
		Output: "file",
		File:   "/nonexistent\x00path/test.log",
	})
	if err == nil {
		t.Error("expected error for invalid file path")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Level != "info" {
		t.Errorf("expected level info, got %s", cfg.Level)
	}
	if cfg.Format != "text" {
		t.Errorf("expected format text, got %s", cfg.Format)
	}
	if cfg.Output != "stdout" {
		t.Errorf("expected output stdout, got %s", cfg.Output)
	}
}

func TestLoggerFormatArgs(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:  LevelDebug,
		format: FormatText,
		output: &buf,
		fields: make(map[string]interface{}),
	}

	logger.Info("formatted %s with %d args", "message", 2)

	output := buf.String()
	if !strings.Contains(output, "formatted message with 2 args") {
		t.Errorf("expected formatted message, got: %s", output)
	}
}
