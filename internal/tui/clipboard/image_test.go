package clipboard

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestDIBScreenshotFormats(t *testing.T) {
	for _, bits := range []int{24, 32} {
		for _, top := range []bool{false, true} {
			stride := ((2*bits + 31) / 32) * 4
			data := make([]byte, 40+stride*2)
			put := func(n int, v uint32) { binary.LittleEndian.PutUint32(data[n:], v) }
			put(0, 40)
			put(4, 2)
			put(8, 2)
			if top {
				put(8, 0xfffffffe)
			}
			binary.LittleEndian.PutUint16(data[12:], 1)
			binary.LittleEndian.PutUint16(data[14:], uint16(bits))
			for y := 0; y < 2; y++ {
				for x := 0; x < 2; x++ {
					offset := 40 + y*stride + x*bits/8
					data[offset+2] = 255
				}
			}
			raw, err := DIB(data)
			if err != nil {
				t.Fatal(err)
			}
			im, err := png.Decode(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			r, g, b, a := im.At(1, 1).RGBA()
			if r != 65535 || g != 0 || b != 0 || a != 65535 {
				t.Fatalf("unexpected pixel %v", im.At(1, 1))
			}
		}
	}
}
func TestClipboardRejectsMalformedAndHugeImages(t *testing.T) {
	for _, data := range [][]byte{nil, make([]byte, 39), make([]byte, 40)} {
		if _, err := DIB(data); err == nil {
			t.Fatal("accepted invalid DIB")
		}
	}
	data := make([]byte, 40)
	binary.LittleEndian.PutUint32(data, 40)
	binary.LittleEndian.PutUint32(data[4:], 100000)
	binary.LittleEndian.PutUint32(data[8:], 100000)
	binary.LittleEndian.PutUint16(data[12:], 1)
	binary.LittleEndian.PutUint16(data[14:], 32)
	if _, err := DIB(data); err == nil {
		t.Fatal("accepted huge image")
	}
	if _, err := PNG([]byte("not png")); err == nil {
		t.Fatal("accepted invalid PNG")
	}
}
func TestPNGPreservesPixels(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	im.Set(1, 1, color.NRGBA{10, 20, 30, 255})
	var b bytes.Buffer
	png.Encode(&b, im)
	raw, err := PNG(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds() != im.Bounds() || color.NRGBAModel.Convert(got.At(1, 1)) != im.At(1, 1) {
		t.Fatal("pixels changed")
	}
}
