//go:build windows

package capture

import (
	"errors"
	"strings"
	"testing"
)

// =============================================================================
// Capture method selection / engine fallback exhaustive tests
// =============================================================================

func TestEngine_AutoMode_TriesMethodsInOrder(t *testing.T) {
	// Track call order via a slice.
	var callOrder []Method

	m1 := &mockCapturer{
		name: "first",
		err:  errors.New("first fails"),
	}
	m2 := &mockCapturer{
		name: "second",
		err:  errors.New("second fails"),
	}
	m3 := newMockOK("third")

	// Wrap to track order.
	e := &Engine{
		requested: MethodAuto,
		capturers: []Capturer{m1, m2, m3},
	}

	_, trace, err := e.CaptureDesktopWithTrace()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the order from trace.
	for _, a := range trace.Attempts {
		callOrder = append(callOrder, a.Method)
	}
	if len(callOrder) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(callOrder))
	}
	if callOrder[0] != "first" || callOrder[1] != "second" || callOrder[2] != "third" {
		t.Errorf("unexpected call order: %v", callOrder)
	}
}

func TestEngine_AutoMode_StopsOnFirstSuccess(t *testing.T) {
	ok1 := newMockOK("first")
	ok2 := newMockOK("second")

	e := &Engine{
		requested: MethodAuto,
		capturers: []Capturer{ok1, ok2},
	}

	res, trace, err := e.CaptureDesktopWithTrace()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "first" {
		t.Errorf("expected first method to be selected, got %q", res.Method)
	}
	if len(trace.Attempts) != 1 {
		t.Errorf("expected only 1 attempt (stop on first success), got %d", len(trace.Attempts))
	}
	if ok2.called != 0 {
		t.Error("second capturer should not have been called")
	}
}

func TestEngine_BlankFallthrough(t *testing.T) {
	// Simulate: first method returns blank error, should fall through.
	blankFail := &mockCapturer{
		name: "bitblt",
		err:  errors.New("BitBlt produced a blank image"),
	}
	okNext := newMockOK("print")

	e := &Engine{
		requested: MethodAuto,
		capturers: []Capturer{blankFail, okNext},
	}

	res, trace, err := e.CaptureDesktopWithTrace()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "print" {
		t.Errorf("expected fallthrough to print, got %q", res.Method)
	}
	if len(trace.Attempts) != 2 {
		t.Errorf("expected 2 attempts, got %d", len(trace.Attempts))
	}
	if trace.Attempts[0].FailureCode != "EMPTY_FRAME" {
		t.Errorf("first attempt failure code = %q, want EMPTY_FRAME", trace.Attempts[0].FailureCode)
	}
}

func TestEngine_AllBlank_ReturnsError(t *testing.T) {
	// All methods produce blank images.
	blank1 := &mockCapturer{
		name: "bitblt",
		err:  errors.New("BitBlt produced a blank image"),
	}
	blank2 := &mockCapturer{
		name: "print",
		err:  errors.New("PrintWindow produced a blank image"),
	}
	blank3 := &mockCapturer{
		name: "capture",
		err:  errors.New("DXGI capture produced a blank image"),
	}

	e := &Engine{
		requested: MethodAuto,
		capturers: []Capturer{blank1, blank2, blank3},
	}

	_, _, err := e.CaptureDesktopWithTrace()
	if err == nil {
		t.Fatal("expected error when all methods return blank, got nil")
	}
	if !strings.Contains(err.Error(), "all capture methods failed") {
		t.Errorf("error = %q, expected 'all capture methods failed'", err)
	}
}

func TestEngine_AllBlank_CaptureWindow_ReturnsError(t *testing.T) {
	blank1 := &mockCapturer{
		name: "bitblt",
		err:  errors.New("BitBlt produced a blank image"),
	}
	blank2 := &mockCapturer{
		name: "print",
		err:  errors.New("PrintWindow produced a blank image"),
	}

	e := &Engine{
		requested: MethodAuto,
		capturers: []Capturer{blank1, blank2},
	}

	_, err := e.CaptureWindow(0x1234)
	if err == nil {
		t.Fatal("expected error when all methods return blank for CaptureWindow")
	}
}

