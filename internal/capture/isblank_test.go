//go:build windows

package capture

import (
	"image"
	"image/color"
	"testing"
)

// =============================================================================
// isBlank detection tests
// =============================================================================

// makeUniformRGBA creates a w x h image filled with a single RGBA color.
func makeUniformRGBA(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+0] = c.R
		img.Pix[i+1] = c.G
		img.Pix[i+2] = c.B
		img.Pix[i+3] = c.A
	}
	return img
}

func TestIsBlank_AllBlack_RGBA0000(t *testing.T) {
	img := makeUniformRGBA(100, 100, color.RGBA{0, 0, 0, 0})
	if !isBlank(img) {
		t.Error("all-black RGBA{0,0,0,0} image should be detected as blank")
	}
}

func TestIsBlank_AllWhite_RGBA255255255255(t *testing.T) {
	// KNOWN BUG: isBlank checks allWhite but the current sampling approach
	// should detect this. If this test fails, the white detection is broken.
	img := makeUniformRGBA(100, 100, color.RGBA{255, 255, 255, 255})
	if !isBlank(img) {
		t.Error("all-white RGBA{255,255,255,255} image should be detected as blank (CURRENTLY BROKEN if this fails)")
	}
}

func TestIsBlank_AllGray_RGBA128128128255(t *testing.T) {
	img := makeUniformRGBA(100, 100, color.RGBA{128, 128, 128, 255})
	if isBlank(img) {
		t.Error("all-gray RGBA{128,128,128,255} image should NOT be blank")
	}
}

func TestIsBlank_OneNonZeroPixel(t *testing.T) {
	// Start with all-black, then set one pixel to a visible color.
	img := makeUniformRGBA(100, 100, color.RGBA{0, 0, 0, 0})
	// Place the non-zero pixel at center (50,50) which is one of the sample points.
	img.SetRGBA(50, 50, color.RGBA{255, 0, 0, 255})
	if isBlank(img) {
		t.Error("image with one non-zero pixel at center should NOT be blank")
	}
}

func TestIsBlank_OneNonZeroPixel_NotAtSamplePoint(t *testing.T) {
	// Place non-zero pixel at a location NOT sampled by isBlank.
	// isBlank samples at (w/4,h/4), (w/2,h/2), (3w/4,3h/4)
	// For 100x100: (25,25), (50,50), (75,75)
	// Put a pixel at (10,10) which won't be sampled.
	img := makeUniformRGBA(100, 100, color.RGBA{0, 0, 0, 0})
	img.SetRGBA(10, 10, color.RGBA{255, 0, 0, 255})
	// This WILL appear blank because isBlank only samples 3 points.
	// This documents the sampling limitation.
	if !isBlank(img) {
		t.Log("isBlank correctly detected non-zero pixel not at sample points (unexpected but good)")
	} else {
		t.Log("isBlank missed non-zero pixel at (10,10) -- expected due to sparse sampling")
	}
}

func TestIsBlank_1x1_Black(t *testing.T) {
	img := makeUniformRGBA(1, 1, color.RGBA{0, 0, 0, 0})
	if !isBlank(img) {
		t.Error("1x1 black image should be blank")
	}
}

func TestIsBlank_1x1_White(t *testing.T) {
	img := makeUniformRGBA(1, 1, color.RGBA{255, 255, 255, 255})
	if !isBlank(img) {
		t.Error("1x1 white image should be blank")
	}
}

func TestIsBlank_RealContent_VariedPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	// Fill with a gradient pattern.
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	if isBlank(img) {
		t.Error("varied-pixel image should NOT be blank")
	}
}

