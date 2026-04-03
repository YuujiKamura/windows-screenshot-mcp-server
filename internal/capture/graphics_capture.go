package capture

import (
	"fmt"
	"image"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// COM GUIDs
var (
	iidIDXGIDevice  = windows.GUID{0x54ec77fa, 0x1377, 0x44e6, [8]byte{0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c}}
	iidIDXGIOutput  = windows.GUID{0xae02eedb, 0xc735, 0x4690, [8]byte{0x8d, 0x52, 0x5a, 0x8d, 0xc2, 0x02, 0x13, 0xaa}}
	iidIDXGIOutput1 = windows.GUID{0x00cddea8, 0x939b, 0x4b83, [8]byte{0xa3, 0x40, 0xa6, 0x85, 0x22, 0x66, 0x66, 0xcc}}
)

// D3D11 constants
const (
	d3dDriverTypeHardware   = 1
	d3dDriverTypeWarp       = 5
	d3d11CreateDeviceBGRA   = 0x20
	d3d11SDKVersion         = 7
	dxgiFormatB8G8R8A8UNorm = 87
	dxgiErrorWaitTimeout    = 0x887A0027
)

// D3D11 / DXGI structures
type dxgiOutputDesc struct {
	DeviceName          [32]uint16
	DesktopCoords       rect // RECT
	AttachedToDesktop   int32
	Rotation            uint32
	Monitor             uintptr
}

type d3d11Texture2DDesc struct {
	Width             uint32
	Height            uint32
	MipLevels         uint32
	ArraySize         uint32
	Format            uint32
	SampleDescCount   uint32
	SampleDescQuality uint32
	Usage             uint32
	BindFlags         uint32
	CPUAccessFlags    uint32
	MiscFlags         uint32
}

type dxgiOutduplFrameInfo struct {
	LastPresentTime           int64
	LastMouseUpdateTime       int64
	AccumulatedFrames         uint32
	RectsCoalesced            int32
	ProtectedContentMaskedOut int32
	PointerPosition           [24]byte
}

type d3d11MappedSubresource struct {
	PData      uintptr
	RowPitch   uint32
	DepthPitch uint32
}

// DLL handles
var (
	d3d11DLL = windows.NewLazyDLL("d3d11.dll")

	procD3D11CreateDevice = d3d11DLL.NewProc("D3D11CreateDevice")
)

// GraphicsCaptureCapturer implements desktop capture via the DXGI Desktop
// Duplication API (available on Windows 8+). It captures the entire desktop
// and, for per-window capture, crops to the target window's rect.
type GraphicsCaptureCapturer struct{}

// NewGraphicsCapture returns a ready-to-use capturer.
func NewGraphicsCapture() *GraphicsCaptureCapturer { return &GraphicsCaptureCapturer{} }

// Name returns the method identifier.
func (g *GraphicsCaptureCapturer) Name() Method { return MethodCapture }

// CaptureDesktop captures the primary monitor via DXGI Desktop Duplication.
func (g *GraphicsCaptureCapturer) CaptureDesktop() (*CaptureResult, error) {
	return g.captureDesktopDXGI(nil)
}

// CaptureWindow captures the desktop and crops to the target window's rect.
func (g *GraphicsCaptureCapturer) CaptureWindow(hwnd uintptr) (*CaptureResult, error) {
	left, top, width, height, err := windowRect(hwnd)
	if err != nil {
		return nil, fmt.Errorf("get window rect: %w", err)
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid window dimensions %dx%d", width, height)
	}
	cropRect := &image.Rectangle{
		Min: image.Point{X: left, Y: top},
		Max: image.Point{X: left + width, Y: top + height},
	}
	return g.captureDesktopDXGI(cropRect)
}

// captureDesktopDXGI does the actual DXGI Desktop Duplication capture.
// If crop is non-nil, the result is cropped to that rectangle.
func (g *GraphicsCaptureCapturer) captureDesktopDXGI(crop *image.Rectangle) (*CaptureResult, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	start := time.Now()

	dwmFlush()

	// 1. Create D3D11 device
	device, deviceCtx, err := createD3D11Device()
	if err != nil {
		return nil, fmt.Errorf("create D3D11 device: %w", err)
	}
	defer comRelease(device)
	defer comRelease(deviceCtx)

	// 2. Get IDXGIDevice
	dxgiDevice, err := comQueryInterface(device, &iidIDXGIDevice)
	if err != nil {
		return nil, fmt.Errorf("QueryInterface IDXGIDevice: %w", err)
	}
	defer comRelease(dxgiDevice)

	// 3. Get IDXGIAdapter via IDXGIDevice::GetAdapter (slot 7)
	adapter, err := dxgiDeviceGetAdapter(dxgiDevice)
	if err != nil {
		return nil, fmt.Errorf("IDXGIDevice::GetAdapter: %w", err)
	}
	defer comRelease(adapter)

	// 4. Enumerate outputs (get first / primary output)
	output, err := dxgiAdapterEnumOutputs(adapter, 0)
	if err != nil {
		return nil, fmt.Errorf("IDXGIAdapter::EnumOutputs: %w", err)
	}
	defer comRelease(output)

	// 5. Get output description for dimensions
	desc, err := dxgiOutputGetDesc(output)
	if err != nil {
		return nil, fmt.Errorf("IDXGIOutput::GetDesc: %w", err)
	}

	desktopWidth := int(desc.DesktopCoords.Right - desc.DesktopCoords.Left)
	desktopHeight := int(desc.DesktopCoords.Bottom - desc.DesktopCoords.Top)

	// 6. QI for IDXGIOutput1
	output1, err := comQueryInterface(output, &iidIDXGIOutput1)
	if err != nil {
		return nil, fmt.Errorf("QueryInterface IDXGIOutput1: %w", err)
	}
	defer comRelease(output1)

	// 7. DuplicateOutput (slot 22 on IDXGIOutput1)
	duplication, err := dxgiOutput1DuplicateOutput(output1, device)
	if err != nil {
		return nil, fmt.Errorf("IDXGIOutput1::DuplicateOutput: %w", err)
	}
	defer comRelease(duplication)

	// 8. Acquire frame with retries (the first frame may not be ready immediately)
	var desktopResource uintptr
	var frameAcquired bool
	for attempt := 0; attempt < 10; attempt++ {
		var frameInfo dxgiOutduplFrameInfo
		dr, hr := dxgiOutputDuplAcquireNextFrame(duplication, 500, &frameInfo)
		if hr == nil {
			desktopResource = dr
			frameAcquired = true
			break
		}
		// If timeout, retry
		if hrVal := hrToUint32(hr); hrVal == uint32(dxgiErrorWaitTimeout) {
			continue
		}
		return nil, fmt.Errorf("AcquireNextFrame: %w", hr)
	}
	if !frameAcquired {
		return nil, fmt.Errorf("AcquireNextFrame timed out after 10 attempts")
	}
	defer comRelease(desktopResource)
	defer dxgiOutputDuplReleaseFrame(duplication)

	// 9. QI for ID3D11Texture2D from the desktop resource
	iidTexture2D := windows.GUID{0x6f15aaf2, 0xd208, 0x4e89, [8]byte{0x9a, 0xb4, 0x48, 0x95, 0x35, 0xd3, 0x4f, 0x9c}}
	texture, err := comQueryInterface(desktopResource, &iidTexture2D)
	if err != nil {
		return nil, fmt.Errorf("QueryInterface ID3D11Texture2D: %w", err)
	}
	defer comRelease(texture)

	// 10. Create a staging texture (CPU-readable) and copy
	staging, err := createStagingTexture(device, uint32(desktopWidth), uint32(desktopHeight))
	if err != nil {
		return nil, fmt.Errorf("create staging texture: %w", err)
	}
	defer comRelease(staging)

	// 11. CopyResource (slot 47 on ID3D11DeviceContext)
	copyResource(deviceCtx, staging, texture)

	// 12. Map the staging texture (slot 14 on ID3D11DeviceContext)
	mapped, err := mapTexture(deviceCtx, staging)
	if err != nil {
		return nil, fmt.Errorf("map staging texture: %w", err)
	}
	defer unmapTexture(deviceCtx, staging)

	// 13. Copy pixels to image.RGBA
	img := image.NewRGBA(image.Rect(0, 0, desktopWidth, desktopHeight))
	pitch := int(mapped.RowPitch)
	srcSlice := unsafe.Slice((*byte)(unsafe.Pointer(mapped.PData)), pitch*desktopHeight)
	for y := 0; y < desktopHeight; y++ {
		srcRow := srcSlice[y*pitch : y*pitch+desktopWidth*4]
		dstOff := y * img.Stride
		copy(img.Pix[dstOff:dstOff+desktopWidth*4], srcRow)
	}

	// Swap B and R (BGRA -> RGBA)
	n := desktopWidth * desktopHeight * 4
	for i := 0; i < n; i += 4 {
		img.Pix[i+0], img.Pix[i+2] = img.Pix[i+2], img.Pix[i+0]
	}

	// 14. Crop if needed
	resultImg := image.Image(img)
	resultWidth := desktopWidth
	resultHeight := desktopHeight
	if crop != nil {
		// Adjust for desktop coordinates that may not start at 0,0
		adjustedCrop := image.Rectangle{
			Min: image.Point{
				X: crop.Min.X - int(desc.DesktopCoords.Left),
				Y: crop.Min.Y - int(desc.DesktopCoords.Top),
			},
			Max: image.Point{
				X: crop.Max.X - int(desc.DesktopCoords.Left),
				Y: crop.Max.Y - int(desc.DesktopCoords.Top),
			},
		}
		cr := adjustedCrop.Intersect(img.Bounds())
		if cr.Empty() {
			return nil, fmt.Errorf("window rect outside desktop bounds")
		}
		cropped := image.NewRGBA(image.Rect(0, 0, cr.Dx(), cr.Dy()))
		for y := 0; y < cr.Dy(); y++ {
			srcOff := (cr.Min.Y+y)*img.Stride + cr.Min.X*4
			dstOff := y * cropped.Stride
			copy(cropped.Pix[dstOff:dstOff+cr.Dx()*4], img.Pix[srcOff:srcOff+cr.Dx()*4])
		}
		resultImg = cropped
		resultWidth = cr.Dx()
		resultHeight = cr.Dy()
	}

	// Sanity check: all-black or all-white means the capture was empty.
	if rgbaImg, ok := resultImg.(*image.RGBA); ok && isBlank(rgbaImg) {
		return nil, fmt.Errorf("DXGI capture produced a blank image")
	}

	return &CaptureResult{
		Image:    resultImg,
		Width:    resultWidth,
		Height:   resultHeight,
		Method:   MethodCapture,
		Duration: time.Since(start),
	}, nil
}

// ============================================================
// COM / D3D11 / DXGI helper functions
// ============================================================

func hrToUint32(err error) uint32 {
	if e, ok := err.(windows.Errno); ok {
		return uint32(e)
	}
	return 0
}

// comVtbl returns the vtable pointer from a COM object pointer.
func comVtbl(obj uintptr) uintptr {
	return *(*uintptr)(unsafe.Pointer(obj))
}

// comMethod returns the n-th method from a vtable.
func comMethod(vtbl uintptr, index int) uintptr {
	return *(*uintptr)(unsafe.Pointer(vtbl + uintptr(index)*unsafe.Sizeof(uintptr(0))))
}

// comRelease calls IUnknown::Release (slot 2).
func comRelease(obj uintptr) {
	if obj == 0 {
		return
	}
	vtbl := comVtbl(obj)
	method := comMethod(vtbl, 2)
	syscall.SyscallN(method, obj)
}

// comQueryInterface calls IUnknown::QueryInterface (slot 0).
func comQueryInterface(obj uintptr, iid *windows.GUID) (uintptr, error) {
	vtbl := comVtbl(obj)
	method := comMethod(vtbl, 0)
	var result uintptr
	hr, _, _ := syscall.SyscallN(method, obj, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&result)))
	if hr != 0 {
		return 0, fmt.Errorf("QueryInterface failed: HRESULT 0x%08X", hr)
	}
	return result, nil
}

