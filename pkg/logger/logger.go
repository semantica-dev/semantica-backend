// File: pkg/logger/logger.go
package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
)

// Define ANSI color codes for pretty output.
const (
	ColorReset       = "\033[0m"
	ColorRed         = "\033[31m"
	ColorGreen       = "\033[32m"
	ColorYellow      = "\033[33m"
	ColorBlue        = "\033[34m"
	ColorMagenta     = "\033[35m"
	ColorCyan        = "\033[36m"
	ColorWhite       = "\033[37m"
	ColorGray        = "\033[90m"
	ColorBrightBlack = "\033[90m"
	ColorBrightWhite = "\033[97m"
)

// PrettyHandler is a custom slog.Handler for pretty console output.
type PrettyHandler struct {
	handlerOptions slog.HandlerOptions
	jsonHandler    slog.Handler
	mu             *sync.Mutex
	writer         io.Writer
	colorize       bool
	attrBuffer     *bytes.Buffer
}

// NewPrettyHandler creates a new PrettyHandler.
func NewPrettyHandler(writer io.Writer, opts slog.HandlerOptions, colorizeOutput bool) *PrettyHandler {
	buffer := &bytes.Buffer{}
	innerHandlerOpts := &slog.HandlerOptions{
		Level:       opts.Level,
		AddSource:   opts.AddSource,
		ReplaceAttr: opts.ReplaceAttr,
	}

	return &PrettyHandler{
		handlerOptions: opts,
		jsonHandler:    slog.NewJSONHandler(buffer, innerHandlerOpts),
		mu:             &sync.Mutex{},
		writer:         writer,
		colorize:       colorizeOutput,
		attrBuffer:     buffer,
	}
}

func (h *PrettyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.jsonHandler.Enabled(ctx, level)
}

func (h *PrettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &PrettyHandler{
		handlerOptions: h.handlerOptions,
		jsonHandler:    h.jsonHandler.WithAttrs(attrs),
		mu:             h.mu,
		writer:         h.writer,
		colorize:       h.colorize,
		attrBuffer:     h.attrBuffer,
	}
}

func (h *PrettyHandler) WithGroup(name string) slog.Handler {
	return &PrettyHandler{
		handlerOptions: h.handlerOptions,
		jsonHandler:    h.jsonHandler.WithGroup(name),
		mu:             h.mu,
		writer:         h.writer,
		colorize:       h.colorize,
		attrBuffer:     h.attrBuffer,
	}
}

func (h *PrettyHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.attrBuffer.Reset()
	if err := h.jsonHandler.Handle(ctx, r); err != nil {
		_, _ = fmt.Fprintf(h.writer, "slog pretty handler error: %v\n", err)
		return err
	}

	var allAttrs map[string]any
	if err := json.Unmarshal(h.attrBuffer.Bytes(), &allAttrs); err != nil {
		_, _ = fmt.Fprintf(h.writer, "slog pretty handler unmarshal error: %v\n", err)
		return err
	}

	maybeColorize := func(colorCode string, text string) string {
		if h.colorize {
			return colorCode + text + ColorReset
		}
		return text
	}

	// Используем новый, более подробный формат времени
	timeStr := r.Time.Format("2006-01-02 15:04:05.000")
	delete(allAttrs, slog.TimeKey)

	levelStr := r.Level.String()
	delete(allAttrs, slog.LevelKey)
	switch r.Level {
	case slog.LevelDebug:
		levelStr = maybeColorize(ColorBlue, levelStr)
	case slog.LevelInfo:
		levelStr = maybeColorize(ColorGreen, levelStr)
	case slog.LevelWarn:
		levelStr = maybeColorize(ColorYellow, levelStr)
	case slog.LevelError:
		levelStr = maybeColorize(ColorRed, levelStr)
	default:
		levelStr = maybeColorize(ColorMagenta, levelStr)
	}
	// Убираем выравнивание для уровня лога

	msgStr := r.Message
	delete(allAttrs, slog.MessageKey)
	msgStr = maybeColorize(ColorBrightWhite, msgStr)

	sourceStr := ""
	if h.handlerOptions.AddSource {
		if srcVal, ok := allAttrs[slog.SourceKey]; ok {
			if srcMap, isMap := srcVal.(map[string]any); isMap {
				file, _ := srcMap["file"].(string)
				lineFloat, _ := srcMap["line"].(float64)
				if file != "" {
					// Используем полный путь к файлу
					sourceStr = fmt.Sprintf("%s:%d", file, int(lineFloat))
					sourceStr = maybeColorize(ColorGray, sourceStr)
				}
			}
			delete(allAttrs, slog.SourceKey)
		} else if r.PC != 0 {
			fs := runtime.CallersFrames([]uintptr{r.PC})
			f, _ := fs.Next()
			if f.File != "" {
				// Используем полный путь к файлу
				sourceStr = fmt.Sprintf("%s:%d", f.File, f.Line)
				sourceStr = maybeColorize(ColorGray, sourceStr)
			}
		}
	}

	var sb strings.Builder
	sb.WriteString(maybeColorize(ColorBrightBlack, "["+timeStr+"]"))
	sb.WriteString(" ")
	sb.WriteString(levelStr)
	sb.WriteString(" ")
	if sourceStr != "" {
		sb.WriteString(sourceStr)
		sb.WriteString(" ")
	}
	sb.WriteString(msgStr)

	if len(allAttrs) > 0 {
		sb.WriteString(" ")
		attrsBytes, err := json.MarshalIndent(allAttrs, "", "  ")
		if err != nil {
			sb.WriteString(maybeColorize(ColorRed, fmt.Sprintf("<error marshaling attrs: %v>", err)))
		} else {
			sb.WriteString(maybeColorize(ColorGray, string(attrsBytes)))
		}
	}
	sb.WriteString("\n")

	_, err := h.writer.Write([]byte(sb.String()))
	return err
}

// New creates a new slog.Logger based on the specified format and level.
func New(serviceName string, logFormat string, logLevel slog.Level) *slog.Logger {
	var handler slog.Handler

	handlerOpts := slog.HandlerOptions{
		AddSource: true,
		Level:     logLevel,
	}

	commonAttrs := []slog.Attr{slog.String("service", serviceName)}

	switch strings.ToLower(logFormat) {
	case "pretty":
		pretty := NewPrettyHandler(os.Stdout, handlerOpts, true)
		handler = pretty.WithAttrs(commonAttrs)
	default:
		jsonHandler := slog.NewJSONHandler(os.Stdout, &handlerOpts)
		handler = jsonHandler.WithAttrs(commonAttrs)
	}

	return slog.New(handler)
}
