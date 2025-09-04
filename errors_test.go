package serrors

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestSerror_LogValue(t *testing.T) {
	tests := []struct {
		name     string
		serror   serror
		expected map[string]any
	}{
		{
			name: "message only",
			serror: serror{
				msg: "test error",
			},
			expected: map[string]any{
				"msg": "test error",
			},
		},
		{
			name: "message with wrapped error",
			serror: serror{
				msg: "test error",
				err: errors.New("original error"),
			},
			expected: map[string]any{
				"msg":   "test error",
				"cause": "original error",
			},
		},
		{
			name: "message with single attribute",
			serror: serror{
				msg:   "test error",
				attrs: []slog.Attr{slog.String("key1", "value1")},
			},
			expected: map[string]any{
				"msg":  "test error",
				"key1": "value1",
			},
		},
		{
			name: "message with multiple attributes",
			serror: serror{
				msg: "test error",
				attrs: []slog.Attr{
					slog.String("key1", "value1"),
					slog.Int("key2", 42),
					slog.Bool("key3", true),
				},
			},
			expected: map[string]any{
				"msg":  "test error",
				"key1": "value1",
				"key2": float64(42), // JSON unmarshals numbers as float64
				"key3": true,
			},
		},
		{
			name: "message with wrapped error and attributes",
			serror: serror{
				msg: "test error",
				err: errors.New("original error"),
				attrs: []slog.Attr{
					slog.String("user", "john"),
					slog.String("operation", "delete"),
				},
			},
			expected: map[string]any{
				"msg":       "test error",
				"cause":     "original error",
				"user":      "john",
				"operation": "delete",
			},
		},
		{
			name: "empty message with attributes",
			serror: serror{
				msg:   "",
				attrs: []slog.Attr{slog.String("empty", "test")},
			},
			expected: map[string]any{
				"msg":   "",
				"empty": "test",
			},
		},
		{
			name: "nested serror as wrapped error",
			serror: serror{
				msg: "outer error",
				err: serror{
					msg:   "inner error",
					attrs: []slog.Attr{slog.String("inner_key", "inner_value")},
				},
			},
			expected: map[string]any{
				"msg": "outer error",
				"cause": map[string]any{
					"msg":       "inner error",
					"inner_key": "inner_value",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})
			logger := slog.New(handler)

			logger.Info("test log", "error", tt.serror)

			var logOutput map[string]any
			if err := json.Unmarshal(buf.Bytes(), &logOutput); err != nil {
				t.Fatalf("Failed to parse JSON output: %v", err)
			}

			errorGroup, ok := logOutput["error"].(map[string]any)
			if !ok {
				t.Fatalf("Expected 'error' to be a group, got %T: %v", logOutput["error"], logOutput["error"])
			}

			for key, expectedValue := range tt.expected {
				actualValue, exists := errorGroup[key]
				if !exists {
					t.Errorf("Expected key '%s' not found in log output", key)
					continue
				}

				// Handle nested maps (like nested serror causes)
				if expectedMap, ok := expectedValue.(map[string]any); ok {
					actualMap, ok := actualValue.(map[string]any)
					if !ok {
						t.Errorf("Key '%s': expected map[string]any, got %T", key, actualValue)
						continue
					}

					for nestedKey, nestedExpected := range expectedMap {
						nestedActual, exists := actualMap[nestedKey]
						if !exists {
							t.Errorf("Expected nested key '%s.%s' not found", key, nestedKey)
							continue
						}
						if nestedActual != nestedExpected {
							t.Errorf("Nested key '%s.%s': expected %v (%T), got %v (%T)",
								key, nestedKey, nestedExpected, nestedExpected, nestedActual, nestedActual)
						}
					}

					for nestedKey := range actualMap {
						if _, expected := expectedMap[nestedKey]; !expected {
							t.Errorf("Unexpected nested key '%s.%s' found: %v", key, nestedKey, actualMap[nestedKey])
						}
					}
				} else if actualValue != expectedValue {
					t.Errorf("Key '%s': expected %v (%T), got %v (%T)",
						key, expectedValue, expectedValue, actualValue, actualValue)
				}
			}

			for key := range errorGroup {
				if _, expected := tt.expected[key]; !expected {
					t.Errorf("Unexpected key '%s' found in log output: %v", key, errorGroup[key])
				}
			}
		})
	}
}