// createD3D11Device creates a D3D11 device (tries hardware, falls back to WARP).
func createD3D11Device() (device uintptr, deviceCtx uintptr, err error) {
	if e := procD3D11CreateDevice.Find(); e != nil {
		return 0, 0, fmt.Errorf("d3d11.dll not available: %w", e)
	}

	for _, driverType := range []uintptr{d3dDriverTypeHardware, d3dDriverTypeWarp} {
		hr, _, _ := procD3D11CreateDevice.Call(
			0,                     // pAdapter
			driverType,            // DriverType
			0,                     // Software
			d3d11CreateDeviceBGRA, // Flags
			0,                     // pFeatureLevels
			0,                     // FeatureLevels count
			d3d11SDKVersion,       // SDKVersion
			uintptr(unsafe.Pointer(&device)),
			0, // pFeatureLevel out
			uintptr(unsafe.Pointer(&deviceCtx)),
		)
		if hr == 0 {
			return device, deviceCtx, nil
		}
	}
	return 0, 0, fmt.Errorf("D3D11CreateDevice failed with all driver types")
}

// IDXGIDevice vtable:
// IUnknown: 0=QI, 1=AddRef, 2=Release
// IDXGIObject: 3=SetPrivateData, 4=SetPrivateDataInterface, 5=GetPrivateData, 6=GetParent
// IDXGIDevice: 7=GetAdapter, 8=CreateSurface, 9=QueryResourceResidency, 10=SetGPUThreadPriority, 11=GetGPUThreadPriority
func dxgiDeviceGetAdapter(dxgiDevice uintptr) (uintptr, error) {
	vtbl := comVtbl(dxgiDevice)
	method := comMethod(vtbl, 7)
	var adapter uintptr
	hr, _, _ := syscall.SyscallN(method, dxgiDevice, uintptr(unsafe.Pointer(&adapter)))
	if hr != 0 {
		return 0, fmt.Errorf("GetAdapter HRESULT 0x%08X", hr)
	}
	return adapter, nil
}

