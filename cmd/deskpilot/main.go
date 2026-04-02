package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/screenshot-mcp-server/internal/capture"
	"github.com/screenshot-mcp-server/internal/mcp"
	"github.com/screenshot-mcp-server/internal/overlay"
	"github.com/screenshot-mcp-server/internal/window"
	"github.com/spf13/cobra"
)

func main() {
	var (
		title           string
		pid             int
		handle          int
		desktop         bool
		list            bool
		output          string
		method          string
		format          string
		mcpMode         bool
		grid            bool
		crosshair       string
		diagPath        string
		diagAll         bool
		diagState       string
		diagJSONL       string
		diagSummary     string
		diagStateMatrix string
		diagSettleMs    int
	)

	rootCmd := &cobra.Command{
		Use:   "deskpilot",
		Short: "Windows desktop capture and coordinate overlay tool for AI agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			if mcpMode {
				return mcp.NewServer().Run()
			}
			if diagSummary != "" {
				return runDiagSummary(diagSummary)
			}
			if diagStateMatrix != "" {
				return runStateMatrix(
					title, pid, handle, desktop,
					method, format, output,
					diagPath, diagJSONL, diagAll, diagStateMatrix, diagSettleMs,
				)
			}

			if list {
				return listWindows()
			}

			engine := capture.NewEngine(capture.Method(method))
			report := newDiagReport(method, title, pid, handle, desktop)
			report.StateLabel = strings.TrimSpace(diagState)

			var result *capture.CaptureResult
			var trace *capture.Trace
			var err error

			if desktop {
				report.Target.Resolved.Handle = fmt.Sprintf("0x%X", window.DesktopHandle())
				result, trace, err = engine.CaptureDesktopWithTrace()
				if diagAll {
					report.Comparisons = runDiagMatrixDesktop()
				}
			} else {
				hwnd, findErr := resolveTarget(title, uint32(pid), uintptr(handle))
				if findErr != nil {
					report.Capture.Success = false
					report.Capture.FailureCode = "NO_WINDOW"
					report.Capture.FailureError = findErr.Error()
					_ = writeDiag(diagPath, report)
					_ = appendDiagJSONL(diagJSONL, report)
					return findErr
				}
				report.Target.Resolved.Handle = fmt.Sprintf("0x%X", hwnd)
				fillResolvedWindowInfo(report, hwnd)
				result, trace, err = engine.CaptureWindowWithTrace(hwnd)
				if diagAll {
					report.Comparisons = runDiagMatrixWindow(hwnd)
				}
			}
			fillTrace(report, trace)

			if err != nil {
				report.Capture.Success = false
				report.Capture.FailureCode = "CAPTURE_FAILED"
				report.Capture.FailureError = err.Error()
				if lastCode := lastFailureCode(trace); lastCode != "" {
					report.Capture.FailureCode = lastCode
				}
				_ = writeDiag(diagPath, report)
				_ = appendDiagJSONL(diagJSONL, report)
				return err
			}

			// Apply overlays if requested
			if grid || crosshair != "" {
				rgba := overlay.ToRGBA(result.Image)
				if grid {
					overlay.DrawGrid(rgba, 100)
					overlay.DrawRulers(rgba, 100)
				}
				if crosshair != "" {
					cx, cy, parseErr := parseCrosshair(crosshair)
					if parseErr != nil {
						return parseErr
					}
					overlay.DrawCrosshair(rgba, cx, cy)
				}
				result.Image = rgba
			}

			if output == "" {
				output = fmt.Sprintf("screenshot_%d.png", time.Now().Unix())
			}
			absOutput := output
			if resolved, resolveErr := filepath.Abs(output); resolveErr == nil {
				absOutput = resolved
			}

			if err := capture.SaveImage(result.Image, output, format); err != nil {
				report.Capture.Success = false
				report.Capture.FailureCode = "WRITE_FAILED"
				report.Capture.FailureError = err.Error()
				_ = writeDiag(diagPath, report)
				_ = appendDiagJSONL(diagJSONL, report)
				return err
			}
			var outBytes int64
			if st, statErr := os.Stat(output); statErr == nil {
				outBytes = st.Size()
			}

			report.Capture.Success = true
			report.Capture.Method = string(result.Method)
			report.Capture.Width = result.Width
			report.Capture.Height = result.Height
			report.Capture.DurationMs = result.Duration.Milliseconds()
			report.Capture.OutputPath = absOutput
			report.Capture.OutputBytes = outBytes
			if err := writeDiag(diagPath, report); err != nil {
				return err
			}
			if err := appendDiagJSONL(diagJSONL, report); err != nil {
				return err
			}

			fmt.Printf("Saved: %s (%dx%d, method=%s, took=%s)\n",
				output, result.Width, result.Height, result.Method, result.Duration)
			return nil
		},
	}

	rootCmd.Flags().StringVar(&title, "title", "", "Window title (substring match)")
	rootCmd.Flags().IntVar(&pid, "pid", 0, "Process ID")
	rootCmd.Flags().IntVar(&handle, "handle", 0, "Window handle (HWND)")
	rootCmd.Flags().BoolVar(&desktop, "desktop", false, "Capture full desktop")
	rootCmd.Flags().BoolVar(&list, "list", false, "List all windows")
	rootCmd.Flags().StringVarP(&output, "output", "o", "", "Output file path")
	rootCmd.Flags().StringVar(&method, "method", "auto", "Capture method: auto|capture|print|bitblt")
	rootCmd.Flags().StringVar(&format, "format", "png", "Image format: png|jpeg")
	rootCmd.Flags().BoolVar(&mcpMode, "mcp", false, "Run as MCP stdio server (JSON-RPC 2.0)")
	rootCmd.Flags().BoolVar(&grid, "grid", false, "Overlay 100px grid with coordinate rulers")
	rootCmd.Flags().StringVar(&crosshair, "crosshair", "", "Draw crosshair at x,y coordinates (e.g. '500,300')")
	rootCmd.Flags().StringVar(&diagPath, "diag", "", "Write capture diagnostics as JSON to this path ('-' for stdout)")
	rootCmd.Flags().BoolVar(&diagAll, "diag-all-methods", false, "Run print/capture/bitblt/auto and include comparison results in diag JSON")
	rootCmd.Flags().StringVar(&diagState, "diag-state-label", "", "Optional state label (foreground|background|occluded|minimized|custom)")
	rootCmd.Flags().StringVar(&diagJSONL, "diag-jsonl", "", "Append one-line JSON diagnostics to this JSONL file")
	rootCmd.Flags().StringVar(&diagSummary, "diag-summarize-jsonl", "", "Read diagnostics JSONL and print grouped state summary")
	rootCmd.Flags().StringVar(&diagStateMatrix, "diag-state-matrix", "", "Run a comma-separated state sequence (e.g. foreground,background,occluded,minimized) and emit one diag per state")
	rootCmd.Flags().IntVar(&diagSettleMs, "diag-settle-ms", 350, "Wait time in ms after each state transition before capture")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func listWindows() error {
	wins, err := window.List()
	if err != nil {
		return fmt.Errorf("enumerate windows: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "HANDLE\tPID\tVISIBLE\tTITLE")
	for _, wi := range wins {
		vis := "no"
		if wi.Visible {
			vis = "yes"
		}
		fmt.Fprintf(w, "0x%X\t%d\t%s\t%s\n", wi.Handle, wi.PID, vis, wi.Title)
	}
	w.Flush()

	fmt.Fprintf(os.Stderr, "\n(%d windows)\n", len(wins))
	return nil
}

func resolveTarget(title string, pid uint32, handle uintptr) (uintptr, error) {
	if handle != 0 {
		return window.FindByHandle(handle)
	}
	if pid != 0 {
		return window.FindByPID(pid)
	}
	if title != "" {
		return window.FindByTitle(title)
	}
	return 0, fmt.Errorf("specify --title, --pid, --handle, or --desktop")
}

func parseCrosshair(s string) (int, int, error) {
	parts := strings.SplitN(s, ",", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("crosshair format must be 'x,y', got %q", s)
	}
	x, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid crosshair x: %w", err)
	}
	y, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid crosshair y: %w", err)
	}
	return x, y, nil
}

