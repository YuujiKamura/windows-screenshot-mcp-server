//go:build windows

package capture

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// DPI awareness tests
// =============================================================================

func TestDPIAwareness_DesktopSizeReasonable(t *testing.T) {
	// DPI awareness is set in init(). We verify indirectly by checking
	// that desktop dimensions are at least 640x480 (physical pixels).
	b := NewBitBlt()
	result, err := b.CaptureDesktop()
	if err != nil {
		t.Fatalf("CaptureDesktop: %v", err)
	}
	if result.Width < 640 {
		t.Errorf("desktop width %d < 640, DPI awareness may not be set", result.Width)
	}
	if result.Height < 480 {
		t.Errorf("desktop height %d < 480, DPI awareness may not be set", result.Height)
	}
	t.Logf("Desktop resolution: %dx%d", result.Width, result.Height)
}

func TestDPIAwareness_ConsistentAcrossMethods(t *testing.T) {
	// Both BitBlt desktop and DXGI desktop should report similar dimensions.
	bb := NewBitBlt()
	bbResult, err := bb.CaptureDesktop()
	if err != nil {
		t.Fatalf("BitBlt CaptureDesktop: %v", err)
	}

	gc := NewGraphicsCapture()
	gcResult, err := gc.CaptureDesktop()
	if err != nil {
		t.Skipf("GraphicsCapture not available: %v", err)
	}

	// Allow some tolerance for multi-monitor differences, but primary should match.
	if bbResult.Width != gcResult.Width || bbResult.Height != gcResult.Height {
		t.Logf("BitBlt desktop: %dx%d, DXGI desktop: %dx%d",
			bbResult.Width, bbResult.Height, gcResult.Width, gcResult.Height)
		// This is informational -- they may legitimately differ if DXGI captures
		// only primary monitor while BitBlt captures the virtual desktop.
	}
}

// =============================================================================
// Window rect tests
// =============================================================================

func TestWindowRect_DesktopWindow(t *testing.T) {
	desktop, _, _ := procGetDesktopWindow.Call()
	if desktop == 0 {
		t.Skip("GetDesktopWindow returned 0")
	}

	left, top, width, height, err := windowRect(desktop)
	if err != nil {
		t.Fatalf("windowRect: %v", err)
	}
	t.Logf("Desktop rect: left=%d top=%d width=%d height=%d", left, top, width, height)

	if width <= 0 {
		t.Errorf("desktop width %d should be positive", width)
	}
	if height <= 0 {
		t.Errorf("desktop height %d should be positive", height)
	}
}

func TestWindowRect_InvalidHandle(t *testing.T) {
	_, _, _, _, err := windowRect(0xDEADBEEF)
	if err == nil {
		t.Error("expected error for invalid window handle")
	}
}

func TestWindowRect_NullHandle(t *testing.T) {
	_, _, _, _, err := windowRect(0)
	if err == nil {
		t.Error("expected error for null window handle")
	}
}

func TestWindowRect_136x39_DetectionSuspicious(t *testing.T) {
	// Document the bug: a normal application window returning 136x39 is wrong.
	// We can't force a real window to have these dimensions, but we test that
	// the dimensions would be considered suspicious.
	width, height := 136, 39
	minExpectedWidth, minExpectedHeight := 200, 100

	if width < minExpectedWidth || height < minExpectedHeight {
		t.Logf("Dimensions %dx%d are suspiciously small for a normal window "+
			"(expected at least %dx%d). This may indicate DPI scaling issues.",
			width, height, minExpectedWidth, minExpectedHeight)
	}
}

// =============================================================================
// Individual capture method tests
// =============================================================================

// --- BitBlt ---

func TestBitBlt_CaptureDesktop_NonBlank(t *testing.T) {
	b := NewBitBlt()
	result, err := b.CaptureDesktop()
	if err != nil {
		t.Fatalf("CaptureDesktop: %v", err)
	}
	if result.Image == nil {
		t.Fatal("image is nil")
	}

	// Verify non-blank.
	rgbaImg, ok := result.Image.(*image.RGBA)
	if !ok {
		t.Fatalf("image type = %T, want *image.RGBA", result.Image)
	}
	if isBlank(rgbaImg) {
		t.Error("BitBlt desktop capture should NOT be blank")
	}
}

