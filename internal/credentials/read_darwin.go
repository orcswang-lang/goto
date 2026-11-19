//go:build darwin

package credentials

import (
	"os/exec"
	"strings"
)

// readFromOS 通过 macOS Keychain 读取条目。
// 存储方式：security add-generic-password -s <service> -a <account> -w <secret>
func readFromOS(service, account string) (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", service, "-a", account, "-w").Output()
	if err != nil {
		// security 不存在、条目不存在或用户拒绝访问均视为未找到。
		return "", ErrNotFound
	}
	return strings.TrimRight(string(out), "\n"), nil
}