type diagReport struct {
	Timestamp   time.Time       `json:"timestamp"`
	StateLabel  string          `json:"state_label,omitempty"`
	Target      diagTarget      `json:"target"`
	Trace       diagTrace       `json:"trace"`
	Capture     diagCapture     `json:"capture"`
	Comparisons []diagMethodRun `json:"comparisons,omitempty"`
}

type diagTarget struct {
	Input    diagTargetInput    `json:"input"`
	Resolved diagTargetResolved `json:"resolved"`
	State    diagTargetState    `json:"state"`
}

type diagTargetInput struct {
	Desktop bool   `json:"desktop"`
	Title   string `json:"title,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Handle  int    `json:"handle,omitempty"`
}

type diagTargetResolved struct {
	Handle string `json:"handle,omitempty"`
	Title  string `json:"title,omitempty"`
	Class  string `json:"class,omitempty"`
	PID    uint32 `json:"pid,omitempty"`
}

type diagTargetState struct {
	Visible    bool `json:"visible"`
	Minimized  bool `json:"minimized"`
	Foreground bool `json:"foreground"`
}

type diagTrace struct {
	RequestedMethod string        `json:"requested_method"`
	SelectedMethod  string        `json:"selected_method,omitempty"`
	StopReason      string        `json:"stop_reason,omitempty"`
	FallbackSummary string        `json:"fallback_summary,omitempty"`
	Attempts        []diagAttempt `json:"attempts"`
}

type diagAttempt struct {
	Method       string `json:"method"`
	Success      bool   `json:"success"`
	DurationMs   int64  `json:"duration_ms"`
	FailureCode  string `json:"failure_code,omitempty"`
	FailureError string `json:"failure_error,omitempty"`
}

type diagCapture struct {
	Success      bool   `json:"success"`
	Method       string `json:"method,omitempty"`
	FailureCode  string `json:"failure_code,omitempty"`
	FailureError string `json:"failure_error,omitempty"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	DurationMs   int64  `json:"duration_ms,omitempty"`
	OutputPath   string `json:"output_path,omitempty"`
	OutputBytes  int64  `json:"output_bytes,omitempty"`
}