// IDXGIAdapter vtable (inherits IDXGIObject):
// 7=EnumOutputs, 8=GetDesc, 9=CheckInterfaceSupport
func dxgiAdapterEnumOutputs(adapter uintptr, index uint32) (uintptr, error) {
	vtbl := comVtbl(adapter)
	method := comMethod(vtbl, 7)
	var output uintptr
	hr, _, _ := syscall.SyscallN(method, adapter, uintptr(index), uintptr(unsafe.Pointer(&output)))
	if hr != 0 {
		return 0, fmt.Errorf("EnumOutputs HRESULT 0x%08X", hr)
	}
	return output, nil
}

// IDXGIOutput vtable (inherits IDXGIObject):
// 7=GetDesc, 8=GetDisplayModeList, 9=FindClosestMatchingMode,
// 10=WaitForVBlank, 11=TakeOwnership, 12=ReleaseOwnership,
// 13=GetGammaControlCapabilities, 14=SetGammaControl, 15=GetGammaControl,
// 16=SetDisplaySurface, 17=GetDisplaySurfaceData, 18=GetFrameStatistics
func dxgiOutputGetDesc(output uintptr) (*dxgiOutputDesc, error) {
	vtbl := comVtbl(output)
	method := comMethod(vtbl, 7)
	var desc dxgiOutputDesc
	hr, _, _ := syscall.SyscallN(method, output, uintptr(unsafe.Pointer(&desc)))
	if hr != 0 {
		return nil, fmt.Errorf("GetDesc HRESULT 0x%08X", hr)
	}
	return &desc, nil
}

