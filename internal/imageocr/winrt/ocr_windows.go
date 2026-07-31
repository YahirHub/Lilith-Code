//go:build windows

package winrt

import (
	"syscall"
	"unsafe"
)

// Windows.Media.Ocr 命名空间的接口和类

// Rect 表示矩形区域
type Rect struct {
	X      float32
	Y      float32
	Width  float32
	Height float32
}

// IOcrWord 接口
var IID_IOcrWord = GUID{0x3C2A477A, 0x5CD9, 0x3525, [8]byte{0xBA, 0x2A, 0x23, 0xD1, 0xE0, 0xA6, 0x8A, 0x1D}}

type IOcrWord struct {
	IInspectable
}

type IOcrWordVtbl struct {
	IInspectableVtbl
	Get_BoundingRect uintptr
	Get_Text         uintptr
}

func (v *IOcrWord) VTable() *IOcrWordVtbl {
	return (*IOcrWordVtbl)(unsafe.Pointer(v.Vtbl))
}

func (v *IOcrWord) GetBoundingRect() Rect {
	var result Rect
	syscall.SyscallN(v.VTable().Get_BoundingRect, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(&result)))
	return result
}

func (v *IOcrWord) GetText() string {
	var hs HSTRING
	syscall.SyscallN(v.VTable().Get_Text, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(&hs)))
	result := HStringToString(hs)
	DeleteHString(hs)
	return result
}

// IOcrLine 接口
var IID_IOcrLine = GUID{0x0043A16F, 0xE31F, 0x3A24, [8]byte{0x89, 0x9C, 0xD4, 0x44, 0xBD, 0x08, 0x81, 0x24}}

type IOcrLine struct {
	IInspectable
}

type IOcrLineVtbl struct {
	IInspectableVtbl
	Get_Words uintptr
	Get_Text  uintptr
}

func (v *IOcrLine) VTable() *IOcrLineVtbl {
	return (*IOcrLineVtbl)(unsafe.Pointer(v.Vtbl))
}

func (v *IOcrLine) GetText() string {
	var hs HSTRING
	syscall.SyscallN(v.VTable().Get_Text, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(&hs)))
	result := HStringToString(hs)
	DeleteHString(hs)
	return result
}

// GetWords 返回 IOcrWord 集合的迭代器
func (v *IOcrLine) GetWords() (*IVectorView, error) {
	var result *IVectorView
	hr, _, _ := syscall.SyscallN(v.VTable().Get_Words, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(&result)))
	if hr != 0 {
		return nil, syscall.Errno(hr)
	}
	return result, nil
}

// IOcrResult 接口
var IID_IOcrResult = GUID{0x9BD235B2, 0x175B, 0x3D6A, [8]byte{0x92, 0xE2, 0x38, 0x8C, 0x20, 0x6E, 0x2F, 0x63}}

type IOcrResult struct {
	IInspectable
}

type IOcrResultVtbl struct {
	IInspectableVtbl
	Get_Lines     uintptr
	Get_TextAngle uintptr
	Get_Text      uintptr
}

func (v *IOcrResult) VTable() *IOcrResultVtbl {
	return (*IOcrResultVtbl)(unsafe.Pointer(v.Vtbl))
}

func (v *IOcrResult) GetText() string {
	var hs HSTRING
	syscall.SyscallN(v.VTable().Get_Text, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(&hs)))
	result := HStringToString(hs)
	DeleteHString(hs)
	return result
}

// GetLines 返回 IOcrLine 集合
func (v *IOcrResult) GetLines() (*IVectorView, error) {
	var result *IVectorView
	hr, _, _ := syscall.SyscallN(v.VTable().Get_Lines, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(&result)))
	if hr != 0 {
		return nil, syscall.Errno(hr)
	}
	return result, nil
}

// IOcrEngine 接口
var IID_IOcrEngine = GUID{0x5A14BC41, 0x5B76, 0x3140, [8]byte{0xB6, 0x80, 0x88, 0x25, 0x56, 0x26, 0x83, 0xAC}}

type IOcrEngine struct {
	IInspectable
}

type IOcrEngineVtbl struct {
	IInspectableVtbl
	RecognizeAsync         uintptr
	Get_RecognizerLanguage uintptr
}

func (v *IOcrEngine) VTable() *IOcrEngineVtbl {
	return (*IOcrEngineVtbl)(unsafe.Pointer(v.Vtbl))
}

// RecognizeAsync 执行 OCR 识别
func (v *IOcrEngine) RecognizeAsync(bitmap *ISoftwareBitmap) (*IAsyncOperation, error) {
	var result *IAsyncOperation
	hr, _, _ := syscall.SyscallN(v.VTable().RecognizeAsync,
		uintptr(unsafe.Pointer(v)),
		uintptr(unsafe.Pointer(bitmap)),
		uintptr(unsafe.Pointer(&result)))
	if hr != 0 {
		return nil, syscall.Errno(hr)
	}
	return result, nil
}

// IOcrEngineStatics 静态接口
var IID_IOcrEngineStatics = GUID{0x5BFFA85A, 0x3384, 0x3540, [8]byte{0x99, 0x40, 0x69, 0x91, 0x20, 0xD4, 0x28, 0xA8}}

type IOcrEngineStatics struct {
	IInspectable
}

type IOcrEngineStaticsVtbl struct {
	IInspectableVtbl
	Get_MaxImageDimension             uintptr
	Get_AvailableRecognizerLanguages  uintptr
	IsLanguageSupported               uintptr
	TryCreateFromLanguage             uintptr
	TryCreateFromUserProfileLanguages uintptr
}

func (v *IOcrEngineStatics) VTable() *IOcrEngineStaticsVtbl {
	return (*IOcrEngineStaticsVtbl)(unsafe.Pointer(v.Vtbl))
}

// TryCreateFromUserProfileLanguages 创建使用用户配置语言的 OCR 引擎
func (v *IOcrEngineStatics) TryCreateFromUserProfileLanguages() (*IOcrEngine, error) {
	var result *IOcrEngine
	hr, _, _ := syscall.SyscallN(v.VTable().TryCreateFromUserProfileLanguages,
		uintptr(unsafe.Pointer(v)),
		uintptr(unsafe.Pointer(&result)))
	if hr != 0 {
		return nil, syscall.Errno(hr)
	}
	return result, nil
}

// GetOcrEngineStatics 获取 OcrEngine 的静态接口
func GetOcrEngineStatics() (*IOcrEngineStatics, error) {
	factory, err := GetActivationFactory("Windows.Media.Ocr.OcrEngine", &IID_IOcrEngineStatics)
	if err != nil {
		return nil, err
	}
	return (*IOcrEngineStatics)(unsafe.Pointer(factory)), nil
}
