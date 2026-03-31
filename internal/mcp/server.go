package mcp

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"os"

	"github.com/screenshot-mcp-server/internal/capture"
	"github.com/screenshot-mcp-server/internal/window"
)

// JSON-RPC 2.0 types

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
}

type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Server handles MCP stdio communication.
type Server struct{}

// NewServer creates a new MCP server.
func NewServer() *Server {
	return &Server{}
}

// Run reads JSON-RPC requests from stdin line-by-line and writes responses to stdout.
func (s *Server) Run() error {
	fmt.Fprintln(os.Stderr, "screenshot-mcp-server: MCP mode started, reading from stdin")

	scanner := bufio.NewScanner(os.Stdin)
	// Allow large messages (up to 10 MB) for potential large responses
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeError(nil, -32700, "Parse error: "+err.Error())
			continue
		}

		s.handleRequest(&req)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stdin scanner: %w", err)
	}
	return nil
}

func (s *Server) handleRequest(req *Request) {
	switch req.Method {
	case "initialize":
		s.writeResult(req.ID, map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "screenshot",
				"version": "1.0.0",
			},
		})

	case "notifications/initialized":
		// Notification — no response needed

	case "tools/list":
		s.writeResult(req.ID, map[string]interface{}{
			"tools": s.toolDefinitions(),
		})

	case "tools/call":
		s.handleToolCall(req)

	default:
		s.writeError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func (s *Server) toolDefinitions() []interface{} {
	return []interface{}{
		map[string]interface{}{
			"name":        "screenshot_capture",
			"description": "Capture a screenshot of a window or the desktop. Returns base64-encoded PNG image.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Window title substring to match (case-insensitive)",
					},
					"pid": map[string]interface{}{
						"type":        "integer",
						"description": "Process ID of the target window",
					},
					"handle": map[string]interface{}{
						"type":        "integer",
						"description": "Window handle (HWND) as integer",
					},
					"desktop": map[string]interface{}{
						"type":        "boolean",
						"description": "Capture the full desktop",
					},
					"method": map[string]interface{}{
						"type":        "string",
						"description": "Capture method: auto|capture|print|bitblt",
						"enum":        []string{"auto", "capture", "print", "bitblt"},
					},
				},
			},
		},
		map[string]interface{}{
			"name":        "screenshot_list",
			"description": "List all visible windows with their handles, PIDs, and titles.",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

func (s *Server) handleToolCall(req *Request) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(req.ID, -32602, "Invalid params: "+err.Error())
		return
	}

	switch params.Name {
	case "screenshot_capture":
		s.handleCapture(req.ID, params.Arguments)
	case "screenshot_list":
		s.handleList(req.ID)
	default:
		s.writeError(req.ID, -32602, fmt.Sprintf("Unknown tool: %s", params.Name))
	}
}

func (s *Server) handleCapture(id interface{}, argsRaw json.RawMessage) {
	var args struct {
		Title   string `json:"title"`
		PID     int    `json:"pid"`
		Handle  int    `json:"handle"`
		Desktop bool   `json:"desktop"`
		Method  string `json:"method"`
	}
	if argsRaw != nil {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			s.writeError(id, -32602, "Invalid arguments: "+err.Error())
			return
		}
	}

	// Select capture method
	method := capture.MethodAuto
	if args.Method != "" {
		method = capture.Method(args.Method)
	}
	engine := capture.NewEngine(method)

	var result *capture.CaptureResult
	var err error

	if args.Desktop {
		result, err = engine.CaptureDesktop()
	} else {
		var hwnd uintptr
		if args.Handle != 0 {
			hwnd, err = window.FindByHandle(uintptr(args.Handle))
		} else if args.PID != 0 {
			hwnd, err = window.FindByPID(uint32(args.PID))
		} else if args.Title != "" {
			hwnd, err = window.FindByTitle(args.Title)
		} else {
			s.writeToolError(id, "Specify at least one of: title, pid, handle, or desktop=true")
			return
		}
		if err != nil {
			s.writeToolError(id, err.Error())
			return
		}
		result, err = engine.CaptureWindow(hwnd)
	}

	if err != nil {
		s.writeToolError(id, err.Error())
		return
	}

	// Encode image to PNG then base64
	var buf bytes.Buffer
	if err := png.Encode(&buf, result.Image); err != nil {
		s.writeToolError(id, fmt.Sprintf("PNG encode error: %v", err))
		return
	}
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	s.writeResult(id, map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{
				"type":     "image",
				"data":     b64,
				"mimeType": "image/png",
			},
			map[string]interface{}{
				"type": "text",
				"text": fmt.Sprintf("Captured %dx%d using method=%s in %s", result.Width, result.Height, result.Method, result.Duration),
			},
		},
	})
}

func (s *Server) handleList(id interface{}) {
	wins, err := window.List()
	if err != nil {
		s.writeToolError(id, err.Error())
		return
	}

	var items []map[string]interface{}
	for _, w := range wins {
		items = append(items, map[string]interface{}{
			"handle":    fmt.Sprintf("0x%X", w.Handle),
			"pid":       w.PID,
			"title":     w.Title,
			"className": w.ClassName,
			"visible":   w.Visible,
		})
	}

	// Marshal the window list as text content for MCP
	listJSON, _ := json.Marshal(items)

	s.writeResult(id, map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": string(listJSON),
			},
		},
	})
}

// writeResult sends a successful JSON-RPC response.
func (s *Server) writeResult(id interface{}, result interface{}) {
	resp := Response{JSONRPC: "2.0", ID: id, Result: result}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(os.Stdout, "%s\n", data)
}

// writeError sends a JSON-RPC protocol-level error.
func (s *Server) writeError(id interface{}, code int, message string) {
	resp := Response{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: message}}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(os.Stdout, "%s\n", data)
}

// writeToolError sends a tool-level error as MCP content with isError flag.
func (s *Server) writeToolError(id interface{}, message string) {
	s.writeResult(id, map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": message,
			},
		},
		"isError": true,
	})
}
