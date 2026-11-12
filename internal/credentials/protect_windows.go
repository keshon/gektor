package credentials

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func protect(raw []byte) ([]byte, error)   { return crypt(raw, false) }
func unprotect(raw []byte) ([]byte, error) { return crypt(raw, true) }
func crypt(raw []byte, decrypt bool) ([]byte, error) {
	input := windows.DataBlob{Size: uint32(len(raw))}
	if len(raw) > 0 {
		input.Data = &raw[0]
	}
	var output windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	} else {
		err = windows.CryptProtectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, output.Size)...), nil
}

// Encrypted reports whether persisted values are encrypted for the current account.
func Encrypted() bool { return true }

func Protection() string { return "Windows DPAPI, current Windows account" }
