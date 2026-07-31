//go:build windows

package winrt

import (
	"syscall"
	"unsafe"
)

// Windows.Graphics.Imaging 命名空间的接口和类

// BitmapPixelFormat 枚举
type BitmapPixelFormat int32

const (
	BitmapPixelFormat_Unknown BitmapPixelFormat = 0
	BitmapPixelFormat_Rgba16  BitmapPixelFormat = 12
	BitmapPixelFormat_Rgba8   BitmapPixelFormat = 30
	BitmapPixelFormat_Gray16  BitmapPixelFormat = 57
	BitmapPixelFormat_Gray8   BitmapPixelFormat = 62
	BitmapPixelFormat_Bgra8   BitmapPixelFormat = 87
	BitmapPixelFormat_Nv12    BitmapPixelFormat = 103
	BitmapPixelFormat_P010    BitmapPixelFormat = 104
	BitmapPixelFormat_Yuy2    BitmapPixelFormat = 107
)

// BitmapAlphaMode 枚举
type BitmapAlphaMode int32

const (
	BitmapAlphaMode_Premultiplied BitmapAlphaMode = 0
	BitmapAlphaMode_Straight      BitmapAlphaMode = 1
	BitmapAlphaMode_Ignore        BitmapAlphaMode = 2
)

// ISoftwareBitmap 接口
var IID_ISoftwareBitmap = GUID{0xDF0385DB, 0x672F, 0x4A9D, [8]byte{0x80, 0x6E, 0xC2, 0x44, 0x2F, 0x34, 0x3E, 0x86}}

type ISoftwareBitmap struct {
	IInspectable
}

type ISoftwareBitmapVtbl struct {
	IInspectableVtbl
	Get_BitmapPixelFormat uintptr
	Get_BitmapAlphaMode   uintptr
	Get_PixelWidth        uintptr
	Get_PixelHeight       uintptr
	Get_IsReadOnly        uintptr
	Put_DpiX              uintptr
	Get_DpiX              uintptr
	Put_DpiY              uintptr
	Get_DpiY              uintptr
	LockBuffer            uintptr
	CopyTo                uintptr
	CopyFromBuffer        uintptr
	CopyToBuffer          uintptr
	GetReadOnlyView       uintptr
}

func (v *ISoftwareBitmap) VTable() *ISoftwareBitmapVtbl {
	return (*ISoftwareBitmapVtbl)(unsafe.Pointer(v.Vtbl))
}

func (v *ISoftwareBitmap) GetPixelWidth() int32 {
	var result int32
	syscall.SyscallN(v.VTable().Get_PixelWidth, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(&result)))
	return result
}

func (v *ISoftwareBitmap) GetPixelHeight() int32 {
	var result int32
	syscall.SyscallN(v.VTable().Get_PixelHeight, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(&result)))
	return result
}

// IBitmapDecoder 接口
var IID_IBitmapDecoder = GUID{0xACEF22BA, 0x1D74, 0x4C91, [8]byte{0x9D, 0xFC, 0x96, 0x20, 0x74, 0x52, 0x33, 0xE6}}

type IBitmapDecoder struct {
	IInspectable
}

type IBitmapDecoderVtbl struct {
	IInspectableVtbl
	Get_BitmapContainerProperties uintptr
	Get_DecoderInformation        uintptr
	Get_FrameCount                uintptr
	GetPreviewAsync               uintptr
	GetFrameAsync                 uintptr
}

// IBitmapDecoderStatics 静态接口
var IID_IBitmapDecoderStatics = GUID{0x438CCB26, 0xBCEF, 0x4E95, [8]byte{0xBA, 0xD6, 0x23, 0xA8, 0x22, 0xE5, 0x8D, 0x01}}

type IBitmapDecoderStatics struct {
	IInspectable
}

type IBitmapDecoderStaticsVtbl struct {
	IInspectableVtbl
	Get_BmpDecoderId                uintptr
	Get_JpegDecoderId               uintptr
	Get_PngDecoderId                uintptr
	Get_TiffDecoderId               uintptr
	Get_GifDecoderId                uintptr
	Get_JpegXRDecoderId             uintptr
	Get_IcoDecoderId                uintptr
	GetDecoderInformationEnumerator uintptr
	CreateAsync                     uintptr
	CreateWithIdAsync               uintptr
}

func (v *IBitmapDecoderStatics) VTable() *IBitmapDecoderStaticsVtbl {
	return (*IBitmapDecoderStaticsVtbl)(unsafe.Pointer(v.Vtbl))
}

// IBitmapFrame 接口
var IID_IBitmapFrame = GUID{0x72A49A1C, 0x8081, 0x438D, [8]byte{0x91, 0xBC, 0x94, 0xEC, 0xFC, 0x83, 0x85, 0xC6}}

type IBitmapFrame struct {
	IInspectable
}

type IBitmapFrameVtbl struct {
	IInspectableVtbl
	GetThumbnailAsync            uintptr
	Get_BitmapProperties         uintptr
	Get_BitmapPixelFormat        uintptr
	Get_BitmapAlphaMode          uintptr
	Get_DpiX                     uintptr
	Get_DpiY                     uintptr
	Get_PixelWidth               uintptr
	Get_PixelHeight              uintptr
	Get_OrientedPixelWidth       uintptr
	Get_OrientedPixelHeight      uintptr
	GetPixelDataAsync            uintptr
	GetPixelDataTransformedAsync uintptr
}

// IBitmapFrameWithSoftwareBitmap 接口
var IID_IBitmapFrameWithSoftwareBitmap = GUID{0xFE287C9A, 0x420C, 0x4963, [8]byte{0x87, 0xAD, 0x69, 0x14, 0x36, 0xE0, 0x83, 0x83}}

type IBitmapFrameWithSoftwareBitmap struct {
	IInspectable
}

type IBitmapFrameWithSoftwareBitmapVtbl struct {
	IInspectableVtbl
	GetSoftwareBitmapAsync            uintptr
	GetSoftwareBitmapConvertedAsync   uintptr
	GetSoftwareBitmapTransformedAsync uintptr
}

func (v *IBitmapFrameWithSoftwareBitmap) VTable() *IBitmapFrameWithSoftwareBitmapVtbl {
	return (*IBitmapFrameWithSoftwareBitmapVtbl)(unsafe.Pointer(v.Vtbl))
}

// GetBitmapDecoderStatics 获取 BitmapDecoder 的静态接口
func GetBitmapDecoderStatics() (*IBitmapDecoderStatics, error) {
	factory, err := GetActivationFactory("Windows.Graphics.Imaging.BitmapDecoder", &IID_IBitmapDecoderStatics)
	if err != nil {
		return nil, err
	}
	return (*IBitmapDecoderStatics)(unsafe.Pointer(factory)), nil
}
