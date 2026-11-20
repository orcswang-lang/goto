//go:build windows

package credentials

import (
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const credTypeGeneric = 1

// credential 对应 Windows CREDENTIAL 结构（仅使用 CredentialBlob）。
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

var (
	advapi32      = windows.NewLazySystemDLL("advapi32.dll")
	procCredReadW = advapi32.NewProc("CredReadW")
	procCredFree  = advapi32.NewProc("CredFree")
)

// readFromOS 通过 Windows Credential Manager 读取条目。
// 存储方式：cmdkey /generic:goto/<account> /user:goto /pass:<secret>
// CredentialBlob 实际为 UTF-16LE；CredReadW 同步返回、无交互阻塞，故忽略 timeout。
// shortcut: credential 结构体按 64 位指针布局（amd64/arm64）；build.sh 仅产出
// windows/amd64，支持 386 时需为 4 字节指针重排字段。
func readFromOS(service, account string, timeout time.Duration) (string, error) {
	target := service + "/" + account
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return "", ErrNotFound
	}
	var cred *credential
	ret, _, _ := procCredReadW.Call(
		uintptr(unsafe.Pointer(targetPtr)),
		credTypeGeneric,
		0,
		uintptr(unsafe.Pointer(&cred)),
	)
	if ret == 0 {
		return "", ErrNotFound
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(cred)))
	if cred == nil || cred.CredentialBlob == nil || cred.CredentialBlobSize == 0 {
		return "", ErrNotFound
	}
	blob := unsafe.Slice(cred.CredentialBlob, cred.CredentialBlobSize)
	units := make([]uint16, len(blob)/2)
	for i := range units {
		units[i] = uint16(blob[2*i]) | uint16(blob[2*i+1])<<8
	}
	return string(utf16.Decode(units)), nil
}
