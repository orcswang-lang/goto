package app

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestPasswordPromptMatching(t *testing.T) {
	passwordRe := regexp.MustCompile(defaultPasswordPrompt)

	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "environment variable is not a prompt",
			text: "POSTGRES_PASSWORD: ${STARLINK_POSTGRES_PASSWORD: ?STARLINK_POSTGRES_PASSWORD is required}",
			want: false,
		},
		{
			name: "hyphenated environment variable is not a prompt",
			text: "POSTGRES-PASSWORD: ${STARLINK-POSTGRES-PASSWORD: ?STARLINK-POSTGRES-PASSWORD is required}",
			want: false,
		},
		{
			name: "ssh password prompt",
			text: "user@jumper.example.com's password: ",
			want: true,
		},
		{
			name: "colored ssh password prompt",
			text: "\x1b[33muser@jumper.example.com's password:\x1b[0m",
			want: true,
		},
		{
			name: "password prompt ending in carriage return",
			text: "user@jumper.example.com's password:\r",
			want: true,
		},
		{
			name: "only current line is checked",
			text: "user@jumper.example.com's password:\r\napplication output",
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isCurrentLineMatch(passwordRe, test.text); got != test.want {
				t.Fatalf("isCurrentLineMatch() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestPromptMatchersOnlyCheckCurrentOutputLine(t *testing.T) {
	promptRe := regexp.MustCompile("(?i)yes/no|otp code.*:")

	if isCurrentLineMatch(promptRe, "Are you sure you want to continue (yes/no)?\r\napplication output") {
		t.Fatal("historical prompt text must not match")
	}
	if !isCurrentLineMatch(promptRe, "application output\r\nOTP Code: ") {
		t.Fatal("current prompt text must match")
	}
}

func TestCurrentOutputLine(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "no line break", text: "password: ", want: "password: "},
		{name: "line feed", text: "previous\npassword: ", want: "password: "},
		{name: "carriage return", text: "previous\rpassword: ", want: "password: "},
		{name: "trailing carriage return", text: "password: \r", want: "password: "},
		{name: "carriage return line feed", text: "previous\r\npassword: ", want: "password: "},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := currentOutputLine(test.text); got != test.want {
				t.Fatalf("currentOutputLine() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestStripTerminalControlSequences(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "ansi color", text: "\x1b[31mpassword:\x1b[0m", want: "password:"},
		{name: "clear line", text: "password:\x1b[K", want: "password:"},
		{name: "terminal title", text: "\x1b]0;session\apassword:", want: "password:"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := stripTerminalControlSequences(test.text); got != test.want {
				t.Fatalf("stripTerminalControlSequences() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSshArgs(t *testing.T) {
	jumper := Jumper{
		SSHUser:      "alice",
		SSHHost:      "jumper.example.com",
		SSHPort:      2222,
		IdentityFile: "~/.ssh/id_a",
	}
	want := []string{
		"-o", "IdentitiesOnly=yes",
		"-i", "~/.ssh/id_a",
		"-p", "2222",
		"alice@jumper.example.com",
	}
	got := sshArgs(jumper)
	if len(got) != len(want) {
		t.Fatalf("sshArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sshArgs() = %v, want %v", got, want)
		}
	}
}

func TestLoadConfigMigratesLegacyPasswordPrompt(t *testing.T) {
	directory := t.TempDir()
	path := directory + "/config.yaml"
	data := []byte("password_prompt: '(?i)password:'\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	if err := loadConfig(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.PasswordPrompt != defaultPasswordPrompt {
		t.Fatalf("PasswordPrompt = %q, want %q", cfg.PasswordPrompt, defaultPasswordPrompt)
	}
}

func TestSessionStateSawOutput(t *testing.T) {
	s := &sessionState{}
	if s.sawAnyOutput() {
		t.Fatal("sawAnyOutput() = true before any output")
	}
	s.markOutput()
	if !s.sawAnyOutput() {
		t.Fatal("sawAnyOutput() = false after markOutput")
	}
}

func TestAppendWindowTrimsAtLineBoundary(t *testing.T) {
	var window bytes.Buffer
	window.WriteString(strings.Repeat("x", 2500)) // 无换行的长前缀
	window.WriteString("HEADER\nTAIL content")
	appendWindow(&window, []byte(strings.Repeat("y", 2000)))

	want := "TAIL content" + strings.Repeat("y", 2000)
	if got := window.String(); got != want {
		t.Fatalf("window = %q (len %d), want %q (len %d)", got, len(got), want, len(want))
	}
}

func TestAppendWindowUnderLimitKeepsAll(t *testing.T) {
	var window bytes.Buffer
	appendWindow(&window, []byte("small"))
	if got := window.String(); got != "small" {
		t.Fatalf("window = %q, want small", got)
	}
}

func TestFeedPassword(t *testing.T) {
	s := &sessionState{}

	// 非等待模式：输入透传（consumed=false，调用方使用原始输入）。
	consumed, remaining := s.feedPassword([]byte("ls\r"))
	if consumed {
		t.Fatal("非等待模式下输入不应被消费")
	}
	if remaining != nil {
		t.Fatalf("非等待模式 remaining = %q, want nil", remaining)
	}

	// 等待模式：字节累积，\r 完成密码。
	ch := s.beginPasswordRead()
	if consumed, _ = s.feedPassword([]byte("hun")); !consumed {
		t.Fatal("等待模式下输入应被消费")
	}
	consumed, remaining = s.feedPassword([]byte("ter2\r"))
	if !consumed {
		t.Fatal("等待模式下输入应被消费")
	}
	select {
	case pw := <-ch:
		if pw != "hunter2" {
			t.Fatalf("密码 = %q, want hunter2", pw)
		}
	default:
		t.Fatal("密码未在回车后完成")
	}

	// 完成后输入恢复透传。
	if consumed, _ = s.feedPassword([]byte("x")); consumed {
		t.Fatal("密码完成后输入不应再被消费")
	}
}

func TestFeedPasswordNewlineAndRemaining(t *testing.T) {
	s := &sessionState{}
	ch := s.beginPasswordRead()
	// \n 同样结束密码；回车之后同批剩余字节透传。
	consumed, remaining := s.feedPassword([]byte("pw\nnext"))
	if !consumed {
		t.Fatal("输入应被消费")
	}
	if got := <-ch; got != "pw" {
		t.Fatalf("密码 = %q, want pw", got)
	}
	if string(remaining) != "next" {
		t.Fatalf("remaining = %q, want next", remaining)
	}
}
