package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
)

// compose builds the composite frame from the latest per-node images. Each
// node's frame is consumed (ownership transferred, fresh cleared) so an
// unchanged screen returns nil — the caller re-emits the cached JPEG
// instead of re-encoding identical pixels. When the portal reported screen
// positions, streams land at their real coordinates (bounding-box canvas);
// otherwise they stitch horizontally in stream order, matching grim's
// composite semantics.
func compose(slots []streamSlot) *image.RGBA {
	type placed struct {
		img    *image.RGBA
		src    *nodeFrame
		x, y   int
		hasPos bool
	}
	// Stale images stay composited — a quiet node keeps its last content
	// (otherwise one fresh stream would blank the other monitors). Only
	// "no node has anything new" returns nil.
	var items []placed
	maxX, maxY := 0, 0
	xCursor := 0
	anyFresh := false
	for _, s := range slots {
		f := nodeState(int(s.node))
		f.mu.Lock()
		img := f.img
		if f.fresh {
			anyFresh = true
		}
		f.mu.Unlock()
		if img == nil {
			continue
		}
		p := placed{img: img, src: f, x: s.x, y: s.y, hasPos: s.hasPos}
		if !p.hasPos {
			p.x, p.y = xCursor, 0
			xCursor += img.Bounds().Dx()
		}
		if p.x+img.Bounds().Dx() > maxX {
			maxX = p.x + img.Bounds().Dx()
		}
		if p.y+img.Bounds().Dy() > maxY {
			maxY = p.y + img.Bounds().Dy()
		}
		items = append(items, p)
	}
	if !anyFresh || len(items) == 0 || maxX == 0 || maxY == 0 {
		return nil
	}
	canvas := image.NewRGBA(image.Rect(0, 0, maxX, maxY))
	for _, p := range items {
		// Held across the draw so goStreamFrame can't overwrite Pix
		// mid-copy; ms-scale at 1fps emit cadence.
		p.src.mu.Lock()
		draw.Draw(canvas,
			image.Rect(p.x, p.y, p.x+p.img.Bounds().Dx(), p.y+p.img.Bounds().Dy()),
			p.img, image.Point{}, draw.Src)
		p.src.fresh = false
		p.src.mu.Unlock()
	}
	return canvas
}

// downscale bounds the longer edge to maxDim, preserving aspect ratio —
// the same semantics the engine applies on the grim/X11 paths
// (frame_max_dim). Nearest-neighbor is plenty: frames exist for vision
// summarization, not viewing.
func downscale(src *image.RGBA, maxDim int) *image.RGBA {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	long := max(sw, sh)
	if maxDim <= 0 || long <= maxDim {
		return src
	}
	scale := float64(long) / float64(maxDim)
	dw, dh := max(1, int(float64(sw)/scale)), max(1, int(float64(sh)/scale))
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		sy := int(float64(y) * scale)
		for x := 0; x < dw; x++ {
			sx := int(float64(x) * scale)
			so := sy*src.Stride + sx*4
			do := y*dst.Stride + x*4
			copy(dst.Pix[do:do+4], src.Pix[so:so+4])
		}
	}
	return dst
}

// encodeFrame JPEG-encodes a composite; the caller caches the bytes for
// re-emission when the screen is unchanged.
func encodeFrame(img *image.RGBA, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// emitFrame writes one length-prefixed JPEG to the frame sink (stdout in
// main; injectable for tests).
func emitFrame(sink io.Writer, jpeg []byte) error {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(jpeg)))
	if _, err := sink.Write(hdr[:]); err != nil {
		return err
	}
	_, err := sink.Write(jpeg)
	return err
}
