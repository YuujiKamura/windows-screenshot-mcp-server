package capture

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"time"
)

// Method identifies which capture technique is used.
type Method string

const (
	MethodAuto    Method = "auto"
	MethodCapture Method = "capture" // Graphics Capture API
	MethodPrint   Method = "print"   // PrintWindow
	MethodBitBlt  Method = "bitblt"  // BitBlt
)

// CaptureResult holds the captured image and metadata.
type CaptureResult struct {
	Image    image.Image
	Width    int
	Height   int
	Method   Method // which method actually succeeded
	Duration time.Duration
}

// AttemptTrace captures one method attempt for diagnostics.
type AttemptTrace struct {
	Method       Method        `json:"method"`
	Success      bool          `json:"success"`
	Duration     time.Duration `json:"duration"`
	FailureCode  string        `json:"failure_code,omitempty"`
	FailureError string        `json:"failure_error,omitempty"`
}

// Trace captures ordered attempts and final selection for diagnostics.
type Trace struct {
	RequestedMethod Method         `json:"requested_method"`
	Attempts        []AttemptTrace `json:"attempts"`
	SelectedMethod  Method         `json:"selected_method,omitempty"`
	StopReason      string         `json:"stop_reason,omitempty"`
	FallbackSummary string         `json:"fallback_summary,omitempty"`
}

// Capturer is the interface every capture backend must implement.
type Capturer interface {
	CaptureWindow(hwnd uintptr) (*CaptureResult, error)
	CaptureDesktop() (*CaptureResult, error)
	Name() Method
}

// Engine wraps one or more Capturers and provides automatic fallback.
type Engine struct {
	requested Method
	capturers []Capturer
}

// NewEngine builds an Engine with the requested preferred method.
// When preferred is MethodAuto the engine tries BitBlt first (most
// reliable on Windows 10), then PrintWindow, then Graphics Capture.
func NewEngine(preferred Method) *Engine {
	e := &Engine{requested: preferred}
	switch preferred {
	case MethodCapture:
		e.capturers = []Capturer{NewGraphicsCapture()}
	case MethodPrint:
		e.capturers = []Capturer{NewPrintWindow()}
	case MethodBitBlt:
		e.capturers = []Capturer{NewBitBlt()}
	default: // auto
		e.capturers = []Capturer{
			NewBitBlt(),
			NewPrintWindow(),
			NewGraphicsCapture(),
		}
	}
	return e
}

// CaptureWindow tries each capturer in order until one succeeds.
func (e *Engine) CaptureWindow(hwnd uintptr) (*CaptureResult, error) {
	result, _, err := e.CaptureWindowWithTrace(hwnd)
	return result, err
}

// CaptureDesktop tries each capturer in order until one succeeds.
func (e *Engine) CaptureDesktop() (*CaptureResult, error) {
	result, _, err := e.CaptureDesktopWithTrace()
	return result, err
}

// CaptureWindowWithTrace tries each capturer in order and returns diagnostics.
func (e *Engine) CaptureWindowWithTrace(hwnd uintptr) (*CaptureResult, *Trace, error) {
	trace := &Trace{RequestedMethod: e.requested}
	var lastErr error

	for _, c := range e.capturers {
		start := time.Now()
		result, err := c.CaptureWindow(hwnd)
		elapsed := time.Since(start)

		// Guard: even if the capturer reports success, reject blank images.
		if err == nil {
			if rgbaImg, ok := result.Image.(*image.RGBA); ok && isBlank(rgbaImg) {
				err = fmt.Errorf("%s produced a blank image", c.Name())
				result = nil
			}
		}

		attempt := AttemptTrace{
			Method:   c.Name(),
			Success:  err == nil,
			Duration: elapsed,
		}
		if err == nil {
			trace.Attempts = append(trace.Attempts, attempt)
			trace.SelectedMethod = result.Method
			finalizeTrace(trace)
			return result, trace, nil
		}
		attempt.FailureCode = classifyFailure(err)
		attempt.FailureError = err.Error()
		trace.Attempts = append(trace.Attempts, attempt)
		lastErr = fmt.Errorf("%s: %w", c.Name(), err)
	}

	finalizeTrace(trace)
	return nil, trace, fmt.Errorf("all capture methods failed, last: %w", lastErr)
}