type diagMethodRun struct {
	RequestedMethod string        `json:"requested_method"`
	SelectedMethod  string        `json:"selected_method,omitempty"`
	StopReason      string        `json:"stop_reason,omitempty"`
	FallbackSummary string        `json:"fallback_summary,omitempty"`
	Success         bool          `json:"success"`
	FailureCode     string        `json:"failure_code,omitempty"`
	FailureError    string        `json:"failure_error,omitempty"`
	Width           int           `json:"width,omitempty"`
	Height          int           `json:"height,omitempty"`
	DurationMs      int64         `json:"duration_ms,omitempty"`
	Attempts        []diagAttempt `json:"attempts,omitempty"`
}

func newDiagReport(method, title string, pid, handle int, desktop bool) *diagReport {
	return &diagReport{
		Timestamp: time.Now(),
		Target: diagTarget{
			Input: diagTargetInput{
				Desktop: desktop,
				Title:   title,
				PID:     pid,
				Handle:  handle,
			},
		},
		Trace: diagTrace{
			RequestedMethod: method,
		},
	}
}

func fillResolvedWindowInfo(report *diagReport, hwnd uintptr) {
	if info, err := window.InfoByHandle(hwnd); err == nil {
		report.Target.Resolved.Title = info.Title
		report.Target.Resolved.Class = info.ClassName
		report.Target.Resolved.PID = info.PID
	}
	if st, err := window.StateOf(hwnd); err == nil {
		report.Target.State.Visible = st.Visible
		report.Target.State.Minimized = st.Minimized
		report.Target.State.Foreground = st.Foreground
	}
}

