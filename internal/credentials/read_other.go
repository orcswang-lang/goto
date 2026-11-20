//go:build !darwin && !linux && !windows

package credentials

import "time"

// readFromOS 在其他平台不可用，一律视为凭据未找到。
func readFromOS(service, account string, timeout time.Duration) (string, error) {
	return "", ErrNotFound
}
