package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"goto/internal/credentials"
	"goto/internal/mcp"
)

// fakeCreds 实现 credentials.Provider。
type fakeCreds struct {
	password   string
	totpSecret string
}

func (f fakeCreds) GetPassword(jumper string) (string, error) {
	if jumper == "prod" && f.password != "" {
		return f.password, nil
	}
	return "", credentials.ErrNotFound
}

func (f fakeCreds) GetTOTPSecret(jumper string) (string, error) {
	if jumper == "prod" && f.totpSecret != "" {
		return f.totpSecret, nil
	}
	return "", credentials.ErrNotFound
}

const testCfgYAML = `default: prod
jumpers:
  prod:
    ssh_user: alice
    ssh_host: jump.example.com
    ssh_port: 22
    identity_file: ~/.ssh/id_test
  staging:
    ssh_user: bob
    ssh_host: jump2.example.com
    ssh_port: 2200
    identity_file: ~/.ssh/id_test2
host_arg_replace_dash: true
timeout_seconds: 30
`

func writeTestCfg(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(testCfgYAML), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestServer(t *testing.T, sshCommand string) *Server {
	t.Helper()
	policyPath := filepath.Join(t.TempDir(), "agent-policy.yaml")
	data := []byte("version: 1\nallowed_jumpers: [prod]\ntarget_patterns: [\"^app\\\\.[0-9]+$\"]\n")
	if err := os.WriteFile(policyPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(writeTestCfg(t), policyPath, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if sshCommand != "ssh" {
		s.SSHCommand = sshCommand
	}
	return s
}

func TestNewRejectsMissingPolicy(t *testing.T) {
	if _, err := New(writeTestCfg(t), filepath.Join(t.TempDir(), "missing.yaml"), "test", nil); err == nil {
		t.Fatal("缺策略文件: want error")
	}
}

func TestInitializeHandshake(t *testing.T) {
	s := newTestServer(t, "ssh")
	resp := s.HandleRequest(mcp.Request{ID: json.RawMessage("1"), Method: "initialize", Params: json.RawMessage(`{"protocolVersion":"2025-03-26"}`)})
	result := resp.Result.(map[string]any)
	if result["protocolVersion"] != "2025-03-26" {
		t.Fatalf("protocolVersion = %v, want 2025-03-26", result["protocolVersion"])
	}
	info := result["serverInfo"].(map[string]any)
	if info["name"] != "goto-mcp" {
		t.Fatalf("serverInfo = %v", info)
	}

	resp = s.HandleRequest(mcp.Request{ID: json.RawMessage("2"), Method: "initialize", Params: json.RawMessage(`{"protocolVersion":"2099-01-01"}`)})
	result = resp.Result.(map[string]any)
	if result["protocolVersion"] != "2025-11-25" {
		t.Fatalf("未知客户端版本应回显最新支持版本, got %v", result["protocolVersion"])
	}
}

func TestToolsListExposesFiveTools(t *testing.T) {
	s := newTestServer(t, "ssh")
	resp := s.HandleRequest(mcp.Request{ID: json.RawMessage("1"), Method: "tools/list"})
	result := resp.Result.(map[string]any)
	tools := result["tools"].([]tool)
	names := make(map[string]bool)
	for _, t := range tools {
		names[t.Name] = true
	}
	for _, want := range []string{
		"goto_list_jumpers",
		"goto_resolve_target",
		"goto_session_open",
		"goto_session_read",
		"goto_session_close",
	} {
		if !names[want] {
			t.Fatalf("工具列表缺少 %s: %v", want, names)
		}
	}
	if len(tools) != 5 {
		t.Fatalf("工具数 = %d, want 5（v1 不允许会话输入等额外能力）", len(tools))
	}
	// 所有 inputSchema 不得包含凭据字段。
	for _, tl := range tools {
		data, _ := json.Marshal(tl.InputSchema)
		for _, banned := range []string{"password", "totp_secret", "private_key", "secret"} {
			if strings.Contains(strings.ToLower(string(data)), banned) {
				t.Fatalf("工具 %s 的 schema 包含敏感字段 %s: %s", tl.Name, banned, data)
			}
		}
	}
}

func callTool(t *testing.T, s *Server, name string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	params := map[string]any{"name": name, "arguments": args}
	data, _ := json.Marshal(params)
	resp := s.HandleRequest(mcp.Request{ID: json.RawMessage("1"), Method: "tools/call", Params: data})
	result := resp.Result.(map[string]any)
	content := result["content"].([]map[string]any)
	var payload map[string]any
	if err := json.Unmarshal([]byte(content[0]["text"].(string)), &payload); err != nil {
		t.Fatalf("工具结果不是 JSON: %v", err)
	}
	return payload, result["isError"].(bool)
}

func TestListJumpers(t *testing.T) {
	s := newTestServer(t, "ssh")
	s.Credentials = fakeCreds{password: "pw", totpSecret: "JBSWY3DPEHPK3PXP"}
	payload, isErr := callTool(t, s, "goto_list_jumpers", nil)
	if isErr {
		t.Fatalf("list_jumpers 出错: %+v", payload)
	}
	jumpers := payload["jumpers"].([]any)
	if len(jumpers) != 2 {
		t.Fatalf("jumpers = %v", jumpers)
	}
	prod := jumpers[0].(map[string]any)
	if prod["name"] != "prod" || prod["is_default"] != true {
		t.Fatalf("prod = %+v", prod)
	}
	if prod["password_available"] != true || prod["totp_available"] != true {
		t.Fatalf("凭据可用性错误: %+v", prod)
	}
	staging := jumpers[1].(map[string]any)
	if staging["password_available"] != false || staging["totp_available"] != false {
		t.Fatalf("凭据缺失时可用性应为 false: %+v", staging)
	}
	for _, j := range jumpers {
		m := j.(map[string]any)
		for _, banned := range []string{"ssh_user", "ssh_host", "identity_file"} {
			if _, ok := m[banned]; ok {
				t.Fatalf("list_jumpers 泄漏字段 %s", banned)
			}
		}
	}
}

func TestResolveTarget(t *testing.T) {
	s := newTestServer(t, "ssh")
	payload, isErr := callTool(t, s, "goto_resolve_target", map[string]any{"jumper": "prod", "target": "app-01"})
	if isErr {
		t.Fatalf("resolve_target 出错: %+v", payload)
	}
	if payload["normalized_target"] != "app.01" {
		t.Fatalf("normalized_target = %v, want app.01", payload["normalized_target"])
	}
	if payload["target_allowed"] != true {
		t.Fatalf("target_allowed = %v, want true", payload["target_allowed"])
	}

	payload, _ = callTool(t, s, "goto_resolve_target", map[string]any{"jumper": "prod", "target": "db-01"})
	if payload["target_allowed"] != false {
		t.Fatalf("db-01 应被策略拒绝: %+v", payload)
	}

	_, isErr = callTool(t, s, "goto_resolve_target", map[string]any{"jumper": "nope", "target": "app-01"})
	if !isErr {
		t.Fatal("未知 jumper: want error")
	}
}

func TestSessionOpenPolicyDenied(t *testing.T) {
	s := newTestServer(t, "ssh")
	payload, isErr := callTool(t, s, "goto_session_open", map[string]any{"jumper": "staging"})
	if !isErr || payload["code"] != "POLICY_DENIED" {
		t.Fatalf("策略外 jumper 应 POLICY_DENIED: %+v err=%v", payload, isErr)
	}
	payload, isErr = callTool(t, s, "goto_session_open", map[string]any{"jumper": "prod", "target": "db-01"})
	if !isErr || payload["code"] != "TARGET_NOT_ALLOWED" {
		t.Fatalf("策略外 target 应 TARGET_NOT_ALLOWED: %+v err=%v", payload, isErr)
	}
}

func TestSessionOpenReadClose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("假 ssh 脚本依赖 POSIX shell")
	}
	script := filepath.Join(t.TempDir(), "fake-ssh")
	scriptBody := `#!/bin/sh
printf 'jump password: '; IFS= read -r pw
printf 'hello from agent test\n'
exec cat
`
	if err := os.WriteFile(script, []byte(scriptBody), 0755); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, script)
	s.Credentials = fakeCreds{password: "t3stpass"}

	payload, isErr := callTool(t, s, "goto_session_open", map[string]any{"jumper": "prod", "target": "app-01", "purpose": "测试"})
	if isErr {
		t.Fatalf("session_open 出错: %+v", payload)
	}
	sessionID, _ := payload["session_id"].(string)
	if sessionID == "" {
		t.Fatal("session_id 为空")
	}
	if payload["state"] != "connecting" {
		t.Fatalf("state = %v, want connecting", payload["state"])
	}
	caps := payload["capabilities"].([]any)
	if len(caps) != 2 || caps[0] != "read" || caps[1] != "close" {
		t.Fatalf("capabilities = %v, want [read close]", caps)
	}

	var total string
	deadline := time.Now().Add(10 * time.Second)
	for {
		res, isErr := callTool(t, s, "goto_session_read", map[string]any{"session_id": sessionID, "max_bytes": 65536, "wait_ms": 200})
		if isErr {
			t.Fatalf("session_read 出错: %+v", res)
		}
		if events, ok := res["events"].([]any); ok && len(events) > 0 {
			total += events[0].(map[string]any)["text"].(string)
		}
		if strings.Contains(total, "hello from agent test") {
			break
		}
		if st := res["state"].(string); st == "failed" || st == "expired" || st == "closed" {
			t.Fatalf("会话提前结束: %+v", res)
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待输出超时\n%s", total)
		}
	}

	res, isErr := callTool(t, s, "goto_session_read", map[string]any{"session_id": sessionID, "max_bytes": 65536, "wait_ms": 100})
	if isErr {
		t.Fatalf("session_read 出错: %+v", res)
	}
	next, _ := res["next_cursor"].(float64)
	if next <= 0 {
		t.Fatalf("next_cursor = %v, want > 0", next)
	}

	closeRes, isErr := callTool(t, s, "goto_session_close", map[string]any{"session_id": sessionID})
	if isErr {
		t.Fatalf("session_close 出错: %+v", closeRes)
	}
	if closeRes["state"] != "closed" {
		t.Fatalf("state = %v, want closed", closeRes["state"])
	}
	// 幂等：重复关闭成功。
	if _, isErr = callTool(t, s, "goto_session_close", map[string]any{"session_id": sessionID}); isErr {
		t.Fatal("重复关闭应幂等成功")
	}
	// 不存在的会话。
	missing, isErr := callTool(t, s, "goto_session_read", map[string]any{"session_id": "nope"})
	if !isErr || missing["code"] != "SESSION_NOT_FOUND" {
		t.Fatalf("不存在会话应 SESSION_NOT_FOUND: %+v err=%v", missing, isErr)
	}
	// 关闭后的会话已从 Manager 移除，再次读取同样返回 SESSION_NOT_FOUND。
	missing, isErr = callTool(t, s, "goto_session_read", map[string]any{"session_id": sessionID})
	if !isErr || missing["code"] != "SESSION_NOT_FOUND" {
		t.Fatalf("关闭后的会话应已移除: %+v err=%v", missing, isErr)
	}
	if _, ok := s.Sessions.Get(sessionID); ok {
		t.Fatal("Manager 中仍保留已关闭会话")
	}
}