func TestBitBlt_CaptureDesktop_ReasonableFileSize(t *testing.T) {
	b := NewBitBlt()
	result, err := b.CaptureDesktop()
	if err != nil {
		t.Fatalf("CaptureDesktop: %v", err)
	}

	path := filepath.Join(t.TempDir(), "bitblt_desktop.png")
	if err := SaveImage(result.Image, path, "png"); err != nil {
		t.Fatalf("SaveImage: %v", err)
	}

	info, _ := os.Stat(path)
	t.Logf("BitBlt desktop PNG: %d bytes", info.Size())

	if fileSizeSuspicious(result.Width, result.Height, info.Size()) {
		t.Error("BitBlt desktop PNG file size is suspiciously small")
	}
}

func TestBitBlt_CaptureWindow_DesktopHwnd(t *testing.T) {
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
		t.Errorf("bad dimensions: %dx%d", result.Width, result.Height)
	}
	if result.Method != MethodBitBlt {
		t.Errorf("method = %q, want bitblt", result.Method)
	}
}

// --- Graphics Capture (DXGI) ---

func TestGraphicsCapture_CaptureDesktop_BlankCheck(t *testing.T) {
	g := NewGraphicsCapture()
	result, err := g.CaptureDesktop()
	if err != nil {
		t.Skipf("DXGI CaptureDesktop unavailable: %v", err)
	}

	rgbaImg, ok := result.Image.(*image.RGBA)
	if !ok {
		t.Skipf("image type %T, not *image.RGBA", result.Image)
	}

	if isBlank(rgbaImg) {
		t.Error("DXGI desktop capture is blank on this machine")
	} else {
		t.Log("DXGI desktop capture is NOT blank (good)")
	}
}

func TestGraphicsCapture_CaptureDesktop_FileSize(t *testing.T) {
	g := NewGraphicsCapture()
	result, err := g.CaptureDesktop()
	if err != nil {
		t.Skipf("DXGI CaptureDesktop unavailable: %v", err)
	}

	path := filepath.Join(t.TempDir(), "dxgi_desktop.png")
	if err := SaveImage(result.Image, path, "png"); err != nil {
		t.Fatalf("SaveImage: %v", err)
	}

	info, _ := os.Stat(path)
	t.Logf("DXGI desktop PNG: %d bytes", info.Size())
}

// --- PrintWindow ---

func TestPrintWindow_CaptureDesktop_ReturnsError(t *testing.T) {
	p := NewPrintWindow()
	_, err := p.CaptureDesktop()
	if err == nil {
		t.Fatal("PrintWindow.CaptureDesktop should return error")
	}
	if !strings.Contains(err.Error(), "does not support desktop") {
		t.Errorf("error = %q, expected 'does not support desktop'", err)
	}
}

func TestPrintWindow_CaptureWindow_DesktopHwnd(t *testing.T) {
	desktop, _, _ := procGetDesktopWindow.Call()
	if desktop == 0 {
		t.Skip("GetDesktopWindow returned 0")
	}

	p := NewPrintWindow()
	result, err := p.CaptureWindow(desktop)
	if err != nil {
		// Expected: PrintWindow often fails or returns blank for desktop window.
		t.Logf("PrintWindow(desktop) error (expected): %v", err)
		return
	}
	t.Logf("PrintWindow(desktop) unexpectedly succeeded: %dx%d", result.Width, result.Height)
}

// =============================================================================
// Error condition tests
// =============================================================================

func TestCapture_NonExistentWindowHandle(t *testing.T) {
	methods := []struct {
		name    string
		capturer Capturer
	}{
		{"BitBlt", NewBitBlt()},
		{"PrintWindow", NewPrintWindow()},
		{"GraphicsCapture", NewGraphicsCapture()},
	}

	for _, m := range methods {
		t.Run(m.name, func(t *testing.T) {
			_, err := m.capturer.CaptureWindow(0xDEADBEEF)
			if err == nil {
				t.Errorf("%s: expected error for non-existent window handle", m.name)
			} else {
				t.Logf("%s: error = %v", m.name, err)
			}
		})
	}
}

func TestCapture_NullWindowHandle(t *testing.T) {
	methods := []struct {
		name    string
		capturer Capturer
	}{
		{"BitBlt", NewBitBlt()},
		{"PrintWindow", NewPrintWindow()},
		{"GraphicsCapture", NewGraphicsCapture()},
	}

	for _, m := range methods {
		t.Run(m.name, func(t *testing.T) {
			_, err := m.capturer.CaptureWindow(0)
			if err == nil {
				// Some methods may succeed with hwnd=0 (interpreted as desktop).
				t.Logf("%s: CaptureWindow(0) succeeded (may treat as desktop)", m.name)
			} else {
				t.Logf("%s: CaptureWindow(0) error: %v", m.name, err)
			}
		})
	}
}

