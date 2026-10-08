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
// node's frame is consumed (fresh cleared) so an unchanged screen returns
// nil — the caller re-emits the cached JPEG instead of re-encoding
// identical pixels. When the portal reported screen positions, streams
// land at their real coordinates (bounding-box canvas); otherwise they
// stitch horizontally in stream order, matching grim's composite
// semantics. Portal positions are compositor layout coordinates — they can
// be negative (a monitor left of/above the origin), so the canvas is
// normalized by the minimum offset, not anchored at (0,0).
func compose(slots []streamSlot) *image.RGBA {
	type placed struct {
		src    *nodeFrame
		x, y   int
		hasPos bool
	}
	// Stale images stay composited — a quiet node keeps its last content
	// (otherwise one fresh stream would blank the other monitors). Only
	// "no node has anything new" returns nil.
	var items []placed
	minX, minY, maxX, maxY := 0, 0, 0, 0
	xCursor := 0
	anyFresh := false
	for _, s := range slots {
		f := nodeState(int(s.node))
		f.mu.Lock()
		img, fresh := f.img, f.fresh
		f.mu.Unlock()
		if img == nil {
			continue
		}
		if fresh {
			anyFresh = true
		}
		p := placed{src: f, x: s.x, y: s.y, hasPos: s.hasPos}
		if !p.hasPos {
			p.x, p.y = xCursor, 0
			xCursor += img.Bounds().Dx()
		}
		if p.x < minX || len(items) == 0 {
			minX = p.x
		}
		if p.y < minY || len(items) == 0 {
			minY = p.y
		}
		if p.x+img.Bounds().Dx() > maxX {
			maxX = p.x + img.Bounds().Dx()
		}
		if p.y+img.Bounds().Dy() > maxY {
			maxY = p.y + img.Bounds().Dy()
		}
		items = append(items, p)
	}
	w, h := maxX-minX, maxY-minY
	if !anyFresh || len(items) == 0 || w <= 0 || h <= 0 {
		return nil
	}
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	for _, p := range items {
		// Held across the draw so goStreamFrame can't overwrite Pix
		// mid-copy; ms-scale at 1fps emit cadence. Re-read img under this
		// lock — a frame decoded between the passes is drawn now instead
		// of having its fresh flag silently consumed.
		p.src.mu.Lock()
		if img := p.src.img; img != nil {
			draw.Draw(canvas,
				image.Rect(p.x-minX, p.y-minY,
					p.x-minX+img.Bounds().Dx(), p.y-minY+img.Bounds().Dy()),
				img, image.Point{}, draw.Src)
		}
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
func emitFrame(sink io.Writer, payload []byte) error {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := sink.Write(hdr[:]); err != nil {
		return err
	}
	_, err := sink.Write(payload)
	return err
}