func TestSerror_LogValue_Integration(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(handler)

	originalErr := errors.New("database connection failed")
	err := WrapError("failed to fetch user", originalErr,
		slog.String("user_id", "123"),
		slog.String("table", "users"),
		slog.Int("retry_count", 3))

	logger.Error("operation failed",
		"error", err,
		"request_id", "req-456",
		"timestamp", "2023-01-01T00:00:00Z")

	var logOutput map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logOutput); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	expectedTopLevel := []string{"time", "level", "msg", "error", "request_id", "timestamp"}
	for _, key := range expectedTopLevel {
		if _, exists := logOutput[key]; !exists {
			t.Errorf("Expected top-level key '%s' not found", key)
		}
	}

	errorGroup, ok := logOutput["error"].(map[string]any)
	if !ok {
		t.Fatalf("Expected 'error' to be a group, got %T", logOutput["error"])
	}

	expectedErrorKeys := []string{"msg", "cause", "user_id", "table", "retry_count"}
	for _, key := range expectedErrorKeys {
		if _, exists := errorGroup[key]; !exists {
			t.Errorf("Expected error key '%s' not found", key)
		}
	}

	if errorGroup["msg"] != "failed to fetch user" {
		t.Errorf("Expected msg 'failed to fetch user', got %v", errorGroup["msg"])
	}
	if errorGroup["cause"] != "database connection failed" {
		t.Errorf("Expected cause 'database connection failed', got %v", errorGroup["cause"])
	}
	if errorGroup["user_id"] != "123" {
		t.Errorf("Expected user_id '123', got %v", errorGroup["user_id"])
	}
}

func TestSerror_LogValue_WithContext(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	logger := slog.New(handler).With("service", "user-service", "version", "1.0.0")

	err := NewError("validation failed",
		slog.String("field", "email"),
		slog.String("reason", "invalid format"))

	logger.Warn("request validation error", "error", err, "ip", "127.0.0.1")

	var logOutput map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logOutput); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	if logOutput["service"] != "user-service" {
		t.Errorf("Expected service 'user-service', got %v", logOutput["service"])
	}
	if logOutput["version"] != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got %v", logOutput["version"])
	}

	errorGroup, ok := logOutput["error"].(map[string]any)
	if !ok {
		t.Fatalf("Expected 'error' to be a group, got %T", logOutput["error"])
	}

	if errorGroup["msg"] != "validation failed" {
		t.Errorf("Expected msg 'validation failed', got %v", errorGroup["msg"])
	}
	if errorGroup["field"] != "email" {
		t.Errorf("Expected field 'email', got %v", errorGroup["field"])
	}
	if errorGroup["reason"] != "invalid format" {
		t.Errorf("Expected reason 'invalid format', got %v", errorGroup["reason"])
	}
}