func TestEngine_NoCapturers(t *testing.T) {
	e := &Engine{requested: MethodAuto, capturers: []Capturer{}}

	_, _, err := e.CaptureDesktopWithTrace()
	if err == nil {
		t.Fatal("expected error with no capturers")
	}
}

func TestEngine_TraceEmptyCapturers(t *testing.T) {
	e := &Engine{requested: MethodAuto, capturers: []Capturer{}}
	_, trace, _ := e.CaptureDesktopWithTrace()
	if trace == nil {
		t.Fatal("trace should not be nil even with no capturers")
	}
	// With zero capturers, finalizeTrace sees no attempts and sets NO_ATTEMPT,
	// but the loop still falls through to the "all failed" error path.
	// The trace stop reason reflects what finalizeTrace sets.
	if trace.StopReason != "NO_ATTEMPT" {
		t.Errorf("stop reason = %q, want NO_ATTEMPT", trace.StopReason)
	}
}

func TestEngine_CaptureWindowWithTrace_Success(t *testing.T) {
	ok := newMockOK("test")
	e := &Engine{requested: "test", capturers: []Capturer{ok}}

	res, trace, err := e.CaptureWindowWithTrace(0x1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("result should not be nil")
	}
	if trace.SelectedMethod != "test" {
		t.Errorf("selected method = %q, want test", trace.SelectedMethod)
	}
	if trace.StopReason != "FIRST_SUCCESS" {
		t.Errorf("stop reason = %q, want FIRST_SUCCESS", trace.StopReason)
	}
}

func TestEngine_CaptureWindowWithTrace_AllFail(t *testing.T) {
	fail := newMockFail("test")
	e := &Engine{requested: "test", capturers: []Capturer{fail}}

	_, trace, err := e.CaptureWindowWithTrace(0x1)
	if err == nil {
		t.Fatal("expected error")
	}
	if trace.StopReason != "ALL_FAILED" {
		t.Errorf("stop reason = %q, want ALL_FAILED", trace.StopReason)
	}
	if trace.SelectedMethod != "" {
		t.Errorf("selected method should be empty, got %q", trace.SelectedMethod)
	}
}

// --- classifyFailure exhaustive tests ----------------------------------------

func TestClassifyFailure_Nil(t *testing.T) {
	if got := classifyFailure(nil); got != "" {
		t.Errorf("classifyFailure(nil) = %q, want empty", got)
	}
}

func TestClassifyFailure_AllCategories(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"not a valid window handle", "NO_WINDOW"},
		{"GetWindowRect failed for HWND 0x1234", "NO_WINDOW"},
		{"no window matching title 'Foo'", "NO_WINDOW"},
		{"no visible window for PID 1234", "NO_WINDOW"},
		{"invalid window dimensions 0x0", "INVALID_BOUNDS"},
		{"invalid window dimensions 136x39", "INVALID_BOUNDS"},
		{"blank image detected", "EMPTY_FRAME"},
		{"BitBlt produced a blank image", "EMPTY_FRAME"},
		{"PrintWindow produced a blank image", "EMPTY_FRAME"},
		{"DXGI capture produced a blank image", "EMPTY_FRAME"},
		{"PrintWindow does not support desktop capture", "API_UNSUPPORTED"},
		{"feature not supported on this platform", "API_UNSUPPORTED"},
		{"AcquireNextFrame timed out after 10 attempts", "TIMEOUT"},
		{"operation timed out", "TIMEOUT"},
		{"request timeout exceeded", "TIMEOUT"},
		{"Access denied to window", "ACCESS_DENIED"},
		{"window rect outside desktop bounds", "OUT_OF_BOUNDS"},
		{"some completely unrecognized error", "CAPTURE_FAILED"},
	}
	for _, tc := range cases {
		t.Run(tc.msg, func(t *testing.T) {
			got := classifyFailure(errors.New(tc.msg))
			if got != tc.want {
				t.Errorf("classifyFailure(%q) = %q, want %q", tc.msg, got, tc.want)
			}
		})
	}
}

