package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
	"image/jpeg"
	"sync"
)

// compose builds the composite frame from the latest per-node images.
// When the portal reported screen positions, streams land at their real
// coordinates (bounding-box canvas); otherwise they stitch horizontally in
// stream order, matching grim's composite semantics.
func compose(slots []streamSlot) *image.RGBA {
	type placed struct {
		img    *image.RGBA
		x, y   int
		hasPos bool
	}
	var placed_ []placed
	maxX, maxY := 0, 0
	xCursor := 0
	for _, s := range slots {
		f := nodeState(int(s.node))
		f.mu.Lock()
		if !f.fresh || f.img == nil {
			f.mu.Unlock()
			continue
		}
		cp := image.NewRGBA(f.img.Bounds())
		copy(cp.Pix, f.img.Pix)
		f.mu.Unlock()
		p := placed{img: cp, x: s.x, y: s.y, hasPos: s.hasPos}
		if !p.hasPos {
			p.x, p.y = xCursor, 0
			xCursor += cp.Bounds().Dx()
		}
		if p.x+cp.Bounds().Dx() > maxX {
			maxX = p.x + cp.Bounds().Dx()
		}
		if p.y+cp.Bounds().Dy() > maxY {
			maxY = p.y + cp.Bounds().Dy()
		}
		placed_ = append(placed_, p)
	}
	if len(placed_) == 0 || maxX == 0 || maxY == 0 {
		return nil
	}
	canvas := image.NewRGBA(image.Rect(0, 0, maxX, maxY))
	for _, p := range placed_ {
		draw.Draw(canvas,
			image.Rect(p.x, p.y, p.x+p.img.Bounds().Dx(), p.y+p.img.Bounds().Dy()),
			p.img, image.Point{}, draw.Src)
	}
	return canvas
}

// downscale halves the image repeatedly until the longer edge fits maxDim —
// cheap and plenty at capture cadence (no interpolation needed for text
// that OCR/vision reads at summary time anyway).
func downscale(src *image.RGBA, maxDim int) *image.RGBA {
	if maxDim <= 0 {
		return src
	}
	for src.Bounds().Dx() > maxDim || src.Bounds().Dy() > maxDim {
		w, h := src.Bounds().Dx()/2, src.Bounds().Dy()/2
		if w <= 0 || h <= 0 {
			break
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(x, y, src.At(x*2, y*2))
			}
		}
		src = dst
	}
	return src
}

var frameSeq uint64
var frameSeqMu sync.Mutex

// writeFrame emits one length-prefixed JPEG to the frame sink (stdout in
// main; injectable for tests).
func writeFrame(sink interface{ Write([]byte) (int, error) }, img *image.RGBA, quality int) error {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return err
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(buf.Len()))
	if _, err := sink.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := sink.Write(buf.Bytes()); err != nil {
		return err
	}
	frameSeqMu.Lock()
	frameSeq++
	frameSeqMu.Unlock()
	return nil
}
