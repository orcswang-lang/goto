package session

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// fakeCreds 是可预置结果的凭据提供器。
type fakeCreds struct {
	password   string
	totpSecret string
}

var errNoCred = errors.New("no credential")

func (f fakeCreds) GetPassword(string) (string, error) {
	if f.password == "" {
		return "", errNoCred
	}
	return f.password, nil
}

func (f fakeCreds) GetTOTPSecret(string) (string, error) {
	if f.totpSecret == "" {
		return "", errNoCred
	}
	return f.totpSecret, nil
}

// fakeSSHScript 是模拟跳板机登录流程的 POSIX 脚本。
// 场景由 GOTO_FAKE_SCENARIO 环境变量选择；空闲时用 cat 阻塞直到 PTY 关闭。
const fakeSSHScript = `#!/bin/sh
case "${GOTO_FAKE_SCENARIO:-}" in
login)
  printf 'jump password: '; IFS= read -r pw
  printf 'OTP Code: '; IFS= read -r otp
  printf 'echo otp: %s\n' "$otp"
  printf 'Opt: '
  IFS= read -r target
  printf 'Connected to %s\n' "$target"
  exec cat
  ;;
auth-required)
  printf 'jump password: '
  exec cat
  ;;
host-key)
  printf 'Are you sure you want to continue connecting (yes/no)? '
  IFS= read -r line
  printf 'received: %s\n' "$line"
  exec cat
  ;;
silent)
  exec cat
  ;;
flood)
  printf '%010000d\n' 0 | tr '0' 'x'
  exec cat
  ;;
exit-fail)
  printf 'hello\n'
  exit 1
  ;;
host-key-fail)
  printf 'Host key verification failed.\n'
  exit 255
  ;;
password-late)
  printf 'welcome\n'
  sleep 1
  printf 'jump password: '
  IFS= read -r pw
  printf 'got pw: %s\n' "$pw"
  exec cat
  ;;
auth-inject-then-exit)
  printf 'jump password: '; IFS= read -r pw
  printf 'Permission denied, please try again.\n'
  exit 1
  ;;
esac
exit 0
`

func testOptions(scenario string, creds Credentials) Options {
	return Options{
		SSHCommand:     "",
		User:           "alice",
		Host:           "jump.example.com",
		Port:           22,
		IdentityFile:   "~/.ssh/id_test",
		Target:         "app-01",
		Jumper:         "prod",
		Credentials:    creds,
		PasswordPrompt: "(?i)(^|[^[:alnum:]_-])password:[[:space:]]*$",
		HostKeyPrompt:  "(?i)yes/no",
		OTPPrompt:      "OTP Code.*:",
		OptionPrompt:   "Opt",
		LoginTimeout:   5 * time.Second,
		IdleTimeout:    time.Hour,
		MaxLifetime:    time.Hour,
		MaxOutput:      64 * 1024,
	}
}

