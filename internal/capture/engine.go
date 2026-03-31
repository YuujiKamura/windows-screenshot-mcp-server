package capture

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
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

// Capturer is the interface every capture backend must implement.
type Capturer interface {
	CaptureWindow(hwnd uintptr) (*CaptureResult, error)
	CaptureDesktop() (*CaptureResult, error)
	Name() Method
}

// Engine wraps one or more Capturers and provides automatic fallback.
type Engine struct {
	capturers []Capturer
}

// NewEngine builds an Engine with the requested preferred method.
// When preferred is MethodAuto the engine tries Graphics Capture,
// then PrintWindow, then BitBlt in order.
func NewEngine(preferred Method) *Engine {
	e := &Engine{}
	switch preferred {
	case MethodCapture:
		e.capturers = []Capturer{NewGraphicsCapture()}
	case MethodPrint:
		e.capturers = []Capturer{NewPrintWindow()}
	case MethodBitBlt:
		e.capturers = []Capturer{NewBitBlt()}
	default: // auto
		e.capturers = []Capturer{
			NewGraphicsCapture(),
			NewPrintWindow(),
			NewBitBlt(),
		}
	}
	return e
}

// CaptureWindow tries each capturer in order until one succeeds.
func (e *Engine) CaptureWindow(hwnd uintptr) (*CaptureResult, error) {
	var lastErr error
	for _, c := range e.capturers {
		result, err := c.CaptureWindow(hwnd)
		if err == nil {
			return result, nil
		}
		lastErr = fmt.Errorf("%s: %w", c.Name(), err)
	}
	return nil, fmt.Errorf("all capture methods failed, last: %w", lastErr)
}

// CaptureDesktop tries each capturer in order until one succeeds.
func (e *Engine) CaptureDesktop() (*CaptureResult, error) {
	var lastErr error
	for _, c := range e.capturers {
		result, err := c.CaptureDesktop()
		if err == nil {
			return result, nil
		}
		lastErr = fmt.Errorf("%s: %w", c.Name(), err)
	}
	return nil, fmt.Errorf("all capture methods failed, last: %w", lastErr)
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
