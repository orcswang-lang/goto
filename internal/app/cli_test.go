package app

import (
	"bufio"
	"bytes"
	"flag"
	"reflect"
	"strings"
	"testing"
)

func TestReorderArgs(t *testing.T) {
	newFlagSet := func() *flag.FlagSet {
		fs := flag.NewFlagSet("goto", flag.ContinueOnError)
		fs.String("jumper", "", "")
		fs.String("config", "", "")
		fs.String("host", "", "")
		fs.Bool("list", false, "")
		return fs
	}

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "flags after target",
			args: []string{"goto", "app-01", "-jumper", "prod", "-config", "/tmp/c.yaml"},
			want: []string{"goto", "-jumper", "prod", "-config", "/tmp/c.yaml", "app-01"},
		},
		{
			name: "flags before target",
			args: []string{"goto", "-jumper", "prod", "app-01"},
			want: []string{"goto", "-jumper", "prod", "app-01"},
		},
		{
			name: "equals form",
			args: []string{"goto", "app-01", "-jumper=prod"},
			want: []string{"goto", "-jumper=prod", "app-01"},
		},
		{
			name: "bool flag needs no value",
			args: []string{"goto", "app-01", "-list"},
			want: []string{"goto", "-list", "app-01"},
		},
		{
			name: "flag value consumed verbatim",
			args: []string{"goto", "-host", "-weird-host"},
			want: []string{"goto", "-host", "-weird-host"},
		},
		{
			name: "double dash terminates flags",
			args: []string{"goto", "app-01", "--", "-jumper", "prod"},
			want: []string{"goto", "app-01", "--", "-jumper", "prod"},
		},
		{
			name: "unknown flag kept for flag.Parse to reject",
			args: []string{"goto", "app-01", "-jummper", "prod"},
			want: []string{"goto", "-jummper", "app-01", "prod"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := reorderArgs(newFlagSet(), test.args)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("reorderArgs(%v) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}

func TestPromptSelectJumper(t *testing.T) {
	cfg := Config{
		Default: "jumper-b",
		Jumpers: map[string]Jumper{
			"jumper-a": {SSHHost: "a.example.com"},
			"jumper-b": {SSHHost: "b.example.com"},
		},
	}

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "by number", input: "1\n", want: "jumper-a"},
		{name: "by name", input: "jumper-b\n", want: "jumper-b"},
		{name: "empty picks default", input: "\n", want: "jumper-b"},
		{name: "invalid then retry", input: "x\ny\n2\n", want: "jumper-b"},
		{name: "eof", input: "", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			got, err := promptSelectJumper(cfg, bufio.NewReader(strings.NewReader(test.input)), &out)
			if test.wantErr {
				if err == nil {
					t.Fatal("promptSelectJumper() = nil error, want error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("promptSelectJumper() = %q, want %q; output:\n%s", got, test.want, out.String())
			}
		})
	}
}

func TestPromptSelectJumperNoDefaultEmptyRetries(t *testing.T) {
	cfg := Config{
		Jumpers: map[string]Jumper{
			"jumper-a": {SSHHost: "a.example.com"},
		},
	}
	var out bytes.Buffer
	got, err := promptSelectJumper(cfg, bufio.NewReader(strings.NewReader("\n1\n")), &out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "jumper-a" {
		t.Fatalf("promptSelectJumper() = %q, want jumper-a", got)
	}
	if !strings.Contains(out.String(), "无效选择") && !strings.Contains(out.String(), "输入编号") {
		t.Fatalf("expected re-prompt after empty choice, output:\n%s", out.String())
	}
}