func openTestSession(t *testing.T, scenario string, creds Credentials) *Session {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("假 ssh 脚本依赖 POSIX shell")
	}
	script := filepath.Join(t.TempDir(), "fake-ssh")
	if err := os.WriteFile(script, []byte(fakeSSHScript), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOTO_FAKE_SCENARIO", scenario)
	opt := testOptions(scenario, creds)
	opt.SSHCommand = script
	s, err := Open(opt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// waitFor 轮询读取直到断言为真或超时；返回累计输出与最终状态。
func waitFor(t *testing.T, s *Session, timeout time.Duration, cond func(res ReadResult, total string) bool) (string, ReadResult) {
	t.Helper()
	var total string
	var last ReadResult
	deadline := time.Now().Add(timeout)
	for {
		last = s.Read(64*1024, 200*time.Millisecond)
		total += last.Chunk
		if cond(last, total) {
			return total, last
		}
		if last.State != StateConnecting && last.State != StateReady && len(total) > 0 {
			t.Fatalf("会话提前结束: state=%s code=%s msg=%s\n输出:\n%s", last.State, last.ErrorCode, last.ErrorMessage, total)
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待超时，state=%s\n输出:\n%s", last.State, total)
		}
	}
}

func TestLoginFlow(t *testing.T) {
	s := openTestSession(t, "login", fakeCreds{password: "t3stpass", totpSecret: "JBSWY3DPEHPK3PXP"})

	total, last := waitFor(t, s, 10*time.Second, func(res ReadResult, chunk string) bool {
		return res.State == StateReady && strings.Contains(chunk, "Connected to app-01")
	})
	if last.State != StateReady {
		t.Fatalf("state = %s, want ready", last.State)
	}
	if !strings.Contains(total, "echo otp: ********") {
		t.Fatalf("OTP 验证码未脱敏:\n%s", total)
	}
	if strings.Contains(total, "t3stpass") {
		t.Fatalf("密码泄漏到输出:\n%s", total)
	}

	s.Close()
	if got := s.State(); got != StateClosed {
		t.Fatalf("close 后 state = %s, want closed", got)
	}
}

func TestAuthRequired(t *testing.T) {
	s := openTestSession(t, "auth-required", fakeCreds{})
	total, last := waitFor(t, s, 10*time.Second, func(res ReadResult, _ string) bool {
		return res.State == StateFailed
	})
	if last.ErrorCode != "AUTH_REQUIRED" {
		t.Fatalf("code = %s, want AUTH_REQUIRED\n输出:\n%s", last.ErrorCode, total)
	}
}

func TestHostKeyRejected(t *testing.T) {
	s := openTestSession(t, "host-key", fakeCreds{password: "t3stpass"})
	total, last := waitFor(t, s, 10*time.Second, func(res ReadResult, _ string) bool {
		return res.State == StateFailed
	})
	if last.ErrorCode != "HOST_KEY_REQUIRED" {
		t.Fatalf("code = %s, want HOST_KEY_REQUIRED\n输出:\n%s", last.ErrorCode, total)
	}
	if !strings.Contains(total, "(yes/no)? no") {
		t.Fatalf("会话未发送 no 拒绝 host key 确认:\n%s", total)
	}
}

func TestLoginTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("假 ssh 脚本依赖 POSIX shell")
	}
	s := openTestSession(t, "silent", fakeCreds{})
	// 缩短登录超时以便快速失败。
	s.mu.Lock()
	s.opt.LoginTimeout = time.Second
	s.mu.Unlock()
	_, last := waitFor(t, s, 10*time.Second, func(res ReadResult, _ string) bool {
		return res.State == StateFailed
	})
	if last.ErrorCode != "TIMEOUT" {
		t.Fatalf("code = %s, want TIMEOUT", last.ErrorCode)
	}
}

func TestIdleExpire(t *testing.T) {
	s := openTestSession(t, "login", fakeCreds{password: "t3stpass", totpSecret: "JBSWY3DPEHPK3PXP"})
	waitFor(t, s, 10*time.Second, func(res ReadResult, chunk string) bool {
		return strings.Contains(chunk, "Connected to app-01")
	})
	// 缩短空闲超时，等待会话被回收。
	s.mu.Lock()
	s.opt.IdleTimeout = time.Second
	s.mu.Unlock()
	_, last := waitFor(t, s, 10*time.Second, func(res ReadResult, _ string) bool {
		return res.State == StateExpired
	})
	if last.ErrorCode != "SESSION_EXPIRED" {
		t.Fatalf("code = %s, want SESSION_EXPIRED", last.ErrorCode)
	}
}

func TestTruncation(t *testing.T) {
	s := openTestSession(t, "flood", fakeCreds{})
	s.mu.Lock()
	s.opt.MaxOutput = 256
	s.mu.Unlock()
	total, _ := waitFor(t, s, 10*time.Second, func(res ReadResult, chunk string) bool {
		return res.State == StateReady && len(chunk) > 0
	})
	if len(total) > 256+64 {
		t.Fatalf("输出 %d 字节超出缓冲上限 256", len(total))
	}
	res := s.Read(64*1024, 0)
	if !res.Truncated {
		t.Fatal("缓冲丢弃最旧内容后 Truncated 应为 true")
	}
}

func TestUpstreamExit(t *testing.T) {
	s := openTestSession(t, "exit-fail", fakeCreds{})
	_, last := waitFor(t, s, 10*time.Second, func(res ReadResult, _ string) bool {
		return res.State == StateFailed
	})
	if last.ErrorCode != "UPSTREAM_ERROR" {
		t.Fatalf("code = %s, want UPSTREAM_ERROR", last.ErrorCode)
	}
}

func TestHostKeyFailMappedToHostKeyRequired(t *testing.T) {
	s := openTestSession(t, "host-key-fail", fakeCreds{})
	total, last := waitFor(t, s, 10*time.Second, func(res ReadResult, _ string) bool {
		return res.State == StateFailed
	})
	if last.ErrorCode != "HOST_KEY_REQUIRED" {
		t.Fatalf("code = %s, want HOST_KEY_REQUIRED\n输出:\n%s", last.ErrorCode, total)
	}
}

