package app

import (
	"os"
	"testing"
)

func TestLoadConfigMultiJumper(t *testing.T) {
	directory := t.TempDir()
	path := directory + "/config.yaml"
	data := []byte(`default: jumper-b
jumpers:
  jumper-a:
    ssh_user: alice
    ssh_host: a.example.com
    ssh_port: 2222
    identity_file: ~/.ssh/id_a
  jumper-b:
    ssh_user: bob
    ssh_host: b.example.com
    identity_file: ~/.ssh/id_b
`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	if err := loadConfig(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Default != "jumper-b" {
		t.Fatalf("Default = %q, want jumper-b", cfg.Default)
	}
	if len(cfg.Jumpers) != 2 {
		t.Fatalf("len(Jumpers) = %d, want 2", len(cfg.Jumpers))
	}
	a := cfg.Jumpers["jumper-a"]
	if a.SSHUser != "alice" || a.SSHHost != "a.example.com" || a.SSHPort != 2222 || a.IdentityFile != "~/.ssh/id_a" {
		t.Fatalf("jumper-a = %+v", a)
	}
	// 未写 ssh_port 的 jumper 沿用默认端口。
	b := cfg.Jumpers["jumper-b"]
	if b.SSHPort != 22 {
		t.Fatalf("jumper-b.SSHPort = %d, want 22", b.SSHPort)
	}
	if cfg.TimeoutSeconds != 30 {
		t.Fatalf("TimeoutSeconds = %d, want 30", cfg.TimeoutSeconds)
	}
}

func TestLoadConfigLegacyFlat(t *testing.T) {
	directory := t.TempDir()
	path := directory + "/config.yaml"
	data := []byte(`ssh_user: legacy-user
ssh_host: legacy.example.com
ssh_port: 2200
identity_file: ~/.ssh/id_legacy
timeout_seconds: 15
`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	if err := loadConfig(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Default != legacyJumperName {
		t.Fatalf("Default = %q, want %q", cfg.Default, legacyJumperName)
	}
	jumper, ok := cfg.Jumpers[legacyJumperName]
	if !ok {
		t.Fatalf("Jumpers 缺少 %q: %+v", legacyJumperName, cfg.Jumpers)
	}
	if jumper.SSHUser != "legacy-user" || jumper.SSHHost != "legacy.example.com" || jumper.SSHPort != 2200 || jumper.IdentityFile != "~/.ssh/id_legacy" {
		t.Fatalf("jumper = %+v", jumper)
	}
	if cfg.TimeoutSeconds != 15 {
		t.Fatalf("TimeoutSeconds = %d, want 15", cfg.TimeoutSeconds)
	}
	if cfg.HostArgReplaceDash != true {
		t.Fatalf("HostArgReplaceDash = %t, want true (default preserved)", cfg.HostArgReplaceDash)
	}
}

func TestSelectJumper(t *testing.T) {
	cfg := Config{
		Default: "jumper-b",
		Jumpers: map[string]Jumper{
			"jumper-a": {SSHHost: "a.example.com"},
			"jumper-b": {SSHHost: "b.example.com"},
		},
	}

	tests := []struct {
		name     string
		request  string
		want     string
		wantHost string
		wantErr  bool
	}{
		{name: "explicit", request: "jumper-a", want: "jumper-a", wantHost: "a.example.com"},
		{name: "default fallback", request: "", want: "jumper-b", wantHost: "b.example.com"},
		{name: "missing", request: "nope", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, jumper, err := cfg.selectJumper(test.request)
			if test.wantErr {
				if err == nil {
					t.Fatal("selectJumper() = nil error, want error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("name = %q, want %q", got, test.want)
			}
			if jumper.SSHHost != test.wantHost {
				t.Fatalf("jumper = %+v", jumper)
			}
		})
	}
}

func TestSelectJumperErrors(t *testing.T) {
	if _, _, err := (Config{}).selectJumper(""); err == nil {
		t.Fatal("no jumpers: want error")
	}

	single := Config{Jumpers: map[string]Jumper{"only": {}}}
	name, _, err := single.selectJumper("")
	if err != nil {
		t.Fatal(err)
	}
	if name != "only" {
		t.Fatalf("name = %q, want only", name)
	}

	multi := Config{
		Jumpers: map[string]Jumper{
			"jumper-a": {},
			"jumper-b": {},
		},
	}
	if _, _, err := multi.selectJumper(""); err == nil {
		t.Fatal("multi jumper without default: want error")
	}
}

func TestJumperTOTPEnvName(t *testing.T) {
	tests := map[string]string{
		"jumper-a": "GOTO_TOTP_SECRET_JUMPER_A",
		"prod.a":   "GOTO_TOTP_SECRET_PROD_A",
		"Prod-1":   "GOTO_TOTP_SECRET_PROD_1",
	}
	for name, want := range tests {
		if got := jumperTOTPEnvName(name); got != want {
			t.Fatalf("jumperTOTPEnvName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestLoadConfigTotpSecretPermissionCheck(t *testing.T) {
	directory := t.TempDir()
	path := directory + "/config.yaml"
	data := []byte(`jumpers:
  jumper-a:
    ssh_user: alice
    ssh_host: a.example.com
    identity_file: ~/.ssh/id_a
    totp_secret: JBSWY3DPEHPK3PXP
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	if err := loadConfig(path, &cfg); err == nil {
		t.Fatal("loose perms with totp_secret: want error")
	}
}

func TestValidateRejectsEmptyOptionPromptPattern(t *testing.T) {
	cfg := defaultConfig()
	cfg.OptionPromptPattern = ""
	cfg.Jumpers = map[string]Jumper{
		"jumper-a": {SSHUser: "alice", SSHHost: "a.example.com", SSHPort: 22, IdentityFile: "~/.ssh/id_a"},
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("empty option_prompt_pattern: want error")
	}
}
