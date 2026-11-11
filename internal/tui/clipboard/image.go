// Package clipboard reads user-requested image input from the local desktop.
package clipboard

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
)

var ErrUnsupported = errors.New("clipboard images are supported on Windows; attach a file with @path instead")

const MaxPixels = 32 * 1024 * 1024
const MaxBytes = 20 << 20

// PNG validates dimensions before decoding and strips unrelated clipboard metadata.
func PNG(data []byte) ([]byte, error) {
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("clipboard image exceeds 20 MiB")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return nil, fmt.Errorf("clipboard image exceeds 32 megapixels")
	}
	im, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return encode(im)
}
func encode(im image.Image) ([]byte, error) {
	var b bytes.Buffer
	err := png.Encode(&b, im)
	if err != nil {
		return nil, err
	}
	if b.Len() > MaxBytes {
		return nil, fmt.Errorf("clipboard image exceeds 20 MiB")
	}
	return b.Bytes(), nil
}

// DIB supports the uncompressed 24/32-bit Windows screenshot formats.
func DIB(data []byte) ([]byte, error) {
	if len(data) < 40 {
		return nil, fmt.Errorf("truncated clipboard bitmap")
	}
	u16 := func(n int) uint16 { return binary.LittleEndian.Uint16(data[n : n+2]) }
	u32 := func(n int) uint32 { return binary.LittleEndian.Uint32(data[n : n+4]) }
	header := int(u32(0))
	w, h := int(int32(u32(4))), int(int32(u32(8)))
	top := h < 0
	if top {
		h = -h
	}
	bits, compression := int(u16(14)), u32(16)
	if header < 40 || header > len(data) || w <= 0 || h <= 0 || int64(w)*int64(h) > MaxPixels || u16(12) != 1 || (bits != 24 && bits != 32) || (compression != 0 && compression != 3) {
		return nil, fmt.Errorf("unsupported clipboard bitmap; copy a PNG or a standard screenshot")
	}
	offset := header
	if compression == 3 {
		if bits != 32 || len(data) < 52 {
			return nil, fmt.Errorf("unsupported clipboard bitmap masks")
		}
		if u32(40) != 0x00ff0000 || u32(44) != 0x0000ff00 || u32(48) != 0x000000ff {
			return nil, fmt.Errorf("unsupported clipboard bitmap masks")
		}
		if header == 40 {
			offset += 12
		}
	}
	// True-color DIBs may still carry an optional color table.
	colors := uint64(u32(32))
	if colors > 256 {
		return nil, fmt.Errorf("invalid clipboard palette")
	}
	offset += int(colors) * 4
	stride := ((w*bits + 31) / 32) * 4
	if int64(offset)+int64(stride)*int64(h) > int64(len(data)) {
		return nil, fmt.Errorf("truncated clipboard pixels")
	}
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		srcY := y
		if !top {
			srcY = h - 1 - y
		}
		for x := 0; x < w; x++ {
			src := offset + srcY*stride + x*(bits/8)
			dst := y*im.Stride + x*4
			im.Pix[dst] = data[src+2]
			im.Pix[dst+1] = data[src+1]
			im.Pix[dst+2] = data[src]
			im.Pix[dst+3] = 255
		}
	}
	return encode(im)
}