// IDXGIOutput1 vtable (inherits IDXGIOutput):
// 19=GetDisplayModeList1, 20=FindClosestMatchingMode1,
// 21=GetDisplaySurfaceData1, 22=DuplicateOutput
func dxgiOutput1DuplicateOutput(output1 uintptr, device uintptr) (uintptr, error) {
	vtbl := comVtbl(output1)
	method := comMethod(vtbl, 22)
	var duplication uintptr
	hr, _, _ := syscall.SyscallN(method, output1, device, uintptr(unsafe.Pointer(&duplication)))
	if hr != 0 {
		return 0, fmt.Errorf("DuplicateOutput HRESULT 0x%08X", hr)
	}
	return duplication, nil
}

// IDXGIOutputDuplication vtable (inherits IDXGIObject):
// 7=GetDesc, 8=AcquireNextFrame, 9=GetFrameDirtyRects,
// 10=GetFrameMoveRects, 11=GetFramePointerShape, 12=MapDesktopSurface,
// 13=UnMapDesktopSurface, 14=ReleaseFrame
func dxgiOutputDuplAcquireNextFrame(duplication uintptr, timeoutMs uint32, frameInfo *dxgiOutduplFrameInfo) (uintptr, error) {
	vtbl := comVtbl(duplication)
	method := comMethod(vtbl, 8)
	var resource uintptr
	hr, _, _ := syscall.SyscallN(method, duplication, uintptr(timeoutMs), uintptr(unsafe.Pointer(frameInfo)), uintptr(unsafe.Pointer(&resource)))
	if hr != 0 {
		return 0, windows.Errno(hr)
	}
	return resource, nil
}

