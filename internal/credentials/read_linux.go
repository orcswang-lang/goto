//go:build linux

package credentials

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// readFromOS 通过 Secret Service 读取条目。
// 存储方式：secret-tool store --label='goto <account>' service <service> account <account>
// 命令带超时：keyring 锁定需交互解锁时返回 ErrNotFound 而不是无限阻塞。
func readFromOS(service, account string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "secret-tool", "lookup", "service", service, "account", account).Output()
	if err != nil {
		// secret-tool 不存在、条目不存在、超时或 keyring 未解锁均视为未找到。
		return "", ErrNotFound
	}
	return strings.TrimRight(string(out), "\n"), nil
}
