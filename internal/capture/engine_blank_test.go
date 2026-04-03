//go:build windows

package capture

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// newMockBlank returns a mock capturer that succeeds but returns a blank
// (all-white or all-black) image — simulating a capturer that forgot to
// call isBlank internally.
func newMockBlank(name Method, c color.RGBA) *mockCapturer {
	img := makeUniformRGBA(100, 100, c)
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

// ---------------------------------------------------------------------------
// Issue #3: engine must reject blank images returned by capturers
// ---------------------------------------------------------------------------

func TestEngine_RejectsBlankWhiteImage_Desktop(t *testing.T) {
	// A capturer returns a white image without error.
	// The engine should treat this as failure and not return the image.
	white := newMockBlank("buggy", color.RGBA{255, 255, 255, 255})
	ok := newMockOK("good")

	e := &Engine{requested: MethodAuto, capturers: []Capturer{white, ok}}

	res, err := e.CaptureDesktop()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "good" {
		t.Errorf("expected fallback to 'good', got %q", res.Method)
	}
	if white.called != 1 {
		t.Errorf("blank capturer should have been called once, got %d", white.called)
	}
}

func TestEngine_RejectsBlankWhiteImage_Window(t *testing.T) {
	white := newMockBlank("buggy", color.RGBA{255, 255, 255, 255})
	ok := newMockOK("good")

	e := &Engine{requested: MethodAuto, capturers: []Capturer{white, ok}}

	res, err := e.CaptureWindow(0x1234)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "good" {
		t.Errorf("expected fallback to 'good', got %q", res.Method)
	}
}

func TestEngine_RejectsBlankBlackImage(t *testing.T) {
	black := newMockBlank("buggy", color.RGBA{0, 0, 0, 0})
	ok := newMockOK("good")

	e := &Engine{requested: MethodAuto, capturers: []Capturer{black, ok}}

	res, err := e.CaptureDesktop()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "good" {
		t.Errorf("expected fallback to 'good', got %q", res.Method)
	}
}

func TestEngine_AllBlankImages_ReturnsError_Desktop(t *testing.T) {
	// All capturers return blank images without error.
	// Engine should return an error — never return a blank image.
	white1 := newMockBlank("a", color.RGBA{255, 255, 255, 255})
	white2 := newMockBlank("b", color.RGBA{255, 255, 255, 255})
	black3 := newMockBlank("c", color.RGBA{0, 0, 0, 0})

	e := &Engine{requested: MethodAuto, capturers: []Capturer{white1, white2, black3}}

	_, err := e.CaptureDesktop()
	if err == nil {
		t.Fatal("expected error when all capturers return blank images")
	}
	if !strings.Contains(err.Error(), "all capture methods failed") {
		t.Errorf("error = %q, want 'all capture methods failed'", err)
	}
}

func TestEngine_AllBlankImages_ReturnsError_Window(t *testing.T) {
	white1 := newMockBlank("a", color.RGBA{255, 255, 255, 255})
	white2 := newMockBlank("b", color.RGBA{255, 255, 255, 255})

	e := &Engine{requested: MethodAuto, capturers: []Capturer{white1, white2}}

	_, err := e.CaptureWindow(0x1234)
	if err == nil {
		t.Fatal("expected error when all capturers return blank images for CaptureWindow")
	}
}

func TestEngine_BlankImage_TraceDiagnostics(t *testing.T) {
	white := newMockBlank("first", color.RGBA{255, 255, 255, 255})
	ok := newMockOK("second")

	e := &Engine{requested: MethodAuto, capturers: []Capturer{white, ok}}

	res, trace, err := e.CaptureDesktopWithTrace()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "second" {
		t.Errorf("expected second method, got %q", res.Method)
	}
	if len(trace.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(trace.Attempts))
	}
	// First attempt should be marked as failed with EMPTY_FRAME.
	if trace.Attempts[0].Success {
		t.Error("first attempt (blank image) should not be marked as success")
	}
	if trace.Attempts[0].FailureCode != "EMPTY_FRAME" {
		t.Errorf("first attempt failure code = %q, want EMPTY_FRAME", trace.Attempts[0].FailureCode)
	}
	if !trace.Attempts[1].Success {
		t.Error("second attempt should be success")
	}
	if trace.SelectedMethod != "second" {
		t.Errorf("selected method = %q, want 'second'", trace.SelectedMethod)
	}
}

func TestEngine_BlankImage_WindowTrace(t *testing.T) {
	white := newMockBlank("first", color.RGBA{255, 255, 255, 255})
	ok := newMockOK("second")

	e := &Engine{requested: MethodAuto, capturers: []Capturer{white, ok}}

	res, trace, err := e.CaptureWindowWithTrace(0x1234)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "second" {
		t.Errorf("expected second method, got %q", res.Method)
	}
	if trace.Attempts[0].FailureCode != "EMPTY_FRAME" {
		t.Errorf("first attempt failure code = %q, want EMPTY_FRAME", trace.Attempts[0].FailureCode)
	}
}

func TestEngine_NonRGBAImage_SkipsBlankCheck(t *testing.T) {
	// If the image is not *image.RGBA (e.g., image.Gray), the blank check
	// should be skipped gracefully and the result accepted.
	gray := image.NewGray(image.Rect(0, 0, 100, 100))
	for i := range gray.Pix {
		gray.Pix[i] = 128
	}
	mock := &mockCapturer{
		name: "gray",
		result: &CaptureResult{
			Image:  gray,
			Width:  100,
			Height: 100,
			Method: "gray",
		},
	}

	e := &Engine{requested: MethodAuto, capturers: []Capturer{mock}}

	res, err := e.CaptureDesktop()
	if err != nil {
		t.Fatalf("non-RGBA image should be accepted: %v", err)
	}
	if res.Method != "gray" {
		t.Errorf("expected method 'gray', got %q", res.Method)
	}
}

func TestEngine_SingleCapturer_BlankReturnsError(t *testing.T) {
	// Even with a single capturer (non-auto mode), blank should be rejected.
	white := newMockBlank("only", color.RGBA{255, 255, 255, 255})

	e := &Engine{requested: "only", capturers: []Capturer{white}}

	_, err := e.CaptureDesktop()
	if err == nil {
		t.Fatal("single capturer returning blank should result in error")
	}
}
