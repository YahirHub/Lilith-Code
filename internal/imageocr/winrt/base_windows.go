//go:build windows

// Package winrt provides the minimal Windows Runtime bindings needed by imageocr.
package winrt

import (
	"syscall"
	"unsafe"
)

var (
	modole32                      = syscall.NewLazyDLL("ole32.dll")
	modcombase                    = syscall.NewLazyDLL("combase.dll")
	procCoInitializeEx            = modole32.NewProc("CoInitializeEx")
	procRoInitialize              = modcombase.NewProc("RoInitialize")
	procRoActivateInstance        = modcombase.NewProc("RoActivateInstance")
	procRoGetActivationFactory    = modcombase.NewProc("RoGetActivationFactory")
	procWindowsCreateString       = modcombase.NewProc("WindowsCreateString")
	procWindowsDeleteString       = modcombase.NewProc("WindowsDeleteString")
	procWindowsGetStringRawBuffer = modcombase.NewProc("WindowsGetStringRawBuffer")
)

const (
	RO_INIT_SINGLETHREADED = 0
	RO_INIT_MULTITHREADED  = 1
)

// HSTRING 是 Windows Runtime 字符串句柄
type HSTRING uintptr

// GUID 表示 COM 接口标识符
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// IInspectable 是所有 WinRT 对象的基础接口
type IInspectable struct {
	Vtbl *IInspectableVtbl
}

type IInspectableVtbl struct {
	QueryInterface      uintptr
	AddRef              uintptr
	Release             uintptr
	GetIids             uintptr
	GetRuntimeClassName uintptr
	GetTrustLevel       uintptr
}

func (v *IInspectable) AddRef() uint32 {
	ret, _, _ := syscall.SyscallN(v.Vtbl.AddRef, uintptr(unsafe.Pointer(v)))
	return uint32(ret)
}

func (v *IInspectable) Release() uint32 {
	ret, _, _ := syscall.SyscallN(v.Vtbl.Release, uintptr(unsafe.Pointer(v)))
	return uint32(ret)
}

func (v *IInspectable) QueryInterface(iid *GUID, ppv *unsafe.Pointer) int32 {
	ret, _, _ := syscall.SyscallN(
		v.Vtbl.QueryInterface,
		uintptr(unsafe.Pointer(v)),
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(ppv)),
	)
	return int32(ret)
}

// Initialize 初始化 Windows Runtime
func Initialize() error {
	hr, _, _ := procRoInitialize.Call(uintptr(RO_INIT_MULTITHREADED))
	if hr != 0 && hr != 0x80010106 { // RPC_E_CHANGED_MODE 表示已初始化
		return syscall.Errno(hr)
	}
	return nil
}

// NewHString 创建 HSTRING
func NewHString(s string) (HSTRING, error) {
	u16 := syscall.StringToUTF16(s)
	var hs HSTRING
	hr, _, _ := procWindowsCreateString.Call(
		uintptr(unsafe.Pointer(&u16[0])),
		uintptr(len(u16)-1), // 不包含 null 终止符
		uintptr(unsafe.Pointer(&hs)),
	)
	if hr != 0 {
		return 0, syscall.Errno(hr)
	}
	return hs, nil
}

// DeleteHString 释放 HSTRING
func DeleteHString(hs HSTRING) {
	if hs != 0 {
		procWindowsDeleteString.Call(uintptr(hs))
	}
}

// HStringToString 将 HSTRING 转换为 Go string
func HStringToString(hs HSTRING) string {
	if hs == 0 {
		return ""
	}
	var length uint32
	ptr, _, _ := procWindowsGetStringRawBuffer.Call(
		uintptr(hs),
		uintptr(unsafe.Pointer(&length)),
	)
	if ptr == 0 {
		return ""
	}
	// 将 UTF-16 转换为 Go string
	u16 := make([]uint16, length)
	for i := uint32(0); i < length; i++ {
		u16[i] = *(*uint16)(unsafe.Pointer(ptr + uintptr(i)*2))
	}
	return syscall.UTF16ToString(u16)
}

// GetActivationFactory 获取 WinRT 类的激活工厂
func GetActivationFactory(classID string, iid *GUID) (*IInspectable, error) {
	hs, err := NewHString(classID)
	if err != nil {
		return nil, err
	}
	defer DeleteHString(hs)

	var factory *IInspectable
	hr, _, _ := procRoGetActivationFactory.Call(
		uintptr(hs),
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if hr != 0 {
		return nil, syscall.Errno(hr)
	}
	return factory, nil
}

// ActivateInstance 创建 WinRT 类的实例
func ActivateInstance(classID string) (*IInspectable, error) {
	hs, err := NewHString(classID)
	if err != nil {
		return nil, err
	}
	defer DeleteHString(hs)

	var instance *IInspectable
	hr, _, _ := procRoActivateInstance.Call(
		uintptr(hs),
		uintptr(unsafe.Pointer(&instance)),
	)
	if hr != 0 {
		return nil, syscall.Errno(hr)
	}
	return instance, nil
}