func TestSerror_Error(t *testing.T) {
	tests := []struct {
		name     string
		serror   serror
		expected string
	}{
		{
			name: "message only",
			serror: serror{
				msg: "test error",
			},
			expected: "test error",
		},
		{
			name: "message with wrapped error",
			serror: serror{
				msg: "test error",
				err: errors.New("original error"),
			},
			expected: "test error cause=[original error]",
		},
		{
			name: "message with single string attribute",
			serror: serror{
				msg:   "test error",
				attrs: []slog.Attr{slog.String("key1", "value1")},
			},
			expected: "test error key1=value1",
		},
		{
			name: "message with multiple attributes",
			serror: serror{
				msg: "test error",
				attrs: []slog.Attr{
					slog.String("user", "john"),
					slog.Int("count", 42),
					slog.Bool("success", true),
				},
			},
			expected: "test error user=john count=42 success=true",
		},
		{
			name: "message with wrapped error and attributes",
			serror: serror{
				msg: "operation failed",
				err: errors.New("connection timeout"),
				attrs: []slog.Attr{
					slog.String("operation", "fetch"),
					slog.String("endpoint", "/api/users"),
				},
			},
			expected: "operation failed cause=[connection timeout] operation=fetch endpoint=/api/users",
		},
		{
			name: "empty message with attributes",
			serror: serror{
				msg:   "",
				attrs: []slog.Attr{slog.String("key", "value")},
			},
			expected: " key=value",
		},
		{
			name: "empty message only",
			serror: serror{
				msg: "",
			},
			expected: "",
		},
		{
			name: "message with various attribute types",
			serror: serror{
				msg: "validation error",
				attrs: []slog.Attr{
					slog.String("field", "email"),
					slog.Int("line", 123),
					slog.Float64("score", 98.5),
					slog.Bool("valid", false),
					slog.Any("data", map[string]string{"key": "value"}),
				},
			},
			expected: "validation error field=email line=123 score=98.5 valid=false data=map[key:value]",
		},
		{
			name: "nested serror as wrapped error",
			serror: serror{
				msg: "outer error",
				err: serror{
					msg:   "inner error",
					attrs: []slog.Attr{slog.String("inner_key", "inner_value")},
				},
			},
			expected: "outer error cause=[inner error inner_key=inner_value]",
		},
		{
			name: "message with special characters in attributes",
			serror: serror{
				msg: "parse error",
				attrs: []slog.Attr{
					slog.String("input", "hello world"),
					slog.String("chars", "[]{}="),
					slog.String("unicode", "café"),
				},
			},
			expected: "parse error input=hello world chars=[]{}= unicode=café",
		},
		{
			name: "deeply nested serrors",
			serror: serror{
				msg: "level 1",
				err: serror{
					msg:   "level 2",
					err:   errors.New("level 3"),
					attrs: []slog.Attr{slog.String("level", "2")},
				},
				attrs: []slog.Attr{slog.String("level", "1")},
			},
			expected: "level 1 cause=[level 2 cause=[level 3] level=2] level=1",
		},
		{
			name: "message with nil wrapped error and attributes",
			serror: serror{
				msg:   "test error",
				err:   nil,
				attrs: []slog.Attr{slog.String("key", "value")},
			},
			expected: "test error key=value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.serror.Error()
			if actual != tt.expected {
				t.Errorf("Error() = %q, want %q", actual, tt.expected)
			}
		})
	}
}

func TestSerror_Error_Integration(t *testing.T) {
	tests := []struct {
		name     string
		create   func() error
		expected string
	}{
		{
			name: "NewError with multiple attributes",
			create: func() error {
				return NewError("user not found",
					slog.String("user_id", "123"),
					slog.String("table", "users"))
			},
			expected: "user not found user_id=123 table=users",
		},
		{
			name: "WrapError with context",
			create: func() error {
				originalErr := errors.New("database connection failed")
				return WrapError("failed to fetch user", originalErr,
					slog.String("user_id", "456"),
					slog.Int("retry_count", 3))
			},
			expected: "failed to fetch user cause=[database connection failed] user_id=456 retry_count=3",
		},
		{
			name: "nested WrapError calls",
			create: func() error {
				innerErr := NewError("validation failed", slog.String("field", "email"))
				middleErr := WrapError("request processing failed", innerErr, slog.String("request_id", "req-123"))
				return WrapError("handler error", middleErr, slog.String("handler", "UserHandler"))
			},
			expected: "handler error cause=[request processing failed cause=[validation failed field=email] request_id=req-123] handler=UserHandler",
		},
		{
			name: "WrapError with standard error",
			create: func() error {
				standardErr := errors.New("file not found")
				return WrapError("configuration error", standardErr,
					slog.String("config_file", "app.yaml"),
					slog.Bool("required", true))
			},
			expected: "configuration error cause=[file not found] config_file=app.yaml required=true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.create()
			actual := err.Error()
			if actual != tt.expected {
				t.Errorf("Error() = %q, want %q", actual, tt.expected)
			}
		})
	}
}

