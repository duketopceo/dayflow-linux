package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"testing"
)

func rgbaFill(w, h int, r, g, b uint8) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+0], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = r, g, b, 255
	}
	return img
}

func seedNode(t *testing.T, node int, img *image.RGBA) *nodeFrame {
	t.Helper()
	f := nodeState(node)
	f.mu.Lock()
	f.img, f.fresh = img, true
	f.mu.Unlock()
	t.Cleanup(func() {
		f.mu.Lock()
		f.img, f.fresh = nil, false
		f.mu.Unlock()
	})
	return f
}

func TestComposeUnpositionedStitchesHorizontally(t *testing.T) {
	seedNode(t, 101, rgbaFill(10, 5, 255, 0, 0))
	seedNode(t, 102, rgbaFill(6, 8, 0, 255, 0))
	img := compose([]streamSlot{{node: 101}, {node: 102}})
	if img == nil {
		t.Fatal("compose returned nil with fresh frames")
	}
	if img.Bounds().Dx() != 16 || img.Bounds().Dy() != 8 {
		t.Fatalf("canvas = %v, want 16x8 (side-by-side)", img.Bounds())
	}
	if img.RGBAAt(0, 0).R != 255 || img.RGBAAt(10, 0).G != 255 {
		t.Fatal("streams not stitched in order")
	}
}

func TestComposeNegativePositionsAreNormalized(t *testing.T) {
	// A monitor left of the origin reports x=-10; without normalization
	// draw clips it entirely (regression: silent monitor loss).
	seedNode(t, 201, rgbaFill(10, 5, 255, 0, 0))
	seedNode(t, 202, rgbaFill(10, 5, 0, 0, 255))
	slots := []streamSlot{
		{node: 201, x: -10, y: 0, hasPos: true},
		{node: 202, x: 0, y: 0, hasPos: true},
	}
	img := compose(slots)
	if img == nil {
		t.Fatal("compose returned nil")
	}
	if img.Bounds().Dx() != 20 || img.Bounds().Dy() != 5 {
		t.Fatalf("canvas = %v, want 20x5 after origin normalization", img.Bounds())
	}
	if img.RGBAAt(0, 0).R != 255 {
		t.Fatal("negative-position monitor clipped instead of normalized")
	}
	if img.RGBAAt(10, 0).B != 255 {
		t.Fatal("right monitor misplaced after normalization")
	}
}

func TestComposeNilWithoutFreshFrames(t *testing.T) {
	f := seedNode(t, 301, rgbaFill(4, 4, 1, 2, 3))
	f.mu.Lock()
	f.fresh = false // consumed earlier — nothing new to encode
	f.mu.Unlock()
	if img := compose([]streamSlot{{node: 301}}); img != nil {
		t.Fatal("stale-only composite should return nil (caller re-emits cache)")
	}
}

func TestComposeConsumesFresh(t *testing.T) {
	f := seedNode(t, 401, rgbaFill(4, 4, 9, 9, 9))
	compose([]streamSlot{{node: 401}})
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fresh {
		t.Fatal("compose must clear fresh on consumed frames")
	}
}

func TestDownscaleBoundsLongerEdge(t *testing.T) {
	src := rgbaFill(4000, 1000, 0, 0, 0)
	dst := downscale(src, 1000)
	if dst.Bounds().Dx() != 1000 || dst.Bounds().Dy() != 250 {
		t.Fatalf("downscale = %v, want 1000x250", dst.Bounds())
	}
	if d := downscale(src, 0); d.Bounds().Dx() != 4000 {
		t.Fatal("maxDim 0 must keep native size")
	}
}

func TestEmitFrameFraming(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte{0xFF, 0xD8, 0xAA}
	if err := emitFrame(&buf, payload); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if len(b) != 7 || binary.BigEndian.Uint32(b[:4]) != 3 {
		t.Fatalf("framing wrong: %x", b)
	}
	if !bytes.Equal(b[4:], payload) {
		t.Fatal("payload bytes mangled")
	}
}
