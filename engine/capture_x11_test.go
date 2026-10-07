package main

import (
	"testing"

	"github.com/jezek/xgb/xproto"
)

func TestXWireFormat(t *testing.T) {
	fmts := []xproto.Format{
		{Depth: 24, BitsPerPixel: 32, ScanlinePad: 32},
		{Depth: 1, BitsPerPixel: 1, ScanlinePad: 32},
	}
	// 32bpp @ depth 24, width 5: stride 20, already pad-aligned.
	bpp, stride, err := xWireFormat(fmts, 24, 5)
	if err != nil || bpp != 4 || stride != 20 {
		t.Fatalf("bpp=%d stride=%d err=%v", bpp, stride, err)
	}
	// 24bpp packed, width 5: stride 15 -> pads to 16.
	fmts[0].BitsPerPixel = 24
	bpp, stride, err = xWireFormat(fmts, 24, 5)
	if err != nil || bpp != 3 || stride != 16 {
		t.Fatalf("bpp=%d stride=%d err=%v — 24bpp must pad scanlines", bpp, stride, err)
	}
	// Depth with no matching format, or a non-BGR layout, is rejected.
	if _, _, err := xWireFormat(fmts, 16, 5); err == nil {
		t.Fatal("unmatched depth should error")
	}
	fmts[0].BitsPerPixel = 16
	if _, _, err := xWireFormat(fmts, 24, 5); err == nil {
		t.Fatal("16bpp should be rejected, not guessed")
	}
	if _, _, err := xWireFormat(fmts, 24, 0); err == nil {
		t.Fatal("zero width should error")
	}
}

func TestXImageToRGBA(t *testing.T) {
	// 2x1 32bpp BGRX: blue=0x11, green=0x22, red=0x33 on both pixels.
	data := []byte{0x11, 0x22, 0x33, 0xff, 0x44, 0x55, 0x66, 0xff}
	rgba, err := xImageToRGBA(data, 2, 1, 4, 8)
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, a := rgba.At(0, 0).RGBA(); r>>8 != 0x33 || g>>8 != 0x22 || b>>8 != 0x11 || a>>8 != 0xff {
		t.Fatalf("pixel0 %#v — BGRX not unswizzled", rgba.At(0, 0))
	}
	if r, _, _, _ := rgba.At(1, 0).RGBA(); r>>8 != 0x66 {
		t.Fatalf("pixel1 red %#v", rgba.At(1, 0))
	}

	// Padded stride: 2x1 24bpp row is 6 bytes padded to 8 — the pad must
	// not be read as a third pixel's blue.
	data = []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x00, 0x00}
	rgba, err = xImageToRGBA(data, 2, 1, 3, 8)
	if err != nil {
		t.Fatal(err)
	}
	if r, _, _, _ := rgba.At(1, 0).RGBA(); r>>8 != 0x66 {
		t.Fatalf("24bpp pixel1 red %#v — stride/pad mishandled", rgba.At(1, 0))
	}

	// Short payload is an error, not a partial read.
	if _, err := xImageToRGBA(data[:4], 2, 1, 3, 8); err == nil {
		t.Fatal("short payload should error")
	}
}
