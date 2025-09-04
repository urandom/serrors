package serrors

import (
	"log/slog"
	"strings"
)

type serror struct {
	msg   string
	err   error
	attrs []slog.Attr
}

// Error implements error.
func (s serror) Error() string {
	var b strings.Builder

	_, _ = b.WriteString(s.msg)

	if s.err != nil {
		_ = b.WriteByte(' ')
		_, _ = b.WriteString(CauseKey + "=[" + s.err.Error() + "]")
	}

	for _, attr := range s.attrs {
		_ = b.WriteByte(' ')
		_, _ = b.WriteString(attr.String())
	}

	return b.String()
}

func (s serror) LogValue() slog.Value {
	size := len(s.attrs) + 1
	if s.err != nil {
		size++
	}

	attrs := make([]slog.Attr, 0, size)
	attrs = append(attrs, slog.String(slog.MessageKey, s.msg))

	if s.err != nil {
		attrs = append(attrs, slog.Any(CauseKey, s.err))
	}

	attrs = append(attrs, s.attrs...)

	return slog.GroupValue(attrs...)
}

func (e serror) Unwrap() error {
	return e.err
}

var CauseKey = "cause"

func NewError(msg string, attrs ...slog.Attr) error {
	return serror{msg: msg, attrs: attrs}
}

func WrapError(msg string, err error, attrs ...slog.Attr) error {
	return serror{msg: msg, err: err, attrs: attrs}
}
