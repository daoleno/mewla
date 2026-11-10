//go:build windows && !screenfake

package screen

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows calls Screen makes, in plain Go: user32 for input, windows and
// the desktop, gdi32 for pictures of the screen, and COM (ole32, oleaut32)
// for UI Automation.

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")
	oleaut32 = windows.NewLazySystemDLL("oleaut32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	procSendInput                      = user32.NewProc("SendInput")
	procGetSystemMetrics               = user32.NewProc("GetSystemMetrics")
	procSetProcessDpiAwarenessContext  = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetDC                          = user32.NewProc("GetDC")
	procReleaseDC                      = user32.NewProc("ReleaseDC")
	procIsWindowVisible                = user32.NewProc("IsWindowVisible")
	procIsIconic                       = user32.NewProc("IsIconic")
	procGetWindowRect                  = user32.NewProc("GetWindowRect")
	procGetWindow                      = user32.NewProc("GetWindow")
	procGetWindowLongPtrW              = user32.NewProc("GetWindowLongPtrW")
	procGetWindowTextW                 = user32.NewProc("GetWindowTextW")
	procSetForegroundWindow            = user32.NewProc("SetForegroundWindow")
	procShowWindow                     = user32.NewProc("ShowWindow")
	procSetWindowsHookExW              = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx                 = user32.NewProc("CallNextHookEx")
	procGetMessageW                    = user32.NewProc("GetMessageW")
	procOpenInputDesktop               = user32.NewProc("OpenInputDesktop")
	procCloseDesktop                   = user32.NewProc("CloseDesktop")
	procGetUserObjectInformationW      = user32.NewProc("GetUserObjectInformationW")
	procMessageBoxTimeoutW             = user32.NewProc("MessageBoxTimeoutW")
	procVkKeyScanW                     = user32.NewProc("VkKeyScanW")
	procMapVirtualKeyW                 = user32.NewProc("MapVirtualKeyW")
	procCreateCompatibleDC             = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection               = gdi32.NewProc("CreateDIBSection")
	procSelectObject                   = gdi32.NewProc("SelectObject")
	procBitBlt                         = gdi32.NewProc("BitBlt")
	procDeleteObject                   = gdi32.NewProc("DeleteObject")
	procDeleteDC                       = gdi32.NewProc("DeleteDC")
	procCoCreateInstance               = ole32.NewProc("CoCreateInstance")
	procSysFreeString                  = oleaut32.NewProc("SysFreeString")
	procSysAllocString                 = oleaut32.NewProc("SysAllocString")
	procSysStringLen                   = oleaut32.NewProc("SysStringLen")
	procVariantClear                   = oleaut32.NewProc("VariantClear")
	procSafeArrayGetLBound             = oleaut32.NewProc("SafeArrayGetLBound")
	procSafeArrayGetUBound             = oleaut32.NewProc("SafeArrayGetUBound")
	procSafeArrayGetElement            = oleaut32.NewProc("SafeArrayGetElement")
	procSafeArrayDestroy               = oleaut32.NewProc("SafeArrayDestroy")
	procDwmGetWindowAttribute          = dwmapi.NewProc("DwmGetWindowAttribute")
	procShellExecuteW                  = shell32.NewProc("ShellExecuteW")
	procGetModuleHandleW               = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
	procProcessIdToSessionId           = windows.NewLazySystemDLL("kernel32.dll").NewProc("ProcessIdToSessionId")
	procGetWindowThreadProcessId       = user32.NewProc("GetWindowThreadProcessId")
	procGetForegroundWindow            = user32.NewProc("GetForegroundWindow")
	procEnumWindows                    = user32.NewProc("EnumWindows")
	procAllowSetForegroundWindow       = user32.NewProc("AllowSetForegroundWindow")
	procGetGUIThreadInfo               = user32.NewProc("GetGUIThreadInfo")
	procQueryFullProcessImageNameW     = windows.NewLazySystemDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")
	procSetThreadDpiAwarenessContext   = user32.NewProc("SetThreadDpiAwarenessContext")
	procGetDpiAwarenessContextForProce = user32.NewProc("GetDpiAwarenessContextForProcess")
)

// comObject is any COM object: its first word points to its methods.
type comObject struct {
	vtbl *[256]uintptr
}

// hresult is a failed COM call.
type hresult uint32

func (h hresult) Error() string { return fmt.Sprintf("HRESULT 0x%08X", uint32(h)) }