func TestSerror_Error_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		serror   serror
		expected string
	}{
		{
			name: "very long message",
			serror: serror{
				msg: strings.Repeat("a", 1000),
			},
			expected: strings.Repeat("a", 1000),
		},
		{
			name: "message with newlines",
			serror: serror{
				msg:   "line1\nline2\nline3",
				attrs: []slog.Attr{slog.String("multiline", "value1\nvalue2")},
			},
			expected: "line1\nline2\nline3 multiline=value1\nvalue2",
		},
		{
			name: "empty attribute value",
			serror: serror{
				msg:   "test",
				attrs: []slog.Attr{slog.String("empty", "")},
			},
			expected: "test empty=",
		},
		{
			name: "attribute with quote characters",
			serror: serror{
				msg:   "parse error",
				attrs: []slog.Attr{slog.String("quoted", `"hello"`)},
			},
			expected: "parse error quoted=\"hello\"",
		},
		{
			name: "nil error and no attributes",
			serror: serror{
				msg:   "simple message",
				err:   nil,
				attrs: nil,
			},
			expected: "simple message",
		},
		{
			name: "empty attributes slice",
			serror: serror{
				msg:   "message",
				attrs: []slog.Attr{},
			},
			expected: "message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.serror.Error()
			if actual != tt.expected {
				t.Errorf("Error() = %q, want %q", actual, tt.expected)
			}
		})
	}
}

func TestSerror_Unwrap(t *testing.T) {
	tests := []struct {
		name           string
		serror         serror
		expectedError  error
		expectedString string
	}{
		{
			name: "unwrap standard error",
			serror: serror{
				msg: "wrapper message",
				err: errors.New("original error"),
			},
			expectedError:  errors.New("original error"),
			expectedString: "original error",
		},
		{
			name: "unwrap nested serror",
			serror: serror{
				msg: "outer error",
				err: serror{
					msg:   "inner error",
					attrs: []slog.Attr{slog.String("key", "value")},
				},
			},
			expectedError:  nil, // We'll check the type and content separately
			expectedString: "inner error key=value",
		},
		{
			name: "unwrap nil error",
			serror: serror{
				msg: "no wrapped error",
				err: nil,
			},
			expectedError:  nil,
			expectedString: "",
		},
		{
			name: "unwrap deeply nested error",
			serror: serror{
				msg: "level 1",
				err: serror{
					msg: "level 2",
					err: errors.New("level 3"),
				},
			},
			expectedError:  nil,
			expectedString: "level 2 cause=[level 3]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unwrapped := tt.serror.Unwrap()

			if tt.expectedError != nil {
				if unwrapped == nil {
					t.Errorf("Expected unwrapped error to be non-nil")
					return
				}
				if unwrapped.Error() != tt.expectedError.Error() {
					t.Errorf("Unwrap() error = %q, want %q", unwrapped.Error(), tt.expectedError.Error())
				}
			} else if tt.expectedString != "" {
				// Handle serror cases
				if unwrapped == nil {
					t.Errorf("Expected unwrapped error to be non-nil")
					return
				}
				if unwrapped.Error() != tt.expectedString {
					t.Errorf("Unwrap() error = %q, want %q", unwrapped.Error(), tt.expectedString)
				}
			} else {
				// Handle nil case
				if unwrapped != nil {
					t.Errorf("Expected unwrapped error to be nil, got %v", unwrapped)
				}
			}
		})
	}
}

