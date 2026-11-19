package termutil

import (
	"regexp"
	"testing"
)

func TestPromptMatchersOnlyCheckCurrentOutputLine(t *testing.T) {
	promptRe := regexp.MustCompile("(?i)yes/no|otp code.*:")

	if CurrentLineMatches(promptRe, "Are you sure you want to continue (yes/no)?\r\napplication output") {
		t.Fatal("historical prompt text must not match")
	}
	if !CurrentLineMatches(promptRe, "application output\r\nOTP Code: ") {
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
		{name: "single line", text: "abc", want: "abc"},
		{name: "trailing newline yields empty", text: "abc\n", want: ""},
		{name: "empty", text: "", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CurrentOutputLine(test.text); got != test.want {
				t.Fatalf("CurrentOutputLine(%q) = %q, want %q", test.text, got, test.want)
			}
		})
	}
}

func TestStripControlSequences(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "ansi color", text: "\x1b[31mpassword:\x1b[0m", want: "password:"},
		{name: "clear line", text: "password:\x1b[K", want: "password:"},
		{name: "terminal title", text: "\x1b]0;session\apassword:", want: "password:"},
		{name: "plain text unchanged", text: "plain text", want: "plain text"},
		{name: "CSI cursor move", text: "a\x1b[2Db", want: "ab"},
		{name: "OSC title terminated by escape backslash", text: "\x1b]0;title\x1b\\body", want: "body"},
		{name: "other escape pair", text: "a\x1bMb", want: "ab"},
		{name: "lone escape at end dropped", text: "a\x1b", want: "a"},
		{name: "multibyte preserved", text: "中文\x1b[32mok", want: "中文ok"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := StripControlSequences(test.text); got != test.want {
				t.Fatalf("StripControlSequences(%q) = %q, want %q", test.text, got, test.want)
			}
		})
	}
}
