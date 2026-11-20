//go:build darwin

package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDarwinCommandTimeout 验证凭据命令超时：keyring 阻塞时返回 ErrNotFound 而非无限等待。
func TestDarwinCommandTimeout(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "security")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 5\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	o := &OS{timeout: 200 * time.Millisecond}
	start := time.Now()
	if _, err := o.GetPassword("prod"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("超时未生效，耗时 %v", elapsed)
	}
}