// CaptureDesktopWithTrace tries each capturer in order and returns diagnostics.
func (e *Engine) CaptureDesktopWithTrace() (*CaptureResult, *Trace, error) {
	trace := &Trace{RequestedMethod: e.requested}
	var lastErr error

	for _, c := range e.capturers {
		start := time.Now()
		result, err := c.CaptureDesktop()
		elapsed := time.Since(start)

		// Guard: even if the capturer reports success, reject blank images.
		if err == nil {
			if rgbaImg, ok := result.Image.(*image.RGBA); ok && isBlank(rgbaImg) {
				err = fmt.Errorf("%s produced a blank image", c.Name())
				result = nil
			}
		}

		attempt := AttemptTrace{
			Method:   c.Name(),
			Success:  err == nil,
			Duration: elapsed,
		}
		if err == nil {
			trace.Attempts = append(trace.Attempts, attempt)
			trace.SelectedMethod = result.Method
			finalizeTrace(trace)
			return result, trace, nil
		}
		attempt.FailureCode = classifyFailure(err)
		attempt.FailureError = err.Error()
		trace.Attempts = append(trace.Attempts, attempt)
		lastErr = fmt.Errorf("%s: %w", c.Name(), err)
	}

	finalizeTrace(trace)
	return nil, trace, fmt.Errorf("all capture methods failed, last: %w", lastErr)
}

// SaveImage writes img to path in the given format ("png" or "jpeg"/"jpg").
func SaveImage(img image.Image, path string, format string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	switch format {
	case "jpeg", "jpg":
		return jpeg.Encode(f, img, &jpeg.Options{Quality: 95})
	default:
		return png.Encode(f, img)
	}
}

func classifyFailure(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "not a valid window"),
		strings.Contains(msg, "getwindowrect failed"),
		strings.Contains(msg, "no window matching"),
		strings.Contains(msg, "no visible window for pid"):
		return "NO_WINDOW"
	case strings.Contains(msg, "invalid window dimensions"):
		return "INVALID_BOUNDS"
	case strings.Contains(msg, "blank image"),
		strings.Contains(msg, "produced a blank image"):
		return "EMPTY_FRAME"
	case strings.Contains(msg, "does not support desktop"),
		strings.Contains(msg, "not supported"):
		return "API_UNSUPPORTED"
	case strings.Contains(msg, "timed out"),
		strings.Contains(msg, "timeout"):
		return "TIMEOUT"
	case strings.Contains(msg, "access denied"):
		return "ACCESS_DENIED"
	case strings.Contains(msg, "outside desktop bounds"):
		return "OUT_OF_BOUNDS"
	default:
		return "CAPTURE_FAILED"
	}
}

func finalizeTrace(trace *Trace) {
	if trace == nil {
		return
	}
	if len(trace.Attempts) == 0 {
		trace.StopReason = "NO_ATTEMPT"
		trace.FallbackSummary = "no capture attempts executed"
		return
	}
	if trace.SelectedMethod != "" {
		trace.StopReason = "FIRST_SUCCESS"
	} else {
		trace.StopReason = "ALL_FAILED"
	}
	trace.FallbackSummary = buildFallbackSummary(trace.Attempts, trace.SelectedMethod)
}

func buildFallbackSummary(attempts []AttemptTrace, selected Method) string {
	if len(attempts) == 0 {
		return "no capture attempts executed"
	}
	parts := make([]string, 0, len(attempts))
	for _, a := range attempts {
		name := string(a.Method)
		if a.Success {
			parts = append(parts, fmt.Sprintf("%s selected", name))
			continue
		}
		code := a.FailureCode
		if code == "" {
			code = "CAPTURE_FAILED"
		}
		parts = append(parts, fmt.Sprintf("%s failed (%s)", name, code))
	}
	if selected != "" {
		return strings.Join(parts, " -> ")
	}
	return strings.Join(parts, " -> ") + " -> no method selected"
}