// --- buildFallbackSummary tests ----------------------------------------------

func TestBuildFallbackSummary_Empty(t *testing.T) {
	got := buildFallbackSummary(nil, "")
	if got != "no capture attempts executed" {
		t.Errorf("got %q, want 'no capture attempts executed'", got)
	}
}

func TestBuildFallbackSummary_AllFailed_NoSelection(t *testing.T) {
	attempts := []AttemptTrace{
		{Method: "a", Success: false, FailureCode: "TIMEOUT"},
		{Method: "b", Success: false, FailureCode: "EMPTY_FRAME"},
	}
	got := buildFallbackSummary(attempts, "")
	if !strings.HasSuffix(got, "no method selected") {
		t.Errorf("summary = %q, expected suffix 'no method selected'", got)
	}
}

func TestBuildFallbackSummary_EmptyFailureCode(t *testing.T) {
	attempts := []AttemptTrace{
		{Method: "a", Success: false, FailureCode: ""},
	}
	got := buildFallbackSummary(attempts, "")
	if !strings.Contains(got, "CAPTURE_FAILED") {
		t.Errorf("summary = %q, expected CAPTURE_FAILED for empty failure code", got)
	}
}

// --- finalizeTrace tests -----------------------------------------------------

func TestFinalizeTrace_Nil(t *testing.T) {
	// Should not panic.
	finalizeTrace(nil)
}

func TestFinalizeTrace_NoAttempts(t *testing.T) {
	trace := &Trace{}
	finalizeTrace(trace)
	if trace.StopReason != "NO_ATTEMPT" {
		t.Errorf("stop reason = %q, want NO_ATTEMPT", trace.StopReason)
	}
}

func TestFinalizeTrace_WithSelected(t *testing.T) {
	trace := &Trace{
		SelectedMethod: "bitblt",
		Attempts: []AttemptTrace{
			{Method: "bitblt", Success: true},
		},
	}
	finalizeTrace(trace)
	if trace.StopReason != "FIRST_SUCCESS" {
		t.Errorf("stop reason = %q, want FIRST_SUCCESS", trace.StopReason)
	}
}

func TestFinalizeTrace_NoSelected(t *testing.T) {
	trace := &Trace{
		Attempts: []AttemptTrace{
			{Method: "bitblt", Success: false, FailureCode: "EMPTY_FRAME"},
		},
	}
	finalizeTrace(trace)
	if trace.StopReason != "ALL_FAILED" {
		t.Errorf("stop reason = %q, want ALL_FAILED", trace.StopReason)
	}
}

// --- NewEngine order tests ---------------------------------------------------

func TestNewEngine_AutoOrder(t *testing.T) {
	e := NewEngine(MethodAuto)
	if len(e.capturers) != 3 {
		t.Fatalf("expected 3 capturers, got %d", len(e.capturers))
	}
	// The code creates: BitBlt, PrintWindow, GraphicsCapture.
	expectedOrder := []Method{MethodBitBlt, MethodPrint, MethodCapture}
	for i, want := range expectedOrder {
		got := e.capturers[i].Name()
		if got != want {
			t.Errorf("capturer[%d] = %q, want %q", i, got, want)
		}
	}
}

func TestNewEngine_EachSpecificMethod(t *testing.T) {
	methods := []Method{MethodCapture, MethodPrint, MethodBitBlt}
	for _, m := range methods {
		t.Run(string(m), func(t *testing.T) {
			e := NewEngine(m)
			if len(e.capturers) != 1 {
				t.Fatalf("expected 1 capturer, got %d", len(e.capturers))
			}
			if e.capturers[0].Name() != m {
				t.Errorf("capturer name = %q, want %q", e.capturers[0].Name(), m)
			}
		})
	}
}

func TestNewEngine_UnknownMethod_DefaultsToAuto(t *testing.T) {
	e := NewEngine("unknown_method")
	if len(e.capturers) != 3 {
		t.Errorf("unknown method should default to auto (3 capturers), got %d", len(e.capturers))
	}
}
