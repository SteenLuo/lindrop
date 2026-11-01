package main

import (
	"bytes"
	"encoding/binary"
	"image"
	imagecolor "image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func screenshotDIBFixture(bits int, topDown bool, header int) []byte {
	stride := (3*bits/8 + 3) &^ 3
	raw := make([]byte, header+stride*2)
	binary.LittleEndian.PutUint32(raw, uint32(header))
	binary.LittleEndian.PutUint32(raw[4:], 3)
	h := int32(2)
	if topDown {
		h = -h
	}
	binary.LittleEndian.PutUint32(raw[8:], uint32(h))
	binary.LittleEndian.PutUint16(raw[12:], 1)
	binary.LittleEndian.PutUint16(raw[14:], uint16(bits))
	if header >= 56 {
		binary.LittleEndian.PutUint32(raw[16:], 3)
		binary.LittleEndian.PutUint32(raw[40:], 0xff0000)
		binary.LittleEndian.PutUint32(raw[44:], 0xff00)
		binary.LittleEndian.PutUint32(raw[48:], 0xff)
		binary.LittleEndian.PutUint32(raw[52:], 0xff000000)
	}
	for y := 0; y < 2; y++ {
		sy := y
		if !topDown {
			sy = 1 - y
		}
		for x := 0; x < 3; x++ {
			i := header + sy*stride + x*bits/8
			raw[i], raw[i+1], raw[i+2] = byte(x+3), byte(y+4), byte(20+x+y)
			if bits == 32 && header >= 56 {
				raw[i+3] = 128
			}
		}
	}
	return raw
}

func TestScreenshotDIBPixelsOrientationPaddingAndAlpha(t *testing.T) {
	for _, c := range []struct{ bits, header int }{{24, 40}, {32, 40}, {32, 124}} {
		for _, top := range []bool{false, true} {
			img, err := decodeScreenshotDIB(screenshotDIBFixture(c.bits, top, c.header))
			if err != nil {
				t.Fatal(err)
			}
			for y := 0; y < 2; y++ {
				for x := 0; x < 3; x++ {
					a := byte(255)
					if c.header == 124 {
						a = 128
					}
					want := imagecolor.NRGBA{byte(20 + x + y), byte(y + 4), byte(x + 3), a}
					if got := imagecolor.NRGBAModel.Convert(img.At(x, y)); got != want {
						t.Fatal("screenshot pixels changed", c, top, x, y, got, want)
					}
				}
			}
		}
	}
	// BITMAPINFOHEADER bitfields are stored after, rather than in, the header.
	v5 := screenshotDIBFixture(32, true, 124)
	bitfields := append(append(append([]byte{}, v5[:40]...), v5[40:52]...), v5[124:]...)
	binary.LittleEndian.PutUint32(bitfields, 40)
	if _, err := decodeScreenshotDIB(bitfields); err != nil {
		t.Fatal(err)
	}
}

func TestScreenshotRejectsMalformedAndOversizedDIB(t *testing.T) {
	valid := screenshotDIBFixture(32, false, 124)
	for n := 0; n < len(valid); n++ {
		if _, err := decodeScreenshotDIB(valid[:n]); err == nil {
			t.Fatal("truncated DIB accepted", n)
		}
	}
	for _, mutate := range []func([]byte){
		func(b []byte) { binary.LittleEndian.PutUint32(b, 0xffffffff) },
		func(b []byte) { binary.LittleEndian.PutUint32(b[4:], 0x7fffffff) },
		func(b []byte) { binary.LittleEndian.PutUint32(b[8:], 0x80000000) },
		func(b []byte) { binary.LittleEndian.PutUint16(b[12:], 2) },
		func(b []byte) { binary.LittleEndian.PutUint16(b[14:], 16) },
		func(b []byte) { binary.LittleEndian.PutUint32(b[16:], 1) },
		func(b []byte) { binary.LittleEndian.PutUint32(b[32:], 0xffffffff) },
		func(b []byte) { binary.LittleEndian.PutUint32(b[40:], 0xff) },
		func(b []byte) { binary.LittleEndian.PutUint32(b[52:], 1) },
	} {
		raw := append([]byte{}, valid...)
		mutate(raw)
		if _, err := decodeScreenshotDIB(raw); err == nil {
			t.Fatal("invalid DIB accepted")
		}
	}
}

func TestScreenshotPNGStorageCancelAndDraftIsolation(t *testing.T) {
	root := t.TempDir()
	img, _ := decodeScreenshotDIB(screenshotDIBFixture(24, false, 40))
	a, err := saveDraftScreenshot(root, img)
	if err != nil {
		t.Fatal(err)
	}
	b, err := saveDraftScreenshot(root, img)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.Path == b.Path {
		t.Fatal("separate pasted images collided")
	}
	raw, err := os.ReadFile(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeScreenshotPNG(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds() != img.Bounds() || imagecolor.NRGBAModel.Convert(decoded.At(2, 1)) != imagecolor.NRGBAModel.Convert(img.At(2, 1)) {
		t.Fatal("PNG snapshot changed")
	}
	if _, err = decodeScreenshotPNG([]byte("invalid")); err == nil {
		t.Fatal("invalid PNG accepted")
	}
	drafts := map[string][]draftPicture{"A": {a}, "B": {b}}
	drafts["A"] = withoutDraftPicture(drafts["A"], a.ID)
	removeDraftScreenshot(root, a)
	if fileExists(a.Path) || !fileExists(b.Path) || len(drafts["B"]) != 1 {
		t.Fatal("cancel crossed draft boundaries")
	}
	outside := filepath.Join(t.TempDir(), "keep.png")
	os.WriteFile(outside, raw, 0600)
	removeDraftScreenshot(root, draftPicture{b.ID, outside})
	if !fileExists(outside) {
		t.Fatal("cleanup escaped screenshot directory")
	}
	var encoded bytes.Buffer
	if err = png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err = decodeScreenshotPNG(encoded.Bytes()); err != nil {
		t.Fatal(err)
	}
}