func TestPromptOutsideLoginWindowNotAnswered(t *testing.T) {
	s := openTestSession(t, "password-late", fakeCreds{password: "t3stpass"})
	// 登录窗口 300ms：welcome 输出在窗口内，password 提示在 1s 后到达窗口外。
	s.mu.Lock()
	s.opt.LoginTimeout = 300 * time.Millisecond
	s.mu.Unlock()
	var total string
	deadline := time.Now().Add(5 * time.Second)
	for {
		res := s.Read(64*1024, 200*time.Millisecond)
		total += res.Chunk
		if strings.Contains(total, "jump password: ") || time.Now().After(deadline) {
			break
		}
	}
	time.Sleep(500 * time.Millisecond) // 若误注入，脚本会输出 got pw
	res := s.Read(64*1024, 0)
	total += res.Chunk
	if strings.Contains(total, "got pw") {
		t.Fatalf("登录窗口外的 password 提示不应注入凭据:\n%s", total)
	}
	if strings.Contains(total, "t3stpass") {
		t.Fatalf("密码泄漏到输出:\n%s", total)
	}
	if s.State() == StateFailed {
		t.Fatalf("窗口外提示不应导致会话失败:\n%s", total)
	}
}

func TestTerminalStateClearsSecrets(t *testing.T) {
	s := openTestSession(t, "auth-inject-then-exit", fakeCreds{password: "t3stpass"})
	_, last := waitFor(t, s, 10*time.Second, func(res ReadResult, _ string) bool {
		return res.State == StateFailed
	})
	if last.ErrorCode != "UPSTREAM_ERROR" {
		t.Fatalf("code = %s, want UPSTREAM_ERROR", last.ErrorCode)
	}
	s.mu.Lock()
	secrets := s.secrets
	s.mu.Unlock()
	if secrets != nil {
		t.Fatalf("终态后 secrets 未清空: %v", secrets)
	}
}

func TestCloseIdempotent(t *testing.T) {
	s := openTestSession(t, "silent", fakeCreds{})
	s.Close()
	s.Close()
	if got := s.State(); got != StateClosed {
		t.Fatalf("state = %s, want closed", got)
	}
}

func TestReadNextCursorIncreases(t *testing.T) {
	s := openTestSession(t, "login", fakeCreds{password: "t3stpass", totpSecret: "JBSWY3DPEHPK3PXP"})
	var total string
	var cursors []int
	deadline := time.Now().Add(10 * time.Second)
	for {
		res := s.Read(5, 200*time.Millisecond)
		total += res.Chunk
		if res.Chunk != "" {
			cursors = append(cursors, res.NextCursor)
		}
		if strings.Contains(total, "Connected to app-01") && len(cursors) >= 2 {
			break
		}
		if res.State != StateConnecting && res.State != StateReady {
			t.Fatalf("state = %s code=%s\n输出:\n%s", res.State, res.ErrorCode, total)
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待超时\n输出:\n%s", total)
		}
	}
	for i := 1; i < len(cursors); i++ {
		if cursors[i] <= cursors[i-1] {
			t.Fatalf("游标未递增: %v", cursors)
		}
	}
}

func TestOpenRejectsIncompleteOptions(t *testing.T) {
	if _, err := Open(Options{}); err == nil {
		t.Fatal("空参数: want error")
	}
	opt := testOptions("login", fakeCreds{})
	opt.Credentials = nil
	if _, err := Open(opt); err == nil {
		t.Fatal("无凭据提供器: want error")
	}
	opt = testOptions("login", fakeCreds{})
	opt.PasswordPrompt = "["
	if _, err := Open(opt); err == nil {
		t.Fatal("非法正则: want error")
	}
}

func TestAppendVisibleTruncationKeepsUTF8(t *testing.T) {
	s := &Session{opt: Options{MaxOutput: 7}}
	s.mu.Lock()
	s.appendVisibleLocked([]byte("你好世界你好世界"))
	s.mu.Unlock()
	if !utf8.ValidString(s.visible) {
		t.Fatalf("截断产生非法 UTF-8: %q", s.visible)
	}
	if len(s.visible) > 7+3 {
		t.Fatalf("缓冲 %d 字节，超出上限允许值", len(s.visible))
	}
	if s.base == 0 {
		t.Fatal("应发生截断")
	}
}

func TestManagerLifecycle(t *testing.T) {
	m := NewManager()
	s := openTestSession(t, "silent", fakeCreds{})
	m.Add(s)
	got, ok := m.Get(s.ID())
	if !ok || got != s {
		t.Fatal("Get 未找到会话")
	}
	m.CloseAll()
	if s.State() != StateClosed {
		t.Fatalf("CloseAll 后 state = %s, want closed", s.State())
	}
}
