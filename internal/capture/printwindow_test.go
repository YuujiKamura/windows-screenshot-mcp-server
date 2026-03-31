//go:build windows

package capture

import (
	"strings"
	"testing"
)

func TestPrintWindow_Name(t *testing.T) {
	p := NewPrintWindow()
	if p.Name() != MethodPrint {
		t.Errorf("Name() = %q, want %q", p.Name(), MethodPrint)
	}
}

func TestPrintWindow_CaptureDesktop_Fails(t *testing.T) {
	p := NewPrintWindow()
	_, err := p.CaptureDesktop()
	if err == nil {
		t.Fatal("expected error for CaptureDesktop with PrintWindow")
	}
	if !strings.Contains(err.Error(), "does not support desktop") {
		t.Errorf("error = %q, expected it to mention desktop not supported", err)
	}
}

func TestPrintWindow_CaptureWindow_InvalidHandle(t *testing.T) {
	p := NewPrintWindow()
	_, err := p.CaptureWindow(0xDEADBEEF)
	if err == nil {
		t.Fatal("expected error for invalid HWND")
	}
}

func TestPrintWindow_CaptureWindow_Desktop(t *testing.T) {
	// PrintWindow on the desktop HWND typically fails or produces a blank
	// image. Either an error or a blank-check error is acceptable.
	desktop, _, _ := procGetDesktopWindow.Call()
	if desktop == 0 {
		t.Skip("GetDesktopWindow returned 0")
	}

	p := NewPrintWindow()
	result, err := p.CaptureWindow(desktop)
	if err != nil {
		// This is the expected path — PrintWindow often fails on the desktop.
		t.Logf("CaptureWindow(desktop) returned error (expected): %v", err)
		return
	}
	// If it somehow succeeded, verify we got a valid image.
	if result.Image == nil {
		t.Error("result.Image is nil but no error returned")
	}
}
