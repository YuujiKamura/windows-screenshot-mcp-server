package window

import (
	"fmt"
	"sort"
	"strings"
	"syscall"
	"time"
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

// State captures runtime window visibility/focus state.
type State struct {
	Visible    bool
	Minimized  bool
	Foreground bool
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
	procIsIconic                 = modUser32.NewProc("IsIconic")
	procGetForegroundWindow      = modUser32.NewProc("GetForegroundWindow")
	procShowWindow               = modUser32.NewProc("ShowWindow")
	procSetForegroundWindow      = modUser32.NewProc("SetForegroundWindow")
	procGetShellWindow           = modUser32.NewProc("GetShellWindow")
)

const (
	swRestore  = 9
	swMinimize = 6
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

// InfoByHandle returns window metadata for a specific HWND.
func InfoByHandle(handle uintptr) (Info, error) {
	hwnd, err := FindByHandle(handle)
	if err != nil {
		return Info{}, err
	}
	return getInfo(hwnd), nil
}

// StateOf returns live state (visible/minimized/foreground) for a specific HWND.
func StateOf(handle uintptr) (State, error) {
	hwnd, err := FindByHandle(handle)
	if err != nil {
		return State{}, err
	}

	vis, _, _ := procIsWindowVisible.Call(hwnd)
	min, _, _ := procIsIconic.Call(hwnd)
	fg, _, _ := procGetForegroundWindow.Call()

	return State{
		Visible:    vis != 0,
		Minimized:  min != 0,
		Foreground: fg == hwnd,
	}, nil
}

// ApplyState attempts to transition the window into the requested state.
// Supported states: foreground, background, minimized.
func ApplyState(handle uintptr, state string) error {
	hwnd, err := FindByHandle(handle)
	if err != nil {
		return err
	}

	s := strings.ToLower(strings.TrimSpace(state))
	switch s {
	case "foreground":
		return focusWindow(hwnd)
	case "background":
		procShowWindow.Call(hwnd, swRestore)
		pm, pmErr := FindByTitle("Program Manager")
		if pmErr == nil && pm != 0 {
			if err := focusWindow(pm); err == nil {
				return nil
			}
		}

		shell, _, _ := procGetShellWindow.Call()
		if shell == 0 {
			return fmt.Errorf("GetShellWindow returned 0")
		}
		_ = focusWindow(shell)
		// Some Windows focus rules may reject explicit foreground change calls.
		// If target is not foreground after attempts, we treat it as background.
		if st, stErr := StateOf(hwnd); stErr == nil && !st.Foreground {
			return nil
		}
		return fmt.Errorf("failed to set shell foreground")
	case "minimized":
		procShowWindow.Call(hwnd, swMinimize)
		time.Sleep(150 * time.Millisecond)
		return nil
	case "occluded":
		// Ensure the target is visible first.
		procShowWindow.Call(hwnd, swRestore)
		time.Sleep(100 * time.Millisecond)

		// If something else is already foreground, this may already be occluded.
		fg, _, _ := procGetForegroundWindow.Call()
		if fg != 0 && fg != hwnd {
			return nil
		}

		// Pick another visible top-level window and foreground it.
		wins, listErr := List()
		if listErr != nil {
			return fmt.Errorf("list windows for occlusion: %w", listErr)
		}
		for _, wi := range wins {
			if !wi.Visible || wi.Handle == 0 || wi.Handle == hwnd {
				continue
			}
			ttl := strings.ToLower(strings.TrimSpace(wi.Title))
			if ttl == "" || ttl == "program manager" {
				continue
			}
			if err := focusWindow(wi.Handle); err != nil {
				continue
			}
			// If target is no longer foreground, we achieved practical occlusion.
			targetState, stErr := StateOf(hwnd)
			if stErr == nil && !targetState.Foreground {
				return nil
			}
		}
		return fmt.Errorf("failed to create occluded state")
	default:
		return fmt.Errorf("unsupported state %q", state)
	}
}

func focusWindow(hwnd uintptr) error {
	procShowWindow.Call(hwnd, swRestore)
	ret, _, _ := procSetForegroundWindow.Call(hwnd)
	time.Sleep(150 * time.Millisecond)
	if ret == 0 {
		return fmt.Errorf("failed to set foreground")
	}
	return nil
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