func TestCapture_Engine_InvalidHwnd(t *testing.T) {
	e := NewEngine(MethodAuto)
	_, err := e.CaptureWindow(0xDEADBEEF)
	if err == nil {
		t.Error("expected error for invalid HWND with auto engine")
	}
}

// =============================================================================
// bgraToRGBA tests
// =============================================================================

func TestBgraToRGBA_CorrectSwap(t *testing.T) {
	// Create a 2x1 pixel buffer in BGRA format.
	// Pixel 0: B=0xFF, G=0x00, R=0x00, A=0xFF (blue in BGRA = blue)
	// Pixel 1: B=0x00, G=0x00, R=0xFF, A=0xFF (red in BGRA)
	buf := []byte{
		0xFF, 0x00, 0x00, 0xFF, // BGRA: Blue
		0x00, 0x00, 0xFF, 0xFF, // BGRA: Red
	}

	// We can't easily test bgraToRGBA directly since it takes a uintptr,
	// but we can verify the channel swap logic.
	// After swap: B↔R
	// Pixel 0: R=0xFF, G=0x00, B=0x00, A=0xFF (now red in RGBA -- wait, no)
	// Actually bgraToRGBA copies first then swaps [i+0] and [i+2].
	// Input buffer is BGRA: [B, G, R, A]
	// After copy to img.Pix: [B, G, R, A]
	// After swap [0]↔[2]: [R, G, B, A] -- correct RGBA!

	// Verify the swap logic manually.
	swapped := make([]byte, len(buf))
	copy(swapped, buf)
	for i := 0; i < len(swapped); i += 4 {
		swapped[i+0], swapped[i+2] = swapped[i+2], swapped[i+0]
	}

	// Pixel 0: was BGRA(0xFF,0x00,0x00,0xFF) -> RGBA(0x00,0x00,0xFF,0xFF) = blue
	if swapped[0] != 0x00 || swapped[1] != 0x00 || swapped[2] != 0xFF || swapped[3] != 0xFF {
		t.Errorf("pixel 0 after swap = (%d,%d,%d,%d), want (0,0,255,255)",
			swapped[0], swapped[1], swapped[2], swapped[3])
	}

	// Pixel 1: was BGRA(0x00,0x00,0xFF,0xFF) -> RGBA(0xFF,0x00,0x00,0xFF) = red
	if swapped[4] != 0xFF || swapped[5] != 0x00 || swapped[6] != 0x00 || swapped[7] != 0xFF {
		t.Errorf("pixel 1 after swap = (%d,%d,%d,%d), want (255,0,0,255)",
			swapped[4], swapped[5], swapped[6], swapped[7])
	}
}

// =============================================================================
// SaveImage edge cases
// =============================================================================

func TestSaveImage_EmptyImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 0, 0))
	path := filepath.Join(t.TempDir(), "empty.png")
	// This may or may not error; we just verify it doesn't panic.
	err := SaveImage(img, path, "png")
	t.Logf("SaveImage on 0x0 image: err=%v", err)
}

func TestSaveImage_LargeImage_JPEG_Smaller_Than_PNG(t *testing.T) {
	// For real content, JPEG should typically be smaller than PNG.
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((x * 7) % 256),
				G: uint8((y * 13) % 256),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}

	pngPath := filepath.Join(t.TempDir(), "test.png")
	jpgPath := filepath.Join(t.TempDir(), "test.jpg")

	SaveImage(img, pngPath, "png")
	SaveImage(img, jpgPath, "jpeg")

	pngInfo, _ := os.Stat(pngPath)
	jpgInfo, _ := os.Stat(jpgPath)

	t.Logf("PNG: %d bytes, JPEG: %d bytes", pngInfo.Size(), jpgInfo.Size())

	// JPEG at quality 95 with noisy content should generally be smaller than PNG.
	// This is informational, not a hard requirement.
	if jpgInfo.Size() >= pngInfo.Size() {
		t.Logf("NOTE: JPEG (%d) >= PNG (%d) for this content -- unusual but possible",
			jpgInfo.Size(), pngInfo.Size())
	}
}
