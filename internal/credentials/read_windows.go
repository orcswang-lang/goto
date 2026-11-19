//go:build windows

package credentials

import (
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
func readFromOS(service, account string) (string, error) {
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
	// 约定存储为 UTF-8 文本。
	blob := unsafe.Slice(cred.CredentialBlob, cred.CredentialBlobSize)
	units := make([]uint16, len(blob)/2)
	for i := range units {
		units[i] = uint16(blob[2*i]) | uint16(blob[2*i+1])<<8
	}
	return string(utf16.Decode(units)), nil
}