func TestMaxBytesSchemaFollowsPolicy(t *testing.T) {
	s := newTestServer(t, "ssh")
	s.Policy.MaxOutputBytes = 10000
	var found bool
	for _, tl := range s.tools() {
		if tl.Name != "goto_session_read" {
			continue
		}
		found = true
		props := tl.InputSchema["properties"].(map[string]any)
		maxBytes := props["max_bytes"].(map[string]any)
		if got := maxBytes["maximum"].(int); got != 10000 {
			t.Fatalf("max_bytes maximum = %v, want 10000", got)
		}
	}
	if !found {
		t.Fatal("未找到 goto_session_read 工具")
	}
}

func TestInvalidArguments(t *testing.T) {
	s := newTestServer(t, "ssh")

	cases := []struct {
		name   string
		tool   string
		args   map[string]any
		expect string
	}{
		{"缺 jumper", "goto_session_open", map[string]any{"target": "app-01"}, "INVALID_ARGUMENT"},
		{"max_bytes 越界", "goto_session_read", map[string]any{"session_id": "x", "max_bytes": 999999}, "INVALID_ARGUMENT"},
		{"wait_ms 负值", "goto_session_read", map[string]any{"session_id": "x", "wait_ms": -1}, "INVALID_ARGUMENT"},
		{"cursor 非整数", "goto_session_read", map[string]any{"session_id": "x", "cursor": 1.5}, "INVALID_ARGUMENT"},
		{"resolve 目标含回车", "goto_resolve_target", map[string]any{"jumper": "prod", "target": "app-01\rwhoami"}, "INVALID_ARGUMENT"},
		{"open 目标含换行", "goto_session_open", map[string]any{"jumper": "prod", "target": "app\n01"}, "INVALID_ARGUMENT"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payload, isErr := callTool(t, s, c.tool, c.args)
			if !isErr || payload["code"] != c.expect {
				t.Fatalf("code = %v err=%v, want %s", payload["code"], isErr, c.expect)
			}
		})
	}

	unknown, isErr := callTool(t, s, "goto_shell_exec", nil)
	if !isErr || unknown["code"] != "INVALID_ARGUMENT" {
		t.Fatalf("未知工具应报错: %+v err=%v", unknown, isErr)
	}
}

func TestReadMissingSessionReturnsImmediately(t *testing.T) {
	s := newTestServer(t, "ssh")
	start := time.Now()
	payload, isErr := callTool(t, s, "goto_session_read", map[string]any{"session_id": "missing"})
	if !isErr {
		t.Fatal("missing session: want error")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("missing session 应立即返回: %v", payload)
	}
}
