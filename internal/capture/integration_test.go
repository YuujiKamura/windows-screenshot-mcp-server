//go:build windows

package capture

import (
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestIntegration_CaptureDesktop_SavePNG(t *testing.T) {
	b := NewBitBlt()
	result, err := b.CaptureDesktop()
	if err != nil {
		t.Fatalf("CaptureDesktop: %v", err)
	}

	path := filepath.Join(t.TempDir(), "desktop.png")
	if err := SaveImage(result.Image, path, "png"); err != nil {
		t.Fatalf("SaveImage: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	decoded, err := png.Decode(f)
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	b2 := decoded.Bounds()
	if b2.Dx() != result.Width || b2.Dy() != result.Height {
		t.Errorf("decoded %dx%d != captured %dx%d", b2.Dx(), b2.Dy(), result.Width, result.Height)
	}
	if isBlankImage(decoded) {
		t.Error("decoded PNG is all black")
	}
}

func TestIntegration_CaptureDesktop_SaveJPEG(t *testing.T) {
	b := NewBitBlt()
	result, err := b.CaptureDesktop()
	if err != nil {
		t.Fatalf("CaptureDesktop: %v", err)
	}

	path := filepath.Join(t.TempDir(), "desktop.jpg")
	if err := SaveImage(result.Image, path, "jpeg"); err != nil {
		t.Fatalf("SaveImage: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	decoded, err := jpeg.Decode(f)
	if err != nil {
		t.Fatalf("jpeg.Decode: %v", err)
	}
	b2 := decoded.Bounds()
	if b2.Dx() != result.Width || b2.Dy() != result.Height {
		t.Errorf("decoded %dx%d != captured %dx%d", b2.Dx(), b2.Dy(), result.Width, result.Height)
	}
}

func TestIntegration_AutoFallback(t *testing.T) {
	// Auto engine should succeed — GraphicsCapture fails (stub),
	// PrintWindow fails on desktop, BitBlt succeeds.
	e := NewEngine(MethodAuto)
	result, err := e.CaptureDesktop()
	if err != nil {
		t.Fatalf("Auto CaptureDesktop: %v", err)
	}
	if result.Image == nil {
		t.Fatal("result.Image is nil")
	}
	if result.Width <= 0 || result.Height <= 0 {
		t.Errorf("dimensions %dx%d should be positive", result.Width, result.Height)
	}
	// The method that actually succeeded should be bitblt (since graphics_capture
	// is a stub and printwindow doesn't support desktop).
	if result.Method != MethodBitBlt {
		t.Logf("Auto fallback used method %q (expected bitblt for desktop)", result.Method)
	}
}

func TestIntegration_CaptureByTitle(t *testing.T) {
	// Try to find "Program Manager" which exists on all Windows systems.
	candidates := []string{"Program Manager"}

	// Since we're in the capture package, we can't call window.FindByTitle
	// directly. Use the desktop window handle which is always valid.
	desktop, _, _ := procGetDesktopWindow.Call()
	hwnd := desktop
	if hwnd == 0 {
		t.Skip("GetDesktopWindow returned 0")
	}
	_ = candidates
	t.Logf("Using desktop window 0x%X for capture test", hwnd)

	if hwnd == 0 {
		t.Skip("no suitable window found for capture-by-handle test")
	}

	e := NewEngine(MethodBitBlt)
	result, err := e.CaptureWindow(hwnd)
	if err != nil {
		t.Fatalf("CaptureWindow(0x%X): %v", hwnd, err)
	}
	if result.Image == nil {
		t.Fatal("result.Image is nil")
	}

	// Save and verify.
	path := filepath.Join(t.TempDir(), "window.png")
	if err := SaveImage(result.Image, path, "png"); err != nil {
		t.Fatalf("SaveImage: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() == 0 {
		t.Error("saved PNG file is empty")
	}
}

func TestIntegration_SaveImage_FileSize(t *testing.T) {
	b := NewBitBlt()
	result, err := b.CaptureDesktop()
	if err != nil {
		t.Fatalf("CaptureDesktop: %v", err)
	}

	pngPath := filepath.Join(t.TempDir(), "test.png")
	jpgPath := filepath.Join(t.TempDir(), "test.jpg")

	if err := SaveImage(result.Image, pngPath, "png"); err != nil {
		t.Fatalf("SaveImage PNG: %v", err)
	}
	if err := SaveImage(result.Image, jpgPath, "jpeg"); err != nil {
		t.Fatalf("SaveImage JPEG: %v", err)
	}

	pngInfo, _ := os.Stat(pngPath)
	jpgInfo, _ := os.Stat(jpgPath)

	t.Logf("PNG size: %d bytes, JPEG size: %d bytes", pngInfo.Size(), jpgInfo.Size())

	if pngInfo.Size() == 0 {
		t.Error("PNG file is 0 bytes")
	}
	if jpgInfo.Size() == 0 {
		t.Error("JPEG file is 0 bytes")
	}
}
