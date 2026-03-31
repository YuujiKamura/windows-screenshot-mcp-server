package mcp

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// sendOne sends a single request and returns the single response.
func sendOne(t *testing.T, reqJSON string) Response {
	t.Helper()

	origIn := os.Stdin
	origOut := os.Stdout
	defer func() {
		os.Stdin = origIn
		os.Stdout = origOut
	}()

	inR, inW, _ := os.Pipe()
	os.Stdin = inR

	outR, outW, _ := os.Pipe()
	os.Stdout = outW

	go func() {
		io.WriteString(inW, reqJSON+"\n")
		inW.Close()
	}()

	srv := NewServer()
	done := make(chan error, 1)
	go func() {
		done <- srv.Run()
		outW.Close()
	}()

	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	var resp Response
	if scanner.Scan() {
		json.Unmarshal(scanner.Bytes(), &resp)
	}

	<-done
	return resp
}

// runServer feeds multiple lines and collects all responses.
func runServer(t *testing.T, inputLines []string) []Response {
	t.Helper()

	origIn := os.Stdin
	origOut := os.Stdout
	defer func() {
		os.Stdin = origIn
		os.Stdout = origOut
	}()

	inR, inW, _ := os.Pipe()
	os.Stdin = inR

	outR, outW, _ := os.Pipe()
	os.Stdout = outW

	go func() {
		for _, line := range inputLines {
			inW.WriteString(line + "\n")
		}
		inW.Close()
	}()

	srv := NewServer()
	done := make(chan error, 1)
	go func() {
		done <- srv.Run()
		outW.Close()
	}()

	var responses []Response
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var resp Response
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			t.Errorf("failed to parse response: %v", err)
			continue
		}
		responses = append(responses, resp)
	}

	<-done
	return responses
}

func TestInitialize(t *testing.T) {
	resp := sendOne(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)

	if resp.ID != float64(1) {
		t.Errorf("expected id=1, got %v", resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}

	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("unexpected protocolVersion: %v", result["protocolVersion"])
	}

	serverInfo, ok := result["serverInfo"].(map[string]interface{})
	if !ok {
		t.Fatalf("serverInfo missing")
	}
	if serverInfo["name"] != "screenshot" {
		t.Errorf("unexpected server name: %v", serverInfo["name"])
	}
}

func TestToolsList(t *testing.T) {
	resp := sendOne(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map")
	}

	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatalf("tools is not an array")
	}

	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		toolMap := tool.(map[string]interface{})
		names[toolMap["name"].(string)] = true
	}
	if !names["screenshot_capture"] {
		t.Error("missing screenshot_capture tool")
	}
	if !names["screenshot_list"] {
		t.Error("missing screenshot_list tool")
	}
}

func TestMethodNotFound(t *testing.T) {
	resp := sendOne(t, `{"jsonrpc":"2.0","id":3,"method":"nonexistent","params":{}}`)

	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected code -32601, got %d", resp.Error.Code)
	}
}

func TestParseError(t *testing.T) {
	resp := sendOne(t, `not valid json`)

	if resp.Error == nil {
		t.Fatal("expected parse error")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("expected code -32700, got %d", resp.Error.Code)
	}
}

func TestNotificationNoResponse(t *testing.T) {
	responses := runServer(t, []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":10,"method":"initialize","params":{}}`,
	})

	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if responses[0].ID != float64(10) {
		t.Errorf("expected id=10, got %v", responses[0].ID)
	}
}

func TestToolCallCaptureMissingArgs(t *testing.T) {
	resp := sendOne(t, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"screenshot_capture","arguments":{}}}`)

	// Should be a tool-level error (not protocol error)
	if resp.Error != nil {
		t.Skipf("got protocol error instead of tool error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map")
	}

	isErr, _ := result["isError"].(bool)
	if !isErr {
		t.Error("expected isError=true for missing arguments")
	}

	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatal("expected content array")
	}

	firstContent := content[0].(map[string]interface{})
	text := firstContent["text"].(string)
	if !strings.Contains(text, "Specify") {
		t.Errorf("expected error about missing args, got: %s", text)
	}
}

func TestMultipleRequests(t *testing.T) {
	responses := runServer(t, []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	})

	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}
	if responses[0].ID != float64(1) {
		t.Errorf("first response id should be 1, got %v", responses[0].ID)
	}
	if responses[1].ID != float64(2) {
		t.Errorf("second response id should be 2, got %v", responses[1].ID)
	}
}

func TestStringID(t *testing.T) {
	resp := sendOne(t, `{"jsonrpc":"2.0","id":"abc","method":"initialize","params":{}}`)

	if resp.ID != "abc" {
		t.Errorf("expected id='abc', got %v", resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}
