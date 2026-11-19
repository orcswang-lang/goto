package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type stubHandler struct {
	requests      []Request
	notifications []string
	response      Response
}

func (h *stubHandler) HandleRequest(req Request) Response {
	h.requests = append(h.requests, req)
	return h.response
}

func (h *stubHandler) HandleNotification(method string, params json.RawMessage) {
	h.notifications = append(h.notifications, method)
}

func TestServeRoundTrip(t *testing.T) {
	h := &stubHandler{response: Result(json.RawMessage("1"), map[string]any{"ok": true})}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}
`)
	var out bytes.Buffer
	if err := Serve(in, &out, h); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 1 || h.requests[0].Method != "ping" {
		t.Fatalf("requests = %+v", h.requests)
	}
	var resp Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("output not JSON: %v\n%s", err, out.String())
	}
	if string(resp.ID) != "1" {
		t.Fatalf("id = %s, want 1", resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Result.(map[string]any)["ok"] != true {
		t.Fatalf("result = %+v", resp.Result)
	}
}

func TestServeNotificationNoResponse(t *testing.T) {
	h := &stubHandler{}
	in := strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}
`)
	var out bytes.Buffer
	if err := Serve(in, &out, h); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("notification produced output: %s", out.String())
	}
	if len(h.notifications) != 1 || h.notifications[0] != "notifications/initialized" {
		t.Fatalf("notifications = %+v", h.notifications)
	}
}

func TestServeExplicitNullIDIsNotification(t *testing.T) {
	h := &stubHandler{}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":null,"method":"notifications/cancelled"}
`)
	var out bytes.Buffer
	if err := Serve(in, &out, h); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("null-id notification produced output: %s", out.String())
	}
}

func TestServeParseError(t *testing.T) {
	h := &stubHandler{}
	in := strings.NewReader("not json\n")
	var out bytes.Buffer
	if err := Serve(in, &out, h); err != nil {
		t.Fatal(err)
	}
	var resp Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil || resp.Error.Code != CodeParseError {
		t.Fatalf("response = %+v", resp)
	}
}

func TestServeMultipleLines(t *testing.T) {
	h := &stubHandler{response: Result(json.RawMessage("7"), "ok")}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"a"}
{"jsonrpc":"2.0","id":8,"method":"b"}
`)
	var out bytes.Buffer
	if err := Serve(in, &out, h); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("output lines = %d, want 2: %s", len(lines), out.String())
	}
	if len(h.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(h.requests))
	}
}
