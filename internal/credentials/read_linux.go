//go:build linux

package credentials

import (
	"os/exec"
	"strings"
)

// readFromOS 通过 Secret Service 读取条目。
// 存储方式：secret-tool store --label='goto <account>' service <service> account <account>
func readFromOS(service, account string) (string, error) {
	out, err := exec.Command("secret-tool", "lookup", "service", service, "account", account).Output()
	if err != nil {
		// secret-tool 不存在、条目不存在或 keyring 未解锁均视为未找到。
		return "", ErrNotFound
	}
	return strings.TrimRight(string(out), "\n"), nil
}
