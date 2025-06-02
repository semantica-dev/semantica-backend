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
	expanded       bool
	attrBuffer     *bytes.Buffer
}

// NewPrettyHandler creates a new PrettyHandler.
// 'expanded' controls whether JSON attributes are indented (pretty-expanded) or compact (pretty).
func NewPrettyHandler(writer io.Writer, opts slog.HandlerOptions, colorizeOutput, expanded bool) *PrettyHandler {
	buffer := &bytes.Buffer{}
	innerOpts := &slog.HandlerOptions{
		Level:       opts.Level,
		AddSource:   opts.AddSource,
		ReplaceAttr: opts.ReplaceAttr,
	}
	// JSON handler writes into buffer; indentation is applied manually.
	jsonH := slog.NewJSONHandler(buffer, innerOpts)
	return &PrettyHandler{
		handlerOptions: opts,
		jsonHandler:    jsonH,
		mu:             &sync.Mutex{},
		writer:         writer,
		colorize:       colorizeOutput,
		expanded:       expanded,
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
		expanded:       h.expanded,
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
		expanded:       h.expanded,
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

	maybeColorize := func(colorCode, text string) string {
		if h.colorize {
			return colorCode + text + ColorReset
		}
		return text
	}

	// Format time
	timeStr := r.Time.Format("2006-01-02 15:04:05.000")
	delete(allAttrs, slog.TimeKey)

	// Level
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

	// Message
	msgStr := r.Message
	delete(allAttrs, slog.MessageKey)
	msgStr = maybeColorize(ColorBrightWhite, msgStr)

	// Source
	sourceStr := ""
	if h.handlerOptions.AddSource {
		if srcVal, ok := allAttrs[slog.SourceKey]; ok {
			if srcMap, isMap := srcVal.(map[string]any); isMap {
				file, _ := srcMap["file"].(string)
				lineFloat, _ := srcMap["line"].(float64)
				if file != "" {
					sourceStr = fmt.Sprintf("%s:%d", file, int(lineFloat))
					sourceStr = maybeColorize(ColorGray, sourceStr)
				}
			}
			delete(allAttrs, slog.SourceKey)
		} else if r.PC != 0 {
			fs := runtime.CallersFrames([]uintptr{r.PC})
			f, _ := fs.Next()
			if f.File != "" {
				sourceStr = fmt.Sprintf("%s:%d", f.File, f.Line)
				sourceStr = maybeColorize(ColorGray, sourceStr)
			}
		}
	}

	// Build output
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
		// Choose compact or expanded JSON
		var attrsBytes []byte
		var err error
		if h.expanded {
			attrsBytes, err = json.MarshalIndent(allAttrs, "", "  ")
		} else {
			attrsBytes, err = json.Marshal(allAttrs)
		}
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
func New(serviceName, logFormat string, logLevel slog.Level) *slog.Logger {
	handlerOpts := slog.HandlerOptions{
		AddSource: true,
		Level:     logLevel,
	}
	commonAttrs := []slog.Attr{slog.String("service", serviceName)}

	var handler slog.Handler
	switch strings.ToLower(logFormat) {
	case "pretty-expanded":
		// expanded JSON attributes (multiline)
		h := NewPrettyHandler(os.Stdout, handlerOpts, true, true)
		handler = h.WithAttrs(commonAttrs)
	case "pretty":
		// compact JSON attributes (single line)
		h := NewPrettyHandler(os.Stdout, handlerOpts, true, false)
		handler = h.WithAttrs(commonAttrs)
	default:
		jsonH := slog.NewJSONHandler(os.Stdout, &handlerOpts)
		handler = jsonH.WithAttrs(commonAttrs)
	}

	return slog.New(handler)
}
