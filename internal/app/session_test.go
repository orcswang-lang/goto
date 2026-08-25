package app

import (
	"os"
	"regexp"
	"testing"
)

func TestIsPasswordPrompt(t *testing.T) {
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
			if got := isPasswordPrompt(passwordRe, test.text); got != test.want {
				t.Fatalf("isPasswordPrompt() = %t, want %t", got, test.want)
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
