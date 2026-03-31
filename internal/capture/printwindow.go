package capture

import (
	"fmt"
	"time"
	"unsafe"
)

const (
	pwRenderFullContent = 2 // PW_RENDERFULLCONTENT (Windows 8.1+)
)

var (
	procPrintWindow = user32.NewProc("PrintWindow")
)

// PrintWindowCapturer uses the PrintWindow API with PW_RENDERFULLCONTENT.
// It can capture windows that are behind other windows (background capture).
// Available on Windows 8.1 and later.
type PrintWindowCapturer struct{}

// NewPrintWindow returns a ready-to-use PrintWindowCapturer.
func NewPrintWindow() *PrintWindowCapturer { return &PrintWindowCapturer{} }

// Name returns the method identifier.
func (p *PrintWindowCapturer) Name() Method { return MethodPrint }

// CaptureWindow renders the target window into an off-screen bitmap via
// PrintWindow and returns the result as a standard Go image.
func (p *PrintWindowCapturer) CaptureWindow(hwnd uintptr) (*CaptureResult, error) {
	start := time.Now()

	_, _, width, height, err := windowRect(hwnd)
	if err != nil {
		return nil, err
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid window dimensions %dx%d", width, height)
	}

	// We need a screen DC as the basis for a compatible memory DC.
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("GetDC(NULL) failed")
	}
	defer procReleaseDC.Call(0, screenDC)

	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)

	var bmi bitmapInfo
	bmi.Header.Size = uint32(unsafe.Sizeof(bmi.Header))
	bmi.Header.Width = int32(width)
	bmi.Header.Height = -int32(height) // top-down
	bmi.Header.Planes = 1
	bmi.Header.BitCount = 32
	bmi.Header.Compression = biRGB

	var pBits uintptr
	hBitmap, _, _ := procCreateDIBSection.Call(
		memDC,
		uintptr(unsafe.Pointer(&bmi)),
		dibRGBColors,
		uintptr(unsafe.Pointer(&pBits)),
		0, 0,
	)
	if hBitmap == 0 {
		return nil, fmt.Errorf("CreateDIBSection failed")
	}
	defer procDeleteObject.Call(hBitmap)

	oldBmp, _, _ := procSelectObject.Call(memDC, hBitmap)
	defer procSelectObject.Call(memDC, oldBmp)

	// PW_RENDERFULLCONTENT asks the window to paint its full content,
	// including Direct3D/DirectComposition surfaces.
	ret, _, _ := procPrintWindow.Call(hwnd, memDC, uintptr(pwRenderFullContent))
	if ret == 0 {
		return nil, fmt.Errorf("PrintWindow failed")
	}

	img := bgraToRGBA(pBits, width, height)

	if isBlank(img) {
		return nil, fmt.Errorf("PrintWindow produced a blank image")
	}

	return &CaptureResult{
		Image:    img,
		Width:    width,
		Height:   height,
		Method:   MethodPrint,
		Duration: time.Since(start),
	}, nil
}

// CaptureDesktop returns an error because PrintWindow does not work on
// the desktop window.
func (p *PrintWindowCapturer) CaptureDesktop() (*CaptureResult, error) {
	return nil, fmt.Errorf("PrintWindow does not support desktop capture")
}
