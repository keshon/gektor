//go:build windows

package clipboard

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var user32 = windows.NewLazySystemDLL("user32.dll")
var openClipboard = user32.NewProc("OpenClipboard")
var closeClipboard = user32.NewProc("CloseClipboard")
var getData = user32.NewProc("GetClipboardData")
var available = user32.NewProc("IsClipboardFormatAvailable")
var register = user32.NewProc("RegisterClipboardFormatW")
var kernel32 = windows.NewLazySystemDLL("kernel32.dll")
var globalLock = kernel32.NewProc("GlobalLock")
var globalUnlock = kernel32.NewProc("GlobalUnlock")
var globalSize = kernel32.NewProc("GlobalSize")
var moveMemory = kernel32.NewProc("RtlMoveMemory")

// ReadImage returns nil when the clipboard contains no image. It never alters it.
func ReadImage() ([]byte, error) {
	data, isPNG, err := readImageData()
	if err != nil || len(data) == 0 {
		return nil, err
	}
	if isPNG {
		return PNG(data)
	}
	return DIB(data)
}

// Copy native memory while locked; decoding runs after releasing the clipboard.
func readImageData() ([]byte, bool, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ok, _, _ := openClipboard.Call(0)
	if ok == 0 {
		return nil, false, fmt.Errorf("clipboard is busy; try pasting again")
	}
	defer closeClipboard.Call()
	name, _ := windows.UTF16PtrFromString("PNG")
	format, _, _ := register.Call(uintptr(unsafe.Pointer(name)))
	for _, f := range []uintptr{format, 17, 8} {
		if f == 0 {
			continue
		}
		has, _, _ := available.Call(f)
		if has == 0 {
			continue
		}
		handle, _, _ := getData.Call(f)
		if handle == 0 {
			return nil, false, fmt.Errorf("cannot read clipboard image")
		}
		size, _, _ := globalSize.Call(handle)
		if size == 0 || size > MaxPixels*4+4096 || (f == format && size > MaxBytes) {
			return nil, false, fmt.Errorf("clipboard image is too large or empty")
		}
		ptr, _, _ := globalLock.Call(handle)
		if ptr == 0 {
			return nil, false, fmt.Errorf("cannot lock clipboard image")
		}
		data := make([]byte, int(size))
		moveMemory.Call(uintptr(unsafe.Pointer(&data[0])), ptr, size)
		runtime.KeepAlive(data)
		globalUnlock.Call(handle)
		return data, f == format, nil
	}
	return nil, false, nil
}
