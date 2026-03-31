//go:build windows

package capture

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// --- mock capturer -----------------------------------------------------------

type mockCapturer struct {
	name    Method
	err     error
	result  *CaptureResult
	called  int
}

func (m *mockCapturer) Name() Method { return m.name }

func (m *mockCapturer) CaptureWindow(hwnd uintptr) (*CaptureResult, error) {
	m.called++
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func (m *mockCapturer) CaptureDesktop() (*CaptureResult, error) {
	m.called++
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func newMockOK(name Method) *mockCapturer {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	// Fill with non-black pixels so it's not blank.
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	return &mockCapturer{
		name: name,
		result: &CaptureResult{
			Image:  img,
			Width:  100,
			Height: 100,
			Method: name,
		},
	}
}

func newMockFail(name Method) *mockCapturer {
	return &mockCapturer{
		name: name,
		err:  image.ErrFormat,
	}
}

// --- tests -------------------------------------------------------------------

func TestNewEngine_Auto(t *testing.T) {
	e := NewEngine(MethodAuto)
	if len(e.capturers) != 3 {
		t.Fatalf("expected 3 capturers for auto, got %d", len(e.capturers))
	}
	names := []Method{MethodCapture, MethodPrint, MethodBitBlt}
	for i, want := range names {
		got := e.capturers[i].Name()
		if got != want {
			t.Errorf("capturer[%d] name = %q, want %q", i, got, want)
		}
	}
}

func TestNewEngine_Specific(t *testing.T) {
	for _, m := range []Method{MethodCapture, MethodPrint, MethodBitBlt} {
		e := NewEngine(m)
		if len(e.capturers) != 1 {
			t.Errorf("NewEngine(%s): expected 1 capturer, got %d", m, len(e.capturers))
		}
		if e.capturers[0].Name() != m {
			t.Errorf("NewEngine(%s): capturer name = %s", m, e.capturers[0].Name())
		}
	}
}

func TestEngine_FallbackOrder(t *testing.T) {
	fail1 := newMockFail("first")
	fail2 := newMockFail("second")
	ok3 := newMockOK("third")

	e := &Engine{capturers: []Capturer{fail1, fail2, ok3}}

	res, err := e.CaptureDesktop()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "third" {
		t.Errorf("expected method 'third', got %q", res.Method)
	}
	if fail1.called != 1 || fail2.called != 1 || ok3.called != 1 {
		t.Errorf("call counts: fail1=%d fail2=%d ok3=%d", fail1.called, fail2.called, ok3.called)
	}
}

func TestEngine_AllMethodsFail(t *testing.T) {
	e := &Engine{capturers: []Capturer{
		newMockFail("a"),
		newMockFail("b"),
	}}

	_, err := e.CaptureDesktop()
	if err == nil {
		t.Fatal("expected error when all methods fail")
	}
	_, errW := e.CaptureWindow(0x1234)
	if errW == nil {
		t.Fatal("expected error when all methods fail for CaptureWindow")
	}
}

func TestEngine_FallbackOrder_CaptureWindow(t *testing.T) {
	fail1 := newMockFail("first")
	ok2 := newMockOK("second")

	e := &Engine{capturers: []Capturer{fail1, ok2}}

	res, err := e.CaptureWindow(0x1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "second" {
		t.Errorf("expected method 'second', got %q", res.Method)
	}
}

// --- SaveImage tests ---------------------------------------------------------

func tempFile(t *testing.T, ext string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test"+ext)
}

func testImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 4), G: uint8(y * 4), B: 128, A: 255,
			})
		}
	}
	return img
}

func TestSaveImage_PNG(t *testing.T) {
	path := tempFile(t, ".png")
	img := testImage()

	if err := SaveImage(img, path, "png"); err != nil {
		t.Fatalf("SaveImage PNG: %v", err)
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
	b := decoded.Bounds()
	if b.Dx() != 64 || b.Dy() != 64 {
		t.Errorf("decoded size = %dx%d, want 64x64", b.Dx(), b.Dy())
	}
}

func TestSaveImage_JPEG(t *testing.T) {
	path := tempFile(t, ".jpg")
	img := testImage()

	if err := SaveImage(img, path, "jpeg"); err != nil {
		t.Fatalf("SaveImage JPEG: %v", err)
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
	b := decoded.Bounds()
	if b.Dx() != 64 || b.Dy() != 64 {
		t.Errorf("decoded size = %dx%d, want 64x64", b.Dx(), b.Dy())
	}
}

func TestSaveImage_JPG_Alias(t *testing.T) {
	path := tempFile(t, ".jpg")
	img := testImage()

	if err := SaveImage(img, path, "jpg"); err != nil {
		t.Fatalf("SaveImage jpg: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	// Should be decodable as JPEG.
	if _, err := jpeg.Decode(f); err != nil {
		t.Fatalf("jpeg.Decode on 'jpg' format: %v", err)
	}
}

func TestSaveImage_InvalidPath(t *testing.T) {
	img := testImage()
	err := SaveImage(img, filepath.Join(t.TempDir(), "no", "such", "dir", "img.png"), "png")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestSaveImage_DefaultFormat(t *testing.T) {
	path := tempFile(t, ".png")
	img := testImage()

	// Unknown format should default to PNG.
	if err := SaveImage(img, path, "bmp"); err != nil {
		t.Fatalf("SaveImage default: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	if _, err := png.Decode(f); err != nil {
		t.Fatalf("expected PNG for unknown format: %v", err)
	}
}