func fillTrace(report *diagReport, trace *capture.Trace) {
	if report == nil || trace == nil {
		return
	}
	report.Trace.RequestedMethod = string(trace.RequestedMethod)
	report.Trace.SelectedMethod = string(trace.SelectedMethod)
	report.Trace.StopReason = trace.StopReason
	report.Trace.FallbackSummary = trace.FallbackSummary
	report.Trace.Attempts = make([]diagAttempt, 0, len(trace.Attempts))
	for _, a := range trace.Attempts {
		report.Trace.Attempts = append(report.Trace.Attempts, diagAttempt{
			Method:       string(a.Method),
			Success:      a.Success,
			DurationMs:   a.Duration.Milliseconds(),
			FailureCode:  a.FailureCode,
			FailureError: a.FailureError,
		})
	}
}

func lastFailureCode(trace *capture.Trace) string {
	if trace == nil {
		return ""
	}
	for i := len(trace.Attempts) - 1; i >= 0; i-- {
		if code := trace.Attempts[i].FailureCode; code != "" {
			return code
		}
	}
	return ""
}

func writeDiag(path string, report *diagReport) error {
	if path == "" || report == nil {
		return nil
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal diag json: %w", err)
	}
	if path == "-" {
		fmt.Println(string(data))
		return nil
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write diag file %q: %w", path, err)
	}
	return nil
}

func appendDiagJSONL(path string, report *diagReport) error {
	if path == "" || report == nil {
		return nil
	}
	data, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal diag jsonl: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open diag jsonl %q: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append diag jsonl %q: %w", path, err)
	}
	return nil
}

