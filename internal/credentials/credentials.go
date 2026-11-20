// Package credentials 从操作系统安全凭据存储读取跳板机凭据。
//
// v1 安全约定：密码与 TOTP Secret 只能通过本包获取，
// 绝不进入 MCP 请求参数、MCP 响应或普通日志。
package credentials

import (
	"errors"
	"time"
)

// ErrNotFound 表示凭据存储中没有对应条目，或平台后端不可用。
// 调用方应将其映射为 AUTH_REQUIRED，而不是降级为不安全行为。
var ErrNotFound = errors.New("凭据未找到")

// Provider 从本地安全凭据存储读取凭据。
type Provider interface {
	GetPassword(jumper string) (string, error)
	GetTOTPSecret(jumper string) (string, error)
}

// OS 是操作系统安全凭据存储的 Provider 实现：
//
//	macOS   Keychain（security 命令）
//	Linux   Secret Service（secret-tool 命令）
//	Windows Credential Manager（advapi32 CredReadW）
type OS struct {
	// readFn 覆盖底层读取，仅测试注入使用。
	readFn func(service, account string) (string, error)
	// timeout 是凭据命令的超时；keyring 需交互解锁时避免会话永久阻塞。
	timeout time.Duration
}

// NewOS 返回操作系统凭据提供器。
func NewOS() *OS { return &OS{timeout: 10 * time.Second} }

func (o *OS) GetPassword(jumper string) (string, error) {
	return o.read("goto", jumper)
}

func (o *OS) GetTOTPSecret(jumper string) (string, error) {
	return o.read("goto", jumper+":totp")
}

func (o *OS) read(service, account string) (string, error) {
	if o.readFn != nil {
		return o.readFn(service, account)
	}
	if o.timeout <= 0 {
		o.timeout = 10 * time.Second
	}
	return readFromOS(service, account, o.timeout)
}
