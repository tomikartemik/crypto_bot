package log

import (
	"bytes"
	"context"
	"log/slog"
	"os"
)

var Log Logger

type Logger struct {
	*slog.Logger
	buf *bytes.Buffer
}

func NewLogger(level slog.Level) *Logger {
	var buf bytes.Buffer

	consoleHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})

	bufferHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: level,
	})

	multiHandler := &multiHandler{
		handlers: []slog.Handler{consoleHandler, bufferHandler},
	}

	return &Logger{
		Logger: slog.New(multiHandler),
		buf:    &buf,
	}
}

func (l *Logger) SaveToFile(path string) error {
	return os.WriteFile(path, l.buf.Bytes(), 0644)
}

type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var err error
	for _, h := range m.handlers {
		if e := h.Handle(ctx, r); e != nil {
			err = e
		}
	}
	return err
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: handlers}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: handlers}
}