func runStateMatrix(
	title string,
	pid int,
	handle int,
	desktop bool,
	method string,
	format string,
	output string,
	diagPath string,
	diagJSONL string,
	diagAll bool,
	stateMatrix string,
	settleMs int,
) error {
	if desktop {
		return fmt.Errorf("--diag-state-matrix requires a window target, desktop is not supported")
	}
	if strings.TrimSpace(diagJSONL) == "" {
		return fmt.Errorf("--diag-state-matrix requires --diag-jsonl to persist per-state diagnostics")
	}

	hwnd, err := resolveTarget(title, uint32(pid), uintptr(handle))
	if err != nil {
		return err
	}

	states := parseStates(stateMatrix)
	if len(states) == 0 {
		return fmt.Errorf("no valid states in --diag-state-matrix")
	}
	if settleMs < 0 {
		settleMs = 0
	}

	var finalErr error
	var successCount int
	for _, state := range states {
		report := newDiagReport(method, title, pid, handle, false)
		report.StateLabel = state
		report.Target.Resolved.Handle = fmt.Sprintf("0x%X", hwnd)
		fillResolvedWindowInfo(report, hwnd)

		if err := window.ApplyState(hwnd, state); err != nil {
			report.Capture.Success = false
			report.Capture.FailureCode = "STATE_APPLY_FAILED"
			report.Capture.FailureError = err.Error()
			if statePath := expandStatePath(diagPath, state); statePath != "" {
				_ = writeDiag(statePath, report)
			}
			_ = appendDiagJSONL(diagJSONL, report)
			fmt.Printf("[%s] transition failed: %v\n", state, err)
			finalErr = err
			continue
		}

		time.Sleep(time.Duration(settleMs) * time.Millisecond)
		fillResolvedWindowInfo(report, hwnd)

		engine := capture.NewEngine(capture.Method(method))
		result, trace, capErr := engine.CaptureWindowWithTrace(hwnd)
		fillTrace(report, trace)
		if diagAll {
			report.Comparisons = runDiagMatrixWindow(hwnd)
		}

		if capErr != nil {
			report.Capture.Success = false
			report.Capture.FailureCode = "CAPTURE_FAILED"
			report.Capture.FailureError = capErr.Error()
			if lastCode := lastFailureCode(trace); lastCode != "" {
				report.Capture.FailureCode = lastCode
			}
			if statePath := expandStatePath(diagPath, state); statePath != "" {
				_ = writeDiag(statePath, report)
			}
			_ = appendDiagJSONL(diagJSONL, report)
			fmt.Printf("[%s] capture failed: %v\n", state, capErr)
			finalErr = capErr
			continue
		}

		outPath := outputForState(output, state)
		if outPath == "" {
			outPath = fmt.Sprintf("screenshot_%s_%d.png", state, time.Now().Unix())
		}
		if err := capture.SaveImage(result.Image, outPath, format); err != nil {
			report.Capture.Success = false
			report.Capture.FailureCode = "WRITE_FAILED"
			report.Capture.FailureError = err.Error()
			if statePath := expandStatePath(diagPath, state); statePath != "" {
				_ = writeDiag(statePath, report)
			}
			_ = appendDiagJSONL(diagJSONL, report)
			fmt.Printf("[%s] write failed: %v\n", state, err)
			finalErr = err
			continue
		}

		absOutput := outPath
		if resolved, resolveErr := filepath.Abs(outPath); resolveErr == nil {
			absOutput = resolved
		}
		var outBytes int64
		if st, statErr := os.Stat(outPath); statErr == nil {
			outBytes = st.Size()
		}
		report.Capture.Success = true
		report.Capture.Method = string(result.Method)
		report.Capture.Width = result.Width
		report.Capture.Height = result.Height
		report.Capture.DurationMs = result.Duration.Milliseconds()
		report.Capture.OutputPath = absOutput
		report.Capture.OutputBytes = outBytes

		if statePath := expandStatePath(diagPath, state); statePath != "" {
			_ = writeDiag(statePath, report)
		}
		_ = appendDiagJSONL(diagJSONL, report)
		fmt.Printf("[%s] saved: %s (%dx%d)\n", state, outPath, result.Width, result.Height)
		successCount++
	}

	if err := runDiagSummary(diagJSONL); err != nil {
		return err
	}
	if successCount > 0 {
		return nil
	}
	return finalErr
}

func parseStates(v string) []string {
	raw := strings.Split(v, ",")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		x := strings.ToLower(strings.TrimSpace(s))
		if x == "" {
			continue
		}
		out = append(out, x)
	}
	return out
}

