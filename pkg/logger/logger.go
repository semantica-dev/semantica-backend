package logger

import (
	"log/slog"
	"os"
)

func New(serviceName string) *slog.Logger {
	opts := &slog.HandlerOptions{
		AddSource: true, // Показывает файл и строку, где был вызов лога
		Level:     slog.LevelDebug,
	}
	handler := slog.NewJSONHandler(os.Stdout, opts).WithAttrs([]slog.Attr{slog.String("service", serviceName)})
	return slog.New(handler)
}