func TestIsBlank_WhiteBorderContentCenter(t *testing.T) {
	// White border with colored content in the center.
	img := makeUniformRGBA(200, 200, color.RGBA{255, 255, 255, 255})
	// Fill center 100x100 region with red.
	for y := 50; y < 150; y++ {
		for x := 50; x < 150; x++ {
			img.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	// Sample points for 200x200: (50,50), (100,100), (150,150)
	// (50,50) is at the boundary, (100,100) is in center (red), (150,150) is at boundary.
	if isBlank(img) {
		t.Error("image with white border but red center should NOT be blank")
	}
}

func TestIsBlank_VerySmallImage_136x39(t *testing.T) {
	// This size was seen in a bug report for incorrectly-sized window captures.
	// Test with all-black: should be blank.
	imgBlack := makeUniformRGBA(136, 39, color.RGBA{0, 0, 0, 0})
	if !isBlank(imgBlack) {
		t.Error("136x39 all-black image should be blank")
	}

	// Test with content: should NOT be blank.
	imgContent := makeUniformRGBA(136, 39, color.RGBA{128, 64, 32, 255})
	if isBlank(imgContent) {
		t.Error("136x39 image with content should NOT be blank")
	}
}

func TestIsBlank_Transparent_RGBA0000(t *testing.T) {
	img := makeUniformRGBA(100, 100, color.RGBA{0, 0, 0, 0})
	if !isBlank(img) {
		t.Error("fully transparent image RGBA{0,0,0,0} should be blank")
	}
}

func TestIsBlank_SemiTransparent_RGBA255255255128(t *testing.T) {
	img := makeUniformRGBA(100, 100, color.RGBA{255, 255, 255, 128})
	if isBlank(img) {
		t.Error("semi-transparent RGBA{255,255,255,128} should NOT be blank")
	}
}

func TestIsBlank_LargeImage_1920x1080_Black(t *testing.T) {
	img := makeUniformRGBA(1920, 1080, color.RGBA{0, 0, 0, 0})
	if !isBlank(img) {
		t.Error("1920x1080 all-black image should be blank")
	}
}

func TestIsBlank_LargeImage_1920x1080_White(t *testing.T) {
	img := makeUniformRGBA(1920, 1080, color.RGBA{255, 255, 255, 255})
	if !isBlank(img) {
		t.Error("1920x1080 all-white image should be blank")
	}
}

func TestIsBlank_LargeImage_1920x1080_Content(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	// Fill with a gradient.
	for y := 0; y < 1080; y++ {
		for x := 0; x < 1920; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255,
			})
		}
	}
	if isBlank(img) {
		t.Error("1920x1080 content image should NOT be blank")
	}
}

func TestIsBlank_AlmostBlack_OneChannelNonZero(t *testing.T) {
	// Image where R=1, G=0, B=0, A=0 -- very dark but not exactly black.
	img := makeUniformRGBA(100, 100, color.RGBA{1, 0, 0, 0})
	if isBlank(img) {
		t.Error("RGBA{1,0,0,0} should NOT be blank (differs from {0,0,0,0})")
	}
}

func TestIsBlank_AlmostWhite_254(t *testing.T) {
	// Image where all channels are 254, not exactly 255.
	img := makeUniformRGBA(100, 100, color.RGBA{254, 254, 254, 254})
	if isBlank(img) {
		t.Error("RGBA{254,254,254,254} should NOT be blank (not exactly white)")
	}
}

func TestIsBlank_SamplingPointsDocumentation(t *testing.T) {
	// Document what isBlank actually samples for a 400x400 image:
	// (100,100), (200,200), (300,300)
	img := makeUniformRGBA(400, 400, color.RGBA{0, 0, 0, 0})

	// Set only (200,200) to red -- the center sample point.
	img.SetRGBA(200, 200, color.RGBA{255, 0, 0, 255})

	if isBlank(img) {
		t.Error("image with non-zero pixel at sample point (200,200) should NOT be blank")
	}
}

func TestIsBlank_ZeroSizeImage(t *testing.T) {
	// Edge case: 0x0 image. isBlank samples at (0,0) which may panic or be out of bounds.
	// This tests robustness.
	img := image.NewRGBA(image.Rect(0, 0, 0, 0))
	// Note: with a 0x0 image, sample points all become (0,0) and RGBAAt returns zero value.
	// This should return blank=true (allBlack=true).
	result := isBlank(img)
	t.Logf("isBlank on 0x0 image returned %v", result)
	// We just verify it doesn't panic.
}
