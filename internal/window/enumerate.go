package window

import (
	"fmt"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Info holds metadata about a top-level window.
type Info struct {
	Handle    uintptr
	Title     string
	ClassName string
	PID       uint32
	Visible   bool
	Rect      Rect
}

// Rect mirrors the Win32 RECT structure.
type Rect struct {
	Left, Top, Right, Bottom int32
}

// Win32 API
var (
	modUser32 = windows.NewLazyDLL("user32.dll")

	procEnumWindows              = modUser32.NewProc("EnumWindows")
	procGetWindowTextW           = modUser32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = modUser32.NewProc("GetWindowTextLengthW")
	procGetClassNameW            = modUser32.NewProc("GetClassNameW")
	procGetWindowThreadProcessId = modUser32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = modUser32.NewProc("IsWindowVisible")
	procGetWindowRect            = modUser32.NewProc("GetWindowRect")
	procIsWindow                 = modUser32.NewProc("IsWindow")
	procGetDesktopWindow         = modUser32.NewProc("GetDesktopWindow")
)

// List enumerates all top-level windows that have a non-empty title.
// Results are sorted alphabetically by title (case-insensitive).
func List() ([]Info, error) {
	var result []Info

	cb := syscall.NewCallback(func(hwnd, lParam uintptr) uintptr {
		info := getInfo(hwnd)
		if info.Title != "" {
			result = append(result, info)
		}
		return 1 // continue enumeration
	})

	ret, _, err := procEnumWindows.Call(cb, 0)
	if ret == 0 {
		return nil, fmt.Errorf("EnumWindows failed: %w", err)
	}

	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Title) < strings.ToLower(result[j].Title)
	})

	return result, nil
}

// FindByTitle returns the HWND of the first window whose title contains the
// given substring (case-insensitive).
func FindByTitle(title string) (uintptr, error) {
	lower := strings.ToLower(title)
	var found uintptr

	cb := syscall.NewCallback(func(hwnd, lParam uintptr) uintptr {
		t := windowTitle(hwnd)
		if strings.Contains(strings.ToLower(t), lower) {
			found = hwnd
			return 0 // stop
		}
		return 1
	})

	procEnumWindows.Call(cb, 0)

	if found == 0 {
		return 0, fmt.Errorf("no window matching title %q", title)
	}
	return found, nil
}

// FindByPID returns the HWND of the first visible, titled window belonging to
// the given process ID.
func FindByPID(pid uint32) (uintptr, error) {
	var found uintptr

	cb := syscall.NewCallback(func(hwnd, lParam uintptr) uintptr {
		var wpid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if wpid != pid {
			return 1
		}
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1
		}
		if windowTitle(hwnd) == "" {
			return 1
		}
		found = hwnd
		return 0 // stop
	})

	procEnumWindows.Call(cb, 0)

	if found == 0 {
		return 0, fmt.Errorf("no visible window for PID %d", pid)
	}
	return found, nil
}

// FindByHandle verifies that the given handle refers to a valid window and
// returns it.
func FindByHandle(handle uintptr) (uintptr, error) {
	ret, _, _ := procIsWindow.Call(handle)
	if ret == 0 {
		return 0, fmt.Errorf("handle 0x%X is not a valid window", handle)
	}
	return handle, nil
}

// DesktopHandle returns the HWND of the desktop window.
func DesktopHandle() uintptr {
	h, _, _ := procGetDesktopWindow.Call()
	return h
}

// --- helpers ----------------------------------------------------------------

func getInfo(hwnd uintptr) Info {
	info := Info{Handle: hwnd}

	info.Title = windowTitle(hwnd)
	info.ClassName = windowClassName(hwnd)

	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	info.PID = pid

	vis, _, _ := procIsWindowVisible.Call(hwnd)
	info.Visible = vis != 0

	var r struct{ Left, Top, Right, Bottom int32 }
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	info.Rect = Rect{Left: r.Left, Top: r.Top, Right: r.Right, Bottom: r.Bottom}

	return info
}

func windowTitle(hwnd uintptr) string {
	length, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if length == 0 {
		return ""
	}
	buf := make([]uint16, length+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func windowClassName(hwnd uintptr) string {
	buf := make([]uint16, 256)
	procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf)
}
