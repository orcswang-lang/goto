package policy

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/agent-policy.yaml"
	data := []byte("version: 1\nallowed_jumpers: [prod]\ntarget_patterns: [\"^app-[0-9]+$\"]\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.IdleTimeoutSeconds != 900 || p.MaxSessionSeconds != 1800 || p.MaxOutputBytes != 65536 {
		t.Fatalf("defaults not applied: %+v", p)
	}
	if p.CredentialProvider != "os" || p.HostKeyPolicy != "known_hosts_only" || p.AllowSessionInput {
		t.Fatalf("v1 fixed fields wrong: %+v", p)
	}
	if !p.JumperAllowed("prod") || p.JumperAllowed("staging") {
		t.Fatal("JumperAllowed wrong")
	}
	if !p.TargetAllowed("app-01") || p.TargetAllowed("db-01") {
		t.Fatal("TargetAllowed wrong")
	}
}

func TestLoadForcesV1FixedFields(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/agent-policy.yaml"
	data := []byte(`version: 1
allowed_jumpers: [prod]
allow_session_input: true
host_key_policy: accept_any
credential_provider: env
`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// 策略文件无法放开 v1 固定边界。
	if p.AllowSessionInput || p.HostKeyPolicy != "known_hosts_only" || p.CredentialProvider != "os" {
		t.Fatalf("fixed fields were not forced: %+v", p)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load(t.TempDir() + "/missing.yaml"); err == nil {
		t.Fatal("missing file: want error")
	}

	dir := t.TempDir()
	for _, test := range []struct {
		name string
		data string
	}{
		{name: "bad regexp", data: "version: 1\ntarget_patterns: [\"[\"]\n"},
		{name: "bad version", data: "version: 2\n"},
		{name: "negative timeout", data: "version: 1\nidle_timeout_seconds: -1\n"},
		{name: "session shorter than idle", data: "version: 1\nidle_timeout_seconds: 100\nmax_session_seconds: 10\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := dir + "/" + test.name + ".yaml"
			if err := os.WriteFile(path, []byte(test.data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestTargetAllowedEmptySemantics(t *testing.T) {
	p := Defaults()
	if !p.TargetAllowed("") {
		t.Fatal("empty target (login only) must be allowed")
	}
	if p.TargetAllowed("app-01") {
		t.Fatal("empty patterns must reject every non-empty target")
	}
}

func TestWriteExampleExpandsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := WriteExample("~/agent-policy.yaml"); err != nil {
		t.Fatal(err)
	}
	p, err := Load("~/agent-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !p.JumperAllowed("prod-readonly") {
		t.Fatal("示例策略未写入展开后的路径")
	}
}

func TestWriteExampleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/agent-policy.yaml"
	if err := WriteExample(path); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !p.JumperAllowed("prod-readonly") {
		t.Fatal("example jumper must be allowed")
	}
	if !p.TargetAllowed("app.prod.01") {
		t.Fatal("example target must be allowed")
	}
}
