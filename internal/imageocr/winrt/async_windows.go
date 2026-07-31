//go:build windows

package winrt

import (
	"syscall"
	"time"
	"unsafe"
)

// IAsyncInfo 接口
var IID_IAsyncInfo = GUID{0x00000036, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}

// AsyncStatus 枚举
type AsyncStatus int32

const (
	AsyncStatus_Started   AsyncStatus = 0
	AsyncStatus_Completed AsyncStatus = 1
	AsyncStatus_Canceled  AsyncStatus = 2
	AsyncStatus_Error     AsyncStatus = 3
)

// IAsyncOperation 泛型异步操作接口
type IAsyncOperation struct {
	IInspectable
}

type IAsyncOperationVtbl struct {
	IInspectableVtbl
	Put_Completed uintptr
	Get_Completed uintptr
	GetResults    uintptr
}

func (v *IAsyncOperation) VTable() *IAsyncOperationVtbl {
	return (*IAsyncOperationVtbl)(unsafe.Pointer(v.Vtbl))
}

// IAsyncInfo 接口（通过 QueryInterface 获取）
type IAsyncInfo struct {
	IInspectable
}

type IAsyncInfoVtbl struct {
	IInspectableVtbl
	Get_Id        uintptr
	Get_Status    uintptr
	Get_ErrorCode uintptr
	Cancel        uintptr
	Close         uintptr
}

func (v *IAsyncInfo) VTable() *IAsyncInfoVtbl {
	return (*IAsyncInfoVtbl)(unsafe.Pointer(v.Vtbl))
}

func (v *IAsyncInfo) GetStatus() AsyncStatus {
	var result AsyncStatus
	syscall.SyscallN(v.VTable().Get_Status, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(&result)))
	return result
}

// Wait 等待异步操作完成
func (op *IAsyncOperation) Wait(timeout time.Duration) error {
	// 获取 IAsyncInfo 接口
	var asyncInfo *IAsyncInfo
	var ptr unsafe.Pointer
	hr := op.QueryInterface(&IID_IAsyncInfo, &ptr)
	if hr != 0 {
		return syscall.Errno(hr)
	}
	asyncInfo = (*IAsyncInfo)(ptr)
	defer asyncInfo.Release()

	// 轮询等待完成
	deadline := time.Now().Add(timeout)
	for {
		status := asyncInfo.GetStatus()
		if status != AsyncStatus_Started {
			if status == AsyncStatus_Error {
				return syscall.Errno(0x80004005) // E_FAIL
			}
			return nil
		}
		if time.Now().After(deadline) {
			return syscall.ETIMEDOUT
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// GetResults 获取异步操作结果（返回 IInspectable）
func (op *IAsyncOperation) GetResults() (*IInspectable, error) {
	var result *IInspectable
	hr, _, _ := syscall.SyscallN(op.VTable().GetResults,
		uintptr(unsafe.Pointer(op)),
		uintptr(unsafe.Pointer(&result)))
	if hr != 0 {
		return nil, syscall.Errno(hr)
	}
	return result, nil
}

// IVectorView 泛型只读集合接口
type IVectorView struct {
	IInspectable
}

type IVectorViewVtbl struct {
	IInspectableVtbl
	GetAt    uintptr
	Get_Size uintptr
	IndexOf  uintptr
	GetMany  uintptr
}

func (v *IVectorView) VTable() *IVectorViewVtbl {
	return (*IVectorViewVtbl)(unsafe.Pointer(v.Vtbl))
}

func (v *IVectorView) GetSize() uint32 {
	var result uint32
	syscall.SyscallN(v.VTable().Get_Size, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(&result)))
	return result
}

func (v *IVectorView) GetAt(index uint32) (*IInspectable, error) {
	var result *IInspectable
	hr, _, _ := syscall.SyscallN(v.VTable().GetAt,
		uintptr(unsafe.Pointer(v)),
		uintptr(index),
		uintptr(unsafe.Pointer(&result)))
	if hr != 0 {
		return nil, syscall.Errno(hr)
	}
	return result, nil
}

// IRandomAccessStream 接口
var IID_IRandomAccessStream = GUID{0x905A0FE1, 0xBC53, 0x11DF, [8]byte{0x8C, 0x49, 0x00, 0x1E, 0x4F, 0xC6, 0x86, 0xDA}}

type IRandomAccessStream struct {
	IInspectable
}

type IRandomAccessStreamVtbl struct {
	IInspectableVtbl
	Get_Size          uintptr
	Put_Size          uintptr
	GetInputStreamAt  uintptr
	GetOutputStreamAt uintptr
	Get_Position      uintptr
	Seek              uintptr
	CloneStream       uintptr
}

func (v *IRandomAccessStream) VTable() *IRandomAccessStreamVtbl {
	return (*IRandomAccessStreamVtbl)(unsafe.Pointer(v.Vtbl))
}

// IRandomAccessStreamReference 接口
var IID_IRandomAccessStreamReference = GUID{0x33EE3134, 0x1DD6, 0x4E3A, [8]byte{0x80, 0x67, 0xD1, 0xC1, 0x62, 0xE8, 0x64, 0x2B}}

// InMemoryRandomAccessStream
var IID_IInMemoryRandomAccessStream = GUID{0x905A0FE1, 0xBC53, 0x11DF, [8]byte{0x8C, 0x49, 0x00, 0x1E, 0x4F, 0xC6, 0x86, 0xDA}}

// 创建 InMemoryRandomAccessStream
func CreateInMemoryRandomAccessStream() (*IRandomAccessStream, error) {
	instance, err := ActivateInstance("Windows.Storage.Streams.InMemoryRandomAccessStream")
	if err != nil {
		return nil, err
	}
	return (*IRandomAccessStream)(unsafe.Pointer(instance)), nil
}
