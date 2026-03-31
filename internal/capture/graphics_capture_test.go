//go:build windows

package capture

import (
	"image"
	"testing"
)

func TestGraphicsCapture_Name(t *testing.T) {
	g := NewGraphicsCapture()
	if g.Name() != MethodCapture {
		t.Errorf("Name() = %q, want %q", g.Name(), MethodCapture)
	}
}

func TestGraphicsCapture_CaptureDesktop(t *testing.T) {
	g := NewGraphicsCapture()
	result, err := g.CaptureDesktop()
	if err != nil {
		t.Skipf("CaptureDesktop not available in this environment: %v", err)
	}
	if result == nil {
		t.Fatal("CaptureDesktop returned nil result without error")
	}
	if result.Width <= 0 || result.Height <= 0 {
		t.Errorf("invalid dimensions: %dx%d", result.Width, result.Height)
	}
	if result.Image == nil {
		t.Fatal("CaptureDesktop returned nil image")
	}
	bounds := result.Image.Bounds()
	if bounds.Dx() != result.Width || bounds.Dy() != result.Height {
		t.Errorf("image bounds %v don't match reported size %dx%d", bounds, result.Width, result.Height)
	}
	if result.Method != MethodCapture {
		t.Errorf("Method = %q, want %q", result.Method, MethodCapture)
	}
	t.Logf("Desktop captured: %dx%d in %s", result.Width, result.Height, result.Duration)
}

func TestGraphicsCapture_CaptureWindow_DesktopWindow(t *testing.T) {
	desktop, _, _ := procGetDesktopWindow.Call()
	if desktop == 0 {
		t.Skip("GetDesktopWindow returned 0")
	}

	g := NewGraphicsCapture()
	result, err := g.CaptureWindow(desktop)
	if err != nil {
		t.Skipf("CaptureWindow not available in this environment: %v", err)
	}
	if result == nil {
		t.Fatal("CaptureWindow returned nil result without error")
	}
	if result.Width <= 0 || result.Height <= 0 {
		t.Errorf("invalid dimensions: %dx%d", result.Width, result.Height)
	}
	if result.Image == nil {
		t.Fatal("CaptureWindow returned nil image")
	}
	if result.Method != MethodCapture {
		t.Errorf("Method = %q, want %q", result.Method, MethodCapture)
	}
	t.Logf("Window captured: %dx%d in %s", result.Width, result.Height, result.Duration)
}

func TestGraphicsCapture_CaptureWindow_InvalidHwnd(t *testing.T) {
	g := NewGraphicsCapture()
	_, err := g.CaptureWindow(0xDEAD)
	if err == nil {
		t.Error("expected error for invalid HWND, got nil")
	}
}

func TestGraphicsCapture_ImageIsRGBA(t *testing.T) {
	g := NewGraphicsCapture()
	result, err := g.CaptureDesktop()
	if err != nil {
		t.Skipf("CaptureDesktop not available: %v", err)
	}
	if _, ok := result.Image.(*image.RGBA); !ok {
		t.Errorf("image type = %T, want *image.RGBA", result.Image)
	}
}