func dxgiOutputDuplReleaseFrame(duplication uintptr) {
	vtbl := comVtbl(duplication)
	method := comMethod(vtbl, 14)
	syscall.SyscallN(method, duplication)
}

// ID3D11Device vtable (inherits IUnknown):
// 3=CreateBuffer, 4=CreateTexture1D, 5=CreateTexture2D, ...
func createStagingTexture(device uintptr, width, height uint32) (uintptr, error) {
	desc := d3d11Texture2DDesc{
		Width:             width,
		Height:            height,
		MipLevels:         1,
		ArraySize:         1,
		Format:            dxgiFormatB8G8R8A8UNorm,
		SampleDescCount:   1,
		SampleDescQuality: 0,
		Usage:             3,       // D3D11_USAGE_STAGING
		BindFlags:         0,
		CPUAccessFlags:    0x20000, // D3D11_CPU_ACCESS_READ
		MiscFlags:         0,
	}
	vtbl := comVtbl(device)
	method := comMethod(vtbl, 5)
	var texture uintptr
	hr, _, _ := syscall.SyscallN(method, device, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&texture)))
	if hr != 0 {
		return 0, fmt.Errorf("CreateTexture2D HRESULT 0x%08X", hr)
	}
	return texture, nil
}

// ID3D11DeviceContext vtable (inherits ID3D11DeviceChild):
// ID3D11DeviceChild: 3=GetDevice, 4=GetPrivateData, 5=SetPrivateData, 6=SetPrivateDataInterface
// ID3D11DeviceContext: 7=VSSetConstantBuffers, 8=PSSetShaderResources, 9=PSSetShader,
//
//	10=PSSetSamplers, 11=VSSetShader, 12=DrawIndexed, 13=Draw,
//	14=Map, 15=Unmap, 16=PSSetConstantBuffers, 17=IASetInputLayout,
//	18=IASetVertexBuffers, 19=IASetIndexBuffer, 20=DrawIndexedInstanced,
//	21=DrawInstanced, 22=GSSetConstantBuffers, 23=GSSetShader,
//	24=IASetPrimitiveTopology, 25=VSSetShaderResources, 26=VSSetSamplers,
//	27=Begin, 28=End, 29=GetData, 30=SetPredication, 31=GSSetShaderResources,
//	32=GSSetSamplers, 33=OMSetRenderTargets, 34=OMSetRenderTargetsAndUnorderedAccessViews,
//	35=OMSetBlendState, 36=OMSetDepthStencilState, 37=SOSetTargets,
//	38=DrawAuto, 39=DrawIndexedInstancedIndirect, 40=DrawInstancedIndirect,
//	41=Dispatch, 42=DispatchIndirect, 43=RSSetState, 44=RSSetViewports,
//	45=RSSetScissorRects, 46=CopySubresourceRegion, 47=CopyResource
func copyResource(deviceCtx uintptr, dst, src uintptr) {
	vtbl := comVtbl(deviceCtx)
	method := comMethod(vtbl, 47)
	syscall.SyscallN(method, deviceCtx, dst, src)
}

func mapTexture(deviceCtx uintptr, resource uintptr) (*d3d11MappedSubresource, error) {
	var mapped d3d11MappedSubresource
	vtbl := comVtbl(deviceCtx)
	method := comMethod(vtbl, 14)
	hr, _, _ := syscall.SyscallN(method,
		deviceCtx,
		resource,
		0, // Subresource
		1, // D3D11_MAP_READ
		0, // MapFlags
		uintptr(unsafe.Pointer(&mapped)),
	)
	if hr != 0 {
		return nil, fmt.Errorf("Map HRESULT 0x%08X", hr)
	}
	return &mapped, nil
}

func unmapTexture(deviceCtx uintptr, resource uintptr) {
	vtbl := comVtbl(deviceCtx)
	method := comMethod(vtbl, 15)
	syscall.SyscallN(method, deviceCtx, resource, 0)
}
