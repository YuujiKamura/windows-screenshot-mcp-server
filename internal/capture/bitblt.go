package capture

import (
	"fmt"
	"image"
	"image/color"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Win32 DLL handles and procedures shared by BitBlt and PrintWindow capturers.
var (
	user32 = windows.NewLazyDLL("user32.dll")
	gdi32  = windows.NewLazyDLL("gdi32.dll")
	shcore = windows.NewLazyDLL("shcore.dll")
	dwmapi = windows.NewLazyDLL("dwmapi.dll")

	procGetWindowRect          = user32.NewProc("GetWindowRect")
	procGetWindowDC            = user32.NewProc("GetWindowDC")
	procGetDC                  = user32.NewProc("GetDC")
	procReleaseDC              = user32.NewProc("ReleaseDC")
	procGetDesktopWindow       = user32.NewProc("GetDesktopWindow")
	procSetProcessDPIAware     = user32.NewProc("SetProcessDPIAware")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procCreateDIBSection       = gdi32.NewProc("CreateDIBSection")
	procSetProcessDpiAwareness = shcore.NewProc("SetProcessDpiAwareness")
	procDwmFlush               = dwmapi.NewProc("DwmFlush")
)

// Win32 constants.
const (
	srccopy      = 0x00CC0020
	dibRGBColors = 0
	biRGB        = 0
	dpiAwareVal  = 1
)

// RECT matches the Win32 RECT layout.
type rect struct {
	Left, Top, Right, Bottom int32
}

// BITMAPINFOHEADER matches the Win32 structure layout.
type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// bitmapInfo is BITMAPINFO (header + one dummy colour entry).
type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

func init() {
	// Many GDI calls must run on the OS thread that created the DC.
	runtime.LockOSThread()
	enableDPIAwareness()
}

// enableDPIAwareness tells Windows not to lie about pixel coordinates.
func enableDPIAwareness() {
	// Prefer SetProcessDpiAwareness (Win 8.1+).
	if procSetProcessDpiAwareness.Find() == nil {
		ret, _, _ := procSetProcessDpiAwareness.Call(uintptr(dpiAwareVal))
		if ret == 0 { // S_OK
			return
		}
	}
	// Fallback: SetProcessDPIAware (Vista+).
	if procSetProcessDPIAware.Find() == nil {
		procSetProcessDPIAware.Call()
	}
}

// dwmFlush forces DWM to compose the latest frame before capture.
// This prevents blank/stale frames from D3D11/WinUI3 windows that
// may not have been composited by DWM when the capture runs.
func dwmFlush() {
	if procDwmFlush.Find() == nil {
		procDwmFlush.Call()
	}
}

// windowRect returns the bounding rectangle for hwnd.
func windowRect(hwnd uintptr) (int, int, int, int, error) {
	var r rect
	ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ret == 0 {
		return 0, 0, 0, 0, fmt.Errorf("GetWindowRect failed")
	}
	return int(r.Left), int(r.Top), int(r.Right - r.Left), int(r.Bottom - r.Top), nil
}

// captureDC copies width*height pixels from srcDC into an *image.RGBA.
// The caller must ensure srcDC is valid for the lifetime of this call.
func captureDC(srcDC uintptr, width, height int) (*image.RGBA, error) {
	memDC, _, _ := procCreateCompatibleDC.Call(srcDC)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)

	var bmi bitmapInfo
	bmi.Header.Size = uint32(unsafe.Sizeof(bmi.Header))
	bmi.Header.Width = int32(width)
	bmi.Header.Height = -int32(height) // top-down DIB
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

	ret, _, _ := procBitBlt.Call(
		memDC, 0, 0, uintptr(width), uintptr(height),
		srcDC, 0, 0, srccopy,
	)
	if ret == 0 {
		return nil, fmt.Errorf("BitBlt failed")
	}

	return bgraToRGBA(pBits, width, height), nil
}

// bgraToRGBA converts a BGRA pixel buffer into a standard Go RGBA image.
func bgraToRGBA(pBits uintptr, width, height int) *image.RGBA {
	n := width * height * 4
	src := unsafe.Slice((*byte)(unsafe.Pointer(pBits)), n)

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	copy(img.Pix, src)

	// Swap B and R channels in place.
	for i := 0; i < n; i += 4 {
		img.Pix[i+0], img.Pix[i+2] = img.Pix[i+2], img.Pix[i+0]
	}
	return img
}

// ---------- BitBlt capturer ----------

// BitBltCapturer uses GDI BitBlt. Works on all Windows versions but only
// reliably captures the foreground (non-occluded) portion of a window.
type BitBltCapturer struct{}

// NewBitBlt returns a ready-to-use BitBltCapturer.
func NewBitBlt() *BitBltCapturer { return &BitBltCapturer{} }

// Name returns the method identifier.
func (b *BitBltCapturer) Name() Method { return MethodBitBlt }

// CaptureWindow captures the given window via BitBlt.
func (b *BitBltCapturer) CaptureWindow(hwnd uintptr) (*CaptureResult, error) {
	start := time.Now()

	_, _, width, height, err := windowRect(hwnd)
	if err != nil {
		return nil, err
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid window dimensions %dx%d", width, height)
	}

	dwmFlush()

	hdc, _, _ := procGetWindowDC.Call(hwnd)
	if hdc == 0 {
		return nil, fmt.Errorf("GetWindowDC failed")
	}
	defer procReleaseDC.Call(hwnd, hdc)

	img, err := captureDC(hdc, width, height)
	if err != nil {
		return nil, err
	}

	// Sanity check: all-black means the capture was empty.
	if isBlank(img) {
		return nil, fmt.Errorf("BitBlt produced a blank image")
	}

	return &CaptureResult{
		Image:    img,
		Width:    width,
		Height:   height,
		Method:   MethodBitBlt,
		Duration: time.Since(start),
	}, nil
}

// CaptureDesktop captures the entire virtual screen.
func (b *BitBltCapturer) CaptureDesktop() (*CaptureResult, error) {
	desktop, _, _ := procGetDesktopWindow.Call()
	if desktop == 0 {
		return nil, fmt.Errorf("GetDesktopWindow failed")
	}
	return b.CaptureWindow(desktop)
}

// isBlank does a quick check: sample a few pixels to see if the image is
// completely transparent/black or completely white, which indicates a failed capture.
func isBlank(img *image.RGBA) bool {
	bounds := img.Bounds()
	samples := [][2]int{
		{bounds.Dx() / 4, bounds.Dy() / 4},
		{bounds.Dx() / 2, bounds.Dy() / 2},
		{bounds.Dx() * 3 / 4, bounds.Dy() * 3 / 4},
	}

	black := color.RGBA{0, 0, 0, 0}
	white := color.RGBA{255, 255, 255, 255}

	allBlack := true
	allWhite := true
	for _, s := range samples {
		c := img.RGBAAt(s[0], s[1])
		if c != black {
			allBlack = false
		}
		if c != white {
			allWhite = false
		}
	}
	return allBlack || allWhite
}
