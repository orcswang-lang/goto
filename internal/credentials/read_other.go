//go:build !darwin && !linux && !windows

package credentials

// readFromOS 在其他平台不可用，一律视为凭据未找到。
func readFromOS(service, account string) (string, error) {
	return "", ErrNotFound
}
