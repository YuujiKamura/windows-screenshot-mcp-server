//go:build windows

package capture

import (
	"image"
	"image/color"
	"testing"
)

func TestBitBlt_Name(t *testing.T) {
	b := NewBitBlt()
	if b.Name() != MethodBitBlt {
		t.Errorf("Name() = %q, want %q", b.Name(), MethodBitBlt)
	}
}

func TestBitBlt_CaptureDesktop(t *testing.T) {
	b := NewBitBlt()
	result, err := b.CaptureDesktop()
	if err != nil {
		t.Fatalf("CaptureDesktop: %v", err)
	}
	if result.Image == nil {
		t.Fatal("Image is nil")
	}
	if result.Width <= 0 || result.Height <= 0 {
		t.Errorf("dimensions %dx%d should be positive", result.Width, result.Height)
	}
	if result.Method != MethodBitBlt {
		t.Errorf("Method = %q, want %q", result.Method, MethodBitBlt)
	}
	if result.Duration <= 0 {
		t.Error("Duration should be positive")
	}
}

func TestBitBlt_CaptureWindow_Desktop(t *testing.T) {
	desktop, _, _ := procGetDesktopWindow.Call()
	if desktop == 0 {
		t.Skip("GetDesktopWindow returned 0")
	}

	b := NewBitBlt()
	result, err := b.CaptureWindow(desktop)
	if err != nil {
		t.Fatalf("CaptureWindow(desktop): %v", err)
	}
	if result.Width <= 0 || result.Height <= 0 {
		t.Errorf("dimensions %dx%d should be positive", result.Width, result.Height)
	}
}

func TestBitBlt_CaptureWindow_InvalidHandle(t *testing.T) {
	b := NewBitBlt()
	_, err := b.CaptureWindow(0xDEADBEEF)
	if err == nil {
		t.Fatal("expected error for invalid HWND")
	}
}

func TestBitBlt_ImageNotBlank(t *testing.T) {
	b := NewBitBlt()
	result, err := b.CaptureDesktop()
	if err != nil {
		t.Fatalf("CaptureDesktop: %v", err)
	}

	if isBlankImage(result.Image) {
		t.Error("captured desktop image is all black/zero — expected non-blank content")
	}
}

func TestBitBlt_DPIAwareness(t *testing.T) {
	// DPI awareness is set in init(). We verify indirectly: the desktop
	// capture should return dimensions that match the actual screen resolution,
	// not the DPI-scaled values. At minimum, width and height should be
	// reasonable (> 640x480).
	b := NewBitBlt()
	result, err := b.CaptureDesktop()
	if err != nil {
		t.Fatalf("CaptureDesktop: %v", err)
	}
	if result.Width < 640 || result.Height < 480 {
		t.Errorf("desktop capture %dx%d seems too small — DPI awareness may not be set",
			result.Width, result.Height)
	}
}

func TestBitBlt_ImageDimensions_MatchResult(t *testing.T) {
	b := NewBitBlt()
	result, err := b.CaptureDesktop()
	if err != nil {
		t.Fatalf("CaptureDesktop: %v", err)
	}
	bounds := result.Image.Bounds()
	if bounds.Dx() != result.Width || bounds.Dy() != result.Height {
		t.Errorf("Image bounds %dx%d != reported %dx%d",
			bounds.Dx(), bounds.Dy(), result.Width, result.Height)
	}
}

// --- helpers -----------------------------------------------------------------

// isBlankImage checks if all pixels are black/zero.
func isBlankImage(img image.Image) bool {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y += bounds.Dy() / 10 {
		for x := bounds.Min.X; x < bounds.Max.X; x += bounds.Dx() / 10 {
			r, g, b, _ := img.At(x, y).RGBA()
			if r != 0 || g != 0 || b != 0 {
				return false
			}
		}
	}
	// Also check center pixel.
	cx := (bounds.Min.X + bounds.Max.X) / 2
	cy := (bounds.Min.Y + bounds.Max.Y) / 2
	r, g, b, _ := img.At(cx, cy).RGBA()
	if r != 0 || g != 0 || b != 0 {
		return false
	}
	return true
}

// isBlankRGBA is a typed helper for *image.RGBA.
func isBlankRGBA(img *image.RGBA) bool {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y += max(1, bounds.Dy()/10) {
		for x := bounds.Min.X; x < bounds.Max.X; x += max(1, bounds.Dx()/10) {
			c := img.RGBAAt(x, y)
			if c != (color.RGBA{0, 0, 0, 0}) {
				return false
			}
		}
	}
	return true
}
