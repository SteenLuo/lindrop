package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"
)

type clipboardScreenshot struct {
	PNG bool
	Raw []byte
}

func copyClipboardMemory(format uint32) ([]byte, error) {
	h := call(user.NewProc("GetClipboardData"), uintptr(format))
	if h == 0 {
		return nil, errors.New("无法读取剪贴板图片，请重新复制")
	}
	n := call(kernel.NewProc("GlobalSize"), h)
	if n == 0 || n > maxScreenshotBytes {
		return nil, errors.New("剪贴板图片过大或为空")
	}
	p := call(kernel.NewProc("GlobalLock"), h)
	if p == 0 {
		return nil, errors.New("无法读取剪贴板图片内存")
	}
	defer call(kernel.NewProc("GlobalUnlock"), h)
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(p)), int(n))...), nil
}

func clipboardBitmapDIB() ([]byte, error) {
	h := call(user.NewProc("GetClipboardData"), 2) // CF_BITMAP, owned by clipboard.
	var bm struct {
		Type, Width, Height, WidthBytes int32
		Planes, BitsPixel               uint16
		Bits                            uintptr
	}
	if h == 0 || call(gdi.NewProc("GetObjectW"), h, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))) == 0 {
		return nil, errors.New("无法读取剪贴板位图")
	}
	w, height := int64(bm.Width), int64(bm.Height)
	if err := checkScreenshotSize(w, height); err != nil {
		return nil, err
	}
	size := w * height * 4
	if size > maxScreenshotBytes-40 {
		return nil, errors.New("剪贴板位图过大")
	}
	raw := make([]byte, int(size)+40)
	binary.LittleEndian.PutUint32(raw, 40)
	binary.LittleEndian.PutUint32(raw[4:], uint32(w))
	binary.LittleEndian.PutUint32(raw[8:], uint32(-height))
	binary.LittleEndian.PutUint16(raw[12:], 1)
	binary.LittleEndian.PutUint16(raw[14:], 32)
	dc := call(user.NewProc("GetDC"), 0)
	if dc == 0 {
		return nil, errors.New("无法创建位图读取环境")
	}
	defer call(user.NewProc("ReleaseDC"), 0, dc)
	if got := call(gdi.NewProc("GetDIBits"), dc, h, 0, uintptr(height), uintptr(unsafe.Pointer(&raw[40])), uintptr(unsafe.Pointer(&raw[0])), 0); got != uintptr(height) {
		return nil, errors.New("剪贴板位图数据不完整")
	}
	return raw, nil
}

// Read-only snapshot: never empty/change the user's clipboard and never keep its
// handles after CloseClipboard. PNG decoding and encoding run outside the UI.
func readClipboardScreenshot(owner uintptr) (clipboardScreenshot, bool, error) {
	pngFormat := uint32(call(user.NewProc("RegisterClipboardFormatW"), uintptr(unsafe.Pointer(wp("PNG")))))
	formats := []uint32{pngFormat, 17, 8, 2}
	hasImage := false
	for _, format := range formats {
		if format != 0 && call(user.NewProc("IsClipboardFormatAvailable"), uintptr(format)) != 0 {
			hasImage = true
		}
	}
	if !hasImage {
		return clipboardScreenshot{}, false, nil
	}
	if call(user.NewProc("OpenClipboard"), owner) == 0 {
		return clipboardScreenshot{}, true, errors.New("剪贴板正忙，请稍后再粘贴截图")
	}
	defer call(user.NewProc("CloseClipboard"))
	var last error
	for _, format := range formats {
		if format == 0 || call(user.NewProc("IsClipboardFormatAvailable"), uintptr(format)) == 0 {
			continue
		}
		var raw []byte
		var err error
		if format == 2 {
			raw, err = clipboardBitmapDIB()
		} else {
			raw, err = copyClipboardMemory(format)
		}
		if err == nil {
			return clipboardScreenshot{format == pngFormat, raw}, true, nil
		}
		last = err
	}
	return clipboardScreenshot{}, true, fmt.Errorf("读取截图失败：%v", last)
}
