//go:build windows

package capture

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// =============================================================================
// File size sanity tests
// =============================================================================

// FileSizeExpectation checks whether a saved image file size is reasonable
// relative to its pixel dimensions.
func fileSizeSuspicious(width, height int, fileBytes int64) bool {
	// For a non-blank PNG, expect at least ~100 bytes per 1000 pixels.
	// A 1920x1080 blank PNG might be ~10KB (all same color compresses well).
	// A 1920x1080 real content PNG should be >> 100KB.
	totalPixels := int64(width) * int64(height)
	if totalPixels == 0 {
		return true
	}
	bytesPerPixel := float64(fileBytes) / float64(totalPixels)
	// Less than 0.01 bytes per pixel is suspiciously small for real content.
	return bytesPerPixel < 0.01
}

func TestFileSize_1920x1080_BlankPNG_SuspiciouslySmall(t *testing.T) {
	// A blank (all-black) 1920x1080 image compressed as PNG should be very small.
	img := makeUniformRGBA(1920, 1080, color.RGBA{0, 0, 0, 0})

	path := filepath.Join(t.TempDir(), "blank.png")
	if err := SaveImage(img, path, "png"); err != nil {
		t.Fatalf("SaveImage: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	t.Logf("Blank 1920x1080 PNG size: %d bytes", info.Size())

	// A blank image PNG of 1920x1080 should be suspiciously small (< ~20KB).
	if info.Size() > 50*1024 {
		t.Errorf("blank 1920x1080 PNG is %d bytes, expected < 50KB", info.Size())
	}

	if !fileSizeSuspicious(1920, 1080, info.Size()) {
		t.Log("fileSizeSuspicious did not flag blank image -- threshold may need adjustment")
	}
}

func TestFileSize_1920x1080_ContentPNG_ReasonableSize(t *testing.T) {
	// A content-rich 1920x1080 image should produce a larger PNG.
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	for y := 0; y < 1080; y++ {
		for x := 0; x < 1920; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((x * 7 + y * 13) % 256),
				G: uint8((x * 11 + y * 3) % 256),
				B: uint8((x * 5 + y * 17) % 256),
				A: 255,
			})
		}
	}

	path := filepath.Join(t.TempDir(), "content.png")
	if err := SaveImage(img, path, "png"); err != nil {
		t.Fatalf("SaveImage: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	t.Logf("Content 1920x1080 PNG size: %d bytes (%.2f MB)",
		info.Size(), float64(info.Size())/(1024*1024))

	// Real content should be > 100KB.
	if info.Size() < 100*1024 {
		t.Errorf("content 1920x1080 PNG is only %d bytes, expected > 100KB", info.Size())
	}

	if fileSizeSuspicious(1920, 1080, info.Size()) {
		t.Error("fileSizeSuspicious flagged content image as suspicious")
	}
}

func TestFileSize_FileSizeSuspicious_Function(t *testing.T) {
	tests := []struct {
		name      string
		width     int
		height    int
		fileBytes int64
		want      bool
	}{
		{"1920x1080 at 10KB -- suspicious", 1920, 1080, 10 * 1024, true},
		{"1920x1080 at 1.6MB -- normal", 1920, 1080, 1677721, false},
		{"1920x1080 at 100KB -- borderline", 1920, 1080, 100 * 1024, false},
		{"100x100 at 100 bytes -- suspicious", 100, 100, 100, true},
		{"100x100 at 5KB -- normal", 100, 100, 5 * 1024, false},
		{"0x0 at 0 bytes -- suspicious", 0, 0, 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fileSizeSuspicious(tc.width, tc.height, tc.fileBytes)
			if got != tc.want {
				t.Errorf("fileSizeSuspicious(%d, %d, %d) = %v, want %v",
					tc.width, tc.height, tc.fileBytes, got, tc.want)
			}
		})
	}
}

func TestFileSize_DesktopCapture_NotSuspicious(t *testing.T) {
	b := NewBitBlt()
	result, err := b.CaptureDesktop()
	if err != nil {
		t.Fatalf("CaptureDesktop: %v", err)
	}

	path := filepath.Join(t.TempDir(), "desktop.png")
	if err := SaveImage(result.Image, path, "png"); err != nil {
		t.Fatalf("SaveImage: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	t.Logf("Desktop capture %dx%d PNG size: %d bytes (%.2f MB)",
		result.Width, result.Height, info.Size(), float64(info.Size())/(1024*1024))

	if fileSizeSuspicious(result.Width, result.Height, info.Size()) {
		t.Error("desktop capture PNG file size is suspiciously small -- may be blank")
	}
}
