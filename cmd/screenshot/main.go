package main

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/screenshot-mcp-server/internal/capture"
	"github.com/screenshot-mcp-server/internal/window"
	"github.com/spf13/cobra"
)

func main() {
	var (
		title   string
		pid     int
		handle  int
		desktop bool
		list    bool
		output  string
		method  string
		format  string
	)

	rootCmd := &cobra.Command{
		Use:   "screenshot",
		Short: "Windows screenshot capture tool",
		RunE: func(cmd *cobra.Command, args []string) error {
			if list {
				return listWindows()
			}

			engine := capture.NewEngine(capture.Method(method))

			var result *capture.CaptureResult
			var err error

			if desktop {
				result, err = engine.CaptureDesktop()
			} else {
				hwnd, findErr := resolveTarget(title, uint32(pid), uintptr(handle))
				if findErr != nil {
					return findErr
				}
				result, err = engine.CaptureWindow(hwnd)
			}

			if err != nil {
				return err
			}

			if output == "" {
				output = fmt.Sprintf("screenshot_%d.png", time.Now().Unix())
			}

			if err := capture.SaveImage(result.Image, output, format); err != nil {
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