func expandStatePath(path string, state string) string {
	if strings.TrimSpace(path) == "" || path == "-" {
		return path
	}
	if strings.Contains(path, "{state}") {
		return strings.ReplaceAll(path, "{state}", state)
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	if ext == "" {
		return base + "_" + state
	}
	return base + "_" + state + ext
}

func outputForState(path string, state string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	if strings.Contains(path, "{state}") {
		return strings.ReplaceAll(path, "{state}", state)
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	if ext == "" {
		return base + "_" + state
	}
	return base + "_" + state + ext
}

func runDiagSummary(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open summary source %q: %w", path, err)
	}
	defer f.Close()

	type bucket struct {
		total         int
		success       int
		failureCounts map[string]int
	}
	byState := map[string]*bucket{}

	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rep diagReport
		if err := json.Unmarshal([]byte(line), &rep); err != nil {
			return fmt.Errorf("parse jsonl line %d: %w", lineNo, err)
		}

		state := strings.TrimSpace(rep.StateLabel)
		if state == "" {
			state = "unlabeled"
		}
		b := byState[state]
		if b == nil {
			b = &bucket{failureCounts: map[string]int{}}
			byState[state] = b
		}
		b.total++
		if rep.Capture.Success {
			b.success++
		} else {
			code := strings.TrimSpace(rep.Capture.FailureCode)
			if code == "" {
				code = "CAPTURE_FAILED"
			}
			b.failureCounts[code]++
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read summary source %q: %w", path, err)
	}

	states := make([]string, 0, len(byState))
	for s := range byState {
		states = append(states, s)
	}
	sort.Strings(states)

	if len(states) == 0 {
		fmt.Println("No diagnostics found.")
		return nil
	}

	fmt.Println("State Summary")
	fmt.Println("-------------")
	for _, state := range states {
		b := byState[state]
		rate := 0.0
		if b.total > 0 {
			rate = float64(b.success) * 100.0 / float64(b.total)
		}
		fmt.Printf("%s: success %d/%d (%.1f%%)\n", state, b.success, b.total, rate)
		if len(b.failureCounts) == 0 {
			continue
		}
		fmt.Printf("  failures: %s\n", joinFailureCounts(b.failureCounts))
	}

	return nil
}

func joinFailureCounts(m map[string]int) string {
	type kv struct {
		key string
		n   int
	}
	arr := make([]kv, 0, len(m))
	for k, n := range m {
		arr = append(arr, kv{key: k, n: n})
	}
	sort.Slice(arr, func(i, j int) bool {
		if arr[i].n == arr[j].n {
			return arr[i].key < arr[j].key
		}
		return arr[i].n > arr[j].n
	})
	var b strings.Builder
	for i, item := range arr {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(item.key)
		b.WriteString("=")
		b.WriteString(strconv.Itoa(item.n))
	}
	return b.String()
}

func runDiagMatrixWindow(hwnd uintptr) []diagMethodRun {
	methods := []capture.Method{
		capture.MethodPrint,
		capture.MethodCapture,
		capture.MethodBitBlt,
		capture.MethodAuto,
	}
	return runDiagMatrix(methods, func(e *capture.Engine) (*capture.CaptureResult, *capture.Trace, error) {
		return e.CaptureWindowWithTrace(hwnd)
	})
}

func runDiagMatrixDesktop() []diagMethodRun {
	methods := []capture.Method{
		capture.MethodPrint,
		capture.MethodCapture,
		capture.MethodBitBlt,
		capture.MethodAuto,
	}
	return runDiagMatrix(methods, func(e *capture.Engine) (*capture.CaptureResult, *capture.Trace, error) {
		return e.CaptureDesktopWithTrace()
	})
}

func runDiagMatrix(methods []capture.Method, runner func(e *capture.Engine) (*capture.CaptureResult, *capture.Trace, error)) []diagMethodRun {
	results := make([]diagMethodRun, 0, len(methods))
	for _, m := range methods {
		engine := capture.NewEngine(m)
		res, trace, err := runner(engine)
		entry := diagMethodRun{
			RequestedMethod: string(m),
		}
		if trace != nil {
			entry.SelectedMethod = string(trace.SelectedMethod)
			entry.StopReason = trace.StopReason
			entry.FallbackSummary = trace.FallbackSummary
			entry.Attempts = convertAttempts(trace.Attempts)
		}
		if err != nil {
			entry.Success = false
			entry.FailureError = err.Error()
			entry.FailureCode = lastFailureCode(trace)
			if entry.FailureCode == "" {
				entry.FailureCode = "CAPTURE_FAILED"
			}
			results = append(results, entry)
			continue
		}

		entry.Success = true
		entry.Width = res.Width
		entry.Height = res.Height
		entry.DurationMs = res.Duration.Milliseconds()
		results = append(results, entry)
	}
	return results
}

func convertAttempts(attempts []capture.AttemptTrace) []diagAttempt {
	out := make([]diagAttempt, 0, len(attempts))
	for _, a := range attempts {
		out = append(out, diagAttempt{
			Method:       string(a.Method),
			Success:      a.Success,
			DurationMs:   a.Duration.Milliseconds(),
			FailureCode:  a.FailureCode,
			FailureError: a.FailureError,
		})
	}
	return out
}
