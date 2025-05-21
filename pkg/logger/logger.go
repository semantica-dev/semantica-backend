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
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	// "time" // не используется напрямую в этом файле, только для r.Time
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
	ColorGray        = "\033[90m" // Более темный серый для атрибутов
	ColorBrightBlack = "\033[90m" // Использовался как darkGray в примере
	ColorBrightWhite = "\033[97m" // Использовался как white в примере

)

// PrettyHandler is a custom slog.Handler for pretty console output.
type PrettyHandler struct {
	handlerOptions slog.HandlerOptions // Store original options
	jsonHandler    slog.Handler        // Inner JSON handler to marshal Attrs
	mu             *sync.Mutex
	writer         io.Writer
	colorize       bool
	attrBuffer     *bytes.Buffer // Buffer for the inner JSON handler's output
}

// NewPrettyHandler creates a new PrettyHandler.
// writer: Destination for log output.
// opts: Slog handler options.
// colorizeOutput: Whether to use ANSI colors.
func NewPrettyHandler(writer io.Writer, opts slog.HandlerOptions, colorizeOutput bool) *PrettyHandler {
	buffer := &bytes.Buffer{}

	// The inner JSON handler will write to the buffer.
	// We use its ReplaceAttr to ensure that source, time, level, msg keys are present
	// if AddSource is enabled, so we can extract them.
	// We don't need to suppress them here, as we'll remove them from the map later.
	innerHandlerOpts := &slog.HandlerOptions{
		Level:       opts.Level,
		AddSource:   opts.AddSource,   // Ensure source is added if requested
		ReplaceAttr: opts.ReplaceAttr, // Pass through ReplaceAttr from original options
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
	return h.jsonHandler.Enabled(ctx, level) // Delegate to inner handler
}

func (h *PrettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Create a new PrettyHandler with the new set of Attrs for the inner handler
	return &PrettyHandler{
		handlerOptions: h.handlerOptions,
		jsonHandler:    h.jsonHandler.WithAttrs(attrs),
		mu:             h.mu, // Mutex can be shared as it protects the writer and buffer
		writer:         h.writer,
		colorize:       h.colorize,
		attrBuffer:     h.attrBuffer, // Buffer is also shared and protected by the same mutex
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
	// Use a single mutex lock for the entire handle operation
	// as we are using a shared buffer (h.attrBuffer).
	h.mu.Lock()
	defer h.mu.Unlock()

	// 1. Use the inner JSON handler to get all fields (including source) into the buffer.
	h.attrBuffer.Reset() // Reset buffer before use
	if err := h.jsonHandler.Handle(ctx, r); err != nil {
		// If the inner handler fails, we can't proceed.
		// Write a raw error to the output.
		_, _ = fmt.Fprintf(h.writer, "slog pretty handler error: %v\n", err)
		return err
	}

	// 2. Unmarshal the JSON from the buffer into a map.
	var allAttrs map[string]any
	if err := json.Unmarshal(h.attrBuffer.Bytes(), &allAttrs); err != nil {
		_, _ = fmt.Fprintf(h.writer, "slog pretty handler unmarshal error: %v\n", err)
		return err
	}

	// --- Helper for colorization ---
	maybeColorize := func(colorCode string, text string) string {
		if h.colorize {
			return colorCode + text + ColorReset
		}
		return text
	}

	// 3. Extract and format standard fields.
	timeStr := r.Time.Format("15:04:05.000")
	delete(allAttrs, slog.TimeKey) // Remove so it's not in "extra" attrs

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
	// Padding for level
	if len(levelStr) < 5 && !h.colorize { // Simple padding for non-colored
		levelStr = fmt.Sprintf("%-5s", levelStr)
	} else if len(levelStr) < (5+len(ColorBlue)+len(ColorReset)) && h.colorize { // Approx padding for colored
		levelStr = fmt.Sprintf("%s%-*s", levelStr, (5+len(ColorBlue)+len(ColorReset))-len(levelStr), "")
	}

	msgStr := r.Message
	delete(allAttrs, slog.MessageKey)
	msgStr = maybeColorize(ColorBrightWhite, msgStr)

	sourceStr := ""
	if h.handlerOptions.AddSource { // Check if source was requested
		if srcVal, ok := allAttrs[slog.SourceKey]; ok {
			if srcMap, isMap := srcVal.(map[string]any); isMap {
				file, _ := srcMap["file"].(string)
				lineFloat, _ := srcMap["line"].(float64)
				if file != "" {
					sourceStr = fmt.Sprintf("%s:%d", filepath.Base(file), int(lineFloat))
					sourceStr = maybeColorize(ColorGray, sourceStr)
				}
			}
			delete(allAttrs, slog.SourceKey) // Remove source from extra attrs
		} else if r.PC != 0 { // Fallback if source is not in map but PC is available
			// This path might be taken if ReplaceAttr removed the source map from JSONHandler
			fs := runtime.CallersFrames([]uintptr{r.PC})
			f, _ := fs.Next()
			if f.File != "" {
				sourceStr = fmt.Sprintf("%s:%d", filepath.Base(f.File), f.Line)
				sourceStr = maybeColorize(ColorGray, sourceStr)
			}
		}
	}

	// 4. Build the output string.
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

	// 5. Handle remaining attributes.
	if len(allAttrs) > 0 {
		sb.WriteString(" ") // Space before extra attributes
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
// serviceName: Name of the service (e.g., "orchestrator-service").
// logFormat: "json" for structured JSON output, "pretty" for human-readable console output.
// logLevel: Minimum level to log.
func New(serviceName string, logFormat string, logLevel slog.Level) *slog.Logger {
	var handler slog.Handler

	handlerOpts := slog.HandlerOptions{
		AddSource: true,
		Level:     logLevel,
		// ReplaceAttr can be defined here if needed globally
		// For example, to customize time format for JSONHandler as well.
		// ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
		// 	if a.Key == slog.TimeKey {
		// 		if t, ok := a.Value.Any().(time.Time); ok {
		// 			a.Value = slog.StringValue(t.Format("2006-01-02T15:04:05.000Z07:00"))
		// 		}
		// 	}
		// 	return a
		// },
	}

	commonAttrs := []slog.Attr{slog.String("service", serviceName)}

	switch strings.ToLower(logFormat) {
	case "pretty":
		// Colorize is true by default for pretty format in this setup
		pretty := NewPrettyHandler(os.Stdout, handlerOpts, true)
		handler = pretty.WithAttrs(commonAttrs)
	default: // "json" or any other value
		jsonHandler := slog.NewJSONHandler(os.Stdout, &handlerOpts)
		handler = jsonHandler.WithAttrs(commonAttrs)
	}

	return slog.New(handler)
}
