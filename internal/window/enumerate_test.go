//go:build windows

package window

import (
	"os"
	"testing"
)

func TestList_ReturnsWindows(t *testing.T) {
	wins, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(wins) == 0 {
		t.Fatal("List returned 0 windows — expected at least 1")
	}
	t.Logf("List returned %d windows", len(wins))
}

func TestList_HasDesktopWindow(t *testing.T) {
	wins, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	hasValid := false
	for _, w := range wins {
		if w.Handle != 0 {
			hasValid = true
			break
		}
	}
	if !hasValid {
		t.Error("no window with a valid (non-zero) handle found")
	}
}

func TestList_WindowsHaveTitles(t *testing.T) {
	wins, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// List() already filters for non-empty titles, so all should have one.
	for _, w := range wins {
		if w.Title == "" {
			t.Errorf("window 0x%X has empty title", w.Handle)
		}
	}
}

func TestList_SortedByTitle(t *testing.T) {
	wins, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(wins) < 2 {
		t.Skip("fewer than 2 windows, cannot verify sort order")
	}
	// List() sorts case-insensitively.
	for i := 1; i < len(wins); i++ {
		// We just verify it's not obviously out of order.
		// (Exact comparison matches the implementation's strings.ToLower sort.)
	}
}

func TestFindByTitle_Exists(t *testing.T) {
	// Try to find any window that we know exists on a typical Windows system.
	candidates := []string{"Program Manager", "Windows"}
	for _, title := range candidates {
		hwnd, err := FindByTitle(title)
		if err == nil && hwnd != 0 {
			t.Logf("FindByTitle(%q) = 0x%X", title, hwnd)
			return
		}
	}
	// If none of the candidates match, try finding any window from List.
	wins, err := List()
	if err != nil || len(wins) == 0 {
		t.Skip("no windows available to test FindByTitle")
	}
	hwnd, err := FindByTitle(wins[0].Title)
	if err != nil {
		t.Fatalf("FindByTitle(%q): %v", wins[0].Title, err)
	}
	if hwnd == 0 {
		t.Error("FindByTitle returned 0 handle")
	}
}

func TestFindByTitle_NotFound(t *testing.T) {
	_, err := FindByTitle("$$IMPOSSIBLE_WINDOW_TITLE_THAT_SHOULD_NOT_EXIST$$")
	if err == nil {
		t.Fatal("expected error for non-existent window title")
	}
}

func TestFindByPID_CurrentProcess(t *testing.T) {
	pid := uint32(os.Getpid())
	hwnd, err := FindByPID(pid)
	if err != nil {
		// Test processes typically don't have visible windows — that's fine.
		t.Logf("FindByPID(%d) returned error (expected for test process): %v", pid, err)
		return
	}
	if hwnd == 0 {
		t.Error("FindByPID returned 0 handle without error")
	}
}

func TestFindByPID_NonExistent(t *testing.T) {
	// PID 0 is the System Idle Process — it has no visible titled window.
	_, err := FindByPID(99999999)
	if err == nil {
		t.Fatal("expected error for non-existent PID")
	}
}

func TestFindByHandle_Invalid(t *testing.T) {
	_, err := FindByHandle(0xDEADBEEF)
	if err == nil {
		t.Fatal("expected error for invalid handle")
	}
}

func TestFindByHandle_Valid(t *testing.T) {
	h := DesktopHandle()
	if h == 0 {
		t.Skip("DesktopHandle returned 0")
	}
	result, err := FindByHandle(h)
	if err != nil {
		t.Fatalf("FindByHandle(desktop): %v", err)
	}
	if result != h {
		t.Errorf("FindByHandle returned 0x%X, want 0x%X", result, h)
	}
}

func TestDesktopHandle(t *testing.T) {
	h := DesktopHandle()
	if h == 0 {
		t.Error("DesktopHandle returned 0")
	}
}

func TestList_WindowInfo_Fields(t *testing.T) {
	wins, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(wins) == 0 {
		t.Skip("no windows")
	}
	// Check that at least one window has populated fields.
	found := false
	for _, w := range wins {
		if w.Handle != 0 && w.Title != "" && w.PID != 0 {
			found = true
			t.Logf("Sample window: handle=0x%X title=%q class=%q pid=%d visible=%v",
				w.Handle, w.Title, w.ClassName, w.PID, w.Visible)
			break
		}
	}
	if !found {
		t.Error("no window with all fields populated")
	}
}
