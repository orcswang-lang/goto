//go:build darwin

package credentials

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// readFromOS 通过 macOS Keychain 读取条目。
// 存储方式：security add-generic-password -s <service> -a <account> -w <secret>
// 命令带超时：keyring 锁定需交互解锁时返回 ErrNotFound 而不是无限阻塞。
func readFromOS(service, account string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", service, "-a", account, "-w").Output()
	if err != nil {
		// security 不存在、条目不存在、超时或用户拒绝访问均视为未找到。
		return "", ErrNotFound
	}
	return strings.TrimRight(string(out), "\n"), nil
}