func TestSerror_Unwrap_Integration(t *testing.T) {
	originalErr := errors.New("database connection failed")
	wrappedErr := WrapError("failed to fetch user", originalErr,
		slog.String("user_id", "123"))

	unwrapped := wrappedErr.(interface{ Unwrap() error }).Unwrap()
	if unwrapped != originalErr {
		t.Errorf("Expected unwrapped error to be the original error")
	}

	if !errors.Is(wrappedErr, originalErr) {
		t.Errorf("Expected errors.Is to find original error in chain")
	}

	var targetErr serror
	if !errors.As(wrappedErr, &targetErr) {
		t.Errorf("Expected errors.As to find serror in chain")
	}
}

func TestSerror_Unwrap_ErrorChains(t *testing.T) {
	level3 := errors.New("level 3 error")
	level2 := WrapError("level 2", level3, slog.String("level", "2"))
	level1 := WrapError("level 1", level2, slog.String("level", "1"))

	unwrapped1 := level1.(interface{ Unwrap() error }).Unwrap()
	if unwrapped1.Error() != "level 2 cause=[level 3 error] level=2" {
		t.Errorf("First unwrap failed: %s", unwrapped1.Error())
	}

	unwrapped2 := unwrapped1.(interface{ Unwrap() error }).Unwrap()
	if unwrapped2 != level3 {
		t.Errorf("Second unwrap should return level3 error")
	}

	if !errors.Is(level1, level3) {
		t.Errorf("errors.Is should find level3 in the chain")
	}
}

func TestSerror_Error_CommonExpectations(t *testing.T) {
	tests := []struct {
		name        string
		create      func() error
		expectation string
		description string
	}{
		{
			name: "lowercase message without punctuation",
			create: func() error {
				return NewError("database connection failed")
			},
			expectation: "database connection failed",
			description: "should use lowercase, no trailing punctuation",
		},
		{
			name: "consistent cause format",
			create: func() error {
				return WrapError("operation failed", errors.New("network timeout"))
			},
			expectation: "operation failed cause=[network timeout]",
			description: "wrapped errors should use 'cause=[...]' format",
		},
		{
			name: "attributes without interfering with message",
			create: func() error {
				return NewError("validation failed",
					slog.String("field", "email"),
					slog.String("value", "invalid"))
			},
			expectation: "validation failed field=email value=invalid",
			description: "attributes should append cleanly after message",
		},
		{
			name: "complex error maintains readability",
			create: func() error {
				innerErr := errors.New("connection refused")
				return WrapError("failed to save user", innerErr,
					slog.String("user_id", "u-123"),
					slog.String("table", "users"),
					slog.Int("retry_attempt", 3))
			},
			expectation: "failed to save user cause=[connection refused] user_id=u-123 table=users retry_attempt=3",
			description: "complex errors should remain readable and well-structured",
		},
		{
			name: "error chain preserves all context",
			create: func() error {
				originalErr := errors.New("disk full")
				dbErr := WrapError("database write failed", originalErr, slog.String("table", "events"))
				return WrapError("event processing failed", dbErr, slog.String("event_id", "evt-456"))
			},
			expectation: "event processing failed cause=[database write failed cause=[disk full] table=events] event_id=evt-456",
			description: "nested error chains should preserve all context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.create()
			actual := err.Error()

			if actual != tt.expectation {
				t.Errorf("Error() = %q, want %q\nDescription: %s",
					actual, tt.expectation, tt.description)
			}

			if tt.name == "lowercase message without punctuation" {
				if len(actual) > 0 && actual[0] >= 'A' && actual[0] <= 'Z' {
					t.Errorf("Error message should start with lowercase: %q", actual)
				}
				if len(actual) > 0 {
					lastChar := actual[len(actual)-1]
					if lastChar == '.' || lastChar == '!' || lastChar == '?' {
						t.Errorf("Error message should not end with punctuation: %q", actual)
					}
				}
			}

			if _, ok := err.(interface{ Unwrap() error }); !ok {
				t.Errorf("Error should implement Unwrap interface")
			}
		})
	}
}