// call calls method i of o with the arguments after this.
func (o *comObject) call(i int, args ...uintptr) error {
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(unsafe.Pointer(o)))
	all = append(all, args...)
	hr, _, _ := syscall.SyscallN(o.vtbl[i], all...)
	runtime.KeepAlive(o)
	if int32(hr) < 0 {
		return hresult(uint32(hr))
	}
	return nil
}

func (o *comObject) addRef() {
	if o != nil {
		_, _, _ = syscall.SyscallN(o.vtbl[1], uintptr(unsafe.Pointer(o)))
	}
}

func (o *comObject) release() {
	if o != nil {
		_, _, _ = syscall.SyscallN(o.vtbl[2], uintptr(unsafe.Pointer(o)))
	}
}

// bstr is a COM string the callee allocated.
type bstr *uint16

func bstrString(b *uint16) string {
	if b == nil {
		return ""
	}
	n, _, _ := procSysStringLen.Call(uintptr(unsafe.Pointer(b)))
	s := windows.UTF16ToString(unsafe.Slice(b, int(n)))
	_, _, _ = procSysFreeString.Call(uintptr(unsafe.Pointer(b)))
	return s
}

func allocBSTR(s string) (*uint16, error) {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return nil, err
	}
	var b *uint16
	r, _, _ := syscall.SyscallN(procSysAllocString.Addr(), uintptr(unsafe.Pointer(p)))
	*(*uintptr)(unsafe.Pointer(&b)) = r
	if b == nil {
		return nil, fmt.Errorf("out of memory")
	}
	return b, nil
}

func freeBSTR(b *uint16) {
	if b != nil {
		_, _, _ = procSysFreeString.Call(uintptr(unsafe.Pointer(b)))
	}
}

// variant is a COM VARIANT (24 bytes on 64-bit Windows).
type variant struct {
	vt   uint16
	_    [3]uint16
	val  uint64
	val2 uint64
}

const (
	vtEmpty = 0
	vtI4    = 3
	vtR8    = 5
	vtBSTR  = 8
	vtBool  = 11
	vtArray = 0x2000
)

func (v *variant) clear() { _, _, _ = procVariantClear.Call(uintptr(unsafe.Pointer(v))) }

// str is a string VARIANT's text, or "".
func (v *variant) str() string {
	if v.vt != vtBSTR || v.val == 0 {
		return ""
	}
	p := *(**uint16)(unsafe.Pointer(&v.val))
	n, _, _ := procSysStringLen.Call(uintptr(unsafe.Pointer(p)))
	return windows.UTF16ToString(unsafe.Slice(p, int(n)))
}

func (v *variant) boolean() bool { return v.vt == vtBool && int16(v.val) != 0 }

func (v *variant) int() int {
	switch v.vt {
	case vtI4:
		return int(int32(v.val))
	case vtR8:
		return int(*(*float64)(unsafe.Pointer(&v.val)))
	}
	return 0
}

func (v *variant) float() (float64, bool) {
	switch v.vt {
	case vtR8:
		return *(*float64)(unsafe.Pointer(&v.val)), true
	case vtI4:
		return float64(int32(v.val)), true
	}
	return 0, false
}

// ints is an array of int32 VARIANT's elements (a runtime id).
func (v *variant) ints() []int32 {
	if v.vt != vtArray|vtI4 || v.val == 0 {
		return nil
	}
	sa := uintptr(v.val)
	var lo, hi int32
	if r, _, _ := procSafeArrayGetLBound.Call(sa, 1, uintptr(unsafe.Pointer(&lo))); int32(r) < 0 {
		return nil
	}
	if r, _, _ := procSafeArrayGetUBound.Call(sa, 1, uintptr(unsafe.Pointer(&hi))); int32(r) < 0 {
		return nil
	}
	var out []int32
	for i := lo; i <= hi && len(out) < 64; i++ {
		var x int32
		idx := i
		if r, _, _ := procSafeArrayGetElement.Call(sa, uintptr(unsafe.Pointer(&idx)), uintptr(unsafe.Pointer(&x))); int32(r) < 0 {
			return nil
		}
		out = append(out, x)
	}
	return out
}

// rect is a Win32 RECT.
type rect struct{ Left, Top, Right, Bottom int32 }

func guid(s string) windows.GUID {
	g, err := windows.GUIDFromString(s)
	if err != nil {
		panic(err)
	}
	return g
}
