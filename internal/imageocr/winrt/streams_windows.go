//go:build windows

package winrt

import (
	"syscall"
	"unsafe"
)

// Windows.Storage.Streams 命名空间接口

// IDataWriter 接口
var IID_IDataWriter = GUID{0x64B89265, 0xD341, 0x4922, [8]byte{0xB3, 0x8A, 0xDD, 0x4A, 0xF8, 0x80, 0x8C, 0x4E}}

type IDataWriter struct {
	IInspectable
}

type IDataWriterVtbl struct {
	IInspectableVtbl
	Get_UnstoredBufferLength uintptr
	Get_UnicodeEncoding      uintptr
	Put_UnicodeEncoding      uintptr
	Get_ByteOrder            uintptr
	Put_ByteOrder            uintptr
	WriteByte                uintptr
	WriteBytes               uintptr
	WriteBuffer              uintptr
	WriteBufferRange         uintptr
	WriteBoolean             uintptr
	WriteGuid                uintptr
	WriteInt16               uintptr
	WriteInt32               uintptr
	WriteInt64               uintptr
	WriteUInt16              uintptr
	WriteUInt32              uintptr
	WriteUInt64              uintptr
	WriteSingle              uintptr
	WriteDouble              uintptr
	WriteDateTime            uintptr
	WriteTimeSpan            uintptr
	WriteString              uintptr
	MeasureString            uintptr
	StoreAsync               uintptr
	FlushAsync               uintptr
	DetachBuffer             uintptr
	DetachStream             uintptr
}

func (v *IDataWriter) VTable() *IDataWriterVtbl {
	return (*IDataWriterVtbl)(unsafe.Pointer(v.Vtbl))
}

func (v *IDataWriter) WriteBytes(data []byte) {
	if len(data) == 0 {
		return
	}
	syscall.SyscallN(v.VTable().WriteBytes,
		uintptr(unsafe.Pointer(v)),
		uintptr(len(data)),
		uintptr(unsafe.Pointer(&data[0])))
}

func (v *IDataWriter) StoreAsync() (*IAsyncOperation, error) {
	var result *IAsyncOperation
	hr, _, _ := syscall.SyscallN(v.VTable().StoreAsync,
		uintptr(unsafe.Pointer(v)),
		uintptr(unsafe.Pointer(&result)))
	if hr != 0 {
		return nil, syscall.Errno(hr)
	}
	return result, nil
}

func (v *IDataWriter) FlushAsync() (*IAsyncOperation, error) {
	var result *IAsyncOperation
	hr, _, _ := syscall.SyscallN(v.VTable().FlushAsync,
		uintptr(unsafe.Pointer(v)),
		uintptr(unsafe.Pointer(&result)))
	if hr != 0 {
		return nil, syscall.Errno(hr)
	}
	return result, nil
}

func (v *IDataWriter) DetachStream() {
	syscall.SyscallN(v.VTable().DetachStream, uintptr(unsafe.Pointer(v)))
}

// IDataWriterFactory 接口
var IID_IDataWriterFactory = GUID{0x338C67C2, 0x8B84, 0x4C2B, [8]byte{0x9C, 0x50, 0x7B, 0x87, 0x67, 0x84, 0x7A, 0x1F}}

type IDataWriterFactory struct {
	IInspectable
}

type IDataWriterFactoryVtbl struct {
	IInspectableVtbl
	CreateDataWriter uintptr
}

func (v *IDataWriterFactory) VTable() *IDataWriterFactoryVtbl {
	return (*IDataWriterFactoryVtbl)(unsafe.Pointer(v.Vtbl))
}

func (v *IDataWriterFactory) CreateDataWriter(stream *IRandomAccessStream) (*IDataWriter, error) {
	var result *IDataWriter
	hr, _, _ := syscall.SyscallN(v.VTable().CreateDataWriter,
		uintptr(unsafe.Pointer(v)),
		uintptr(unsafe.Pointer(stream)),
		uintptr(unsafe.Pointer(&result)))
	if hr != 0 {
		return nil, syscall.Errno(hr)
	}
	return result, nil
}

// GetDataWriterFactory 获取 DataWriter 工厂
func GetDataWriterFactory() (*IDataWriterFactory, error) {
	factory, err := GetActivationFactory("Windows.Storage.Streams.DataWriter", &IID_IDataWriterFactory)
	if err != nil {
		return nil, err
	}
	return (*IDataWriterFactory)(unsafe.Pointer(factory)), nil
}

// IOutputStream 接口
var IID_IOutputStream = GUID{0x905A0FE6, 0xBC53, 0x11DF, [8]byte{0x8C, 0x49, 0x00, 0x1E, 0x4F, 0xC6, 0x86, 0xDA}}

// IInputStream 接口
var IID_IInputStream = GUID{0x905A0FE2, 0xBC53, 0x11DF, [8]byte{0x8C, 0x49, 0x00, 0x1E, 0x4F, 0xC6, 0x86, 0xDA}}
