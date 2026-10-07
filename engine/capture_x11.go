package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"image"
	"image/jpeg"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// x11Backend captures the X11 root window via pure-Go wire protocol (xgb —
// no Xlib/cgo, the engine binary stays CGO_ENABLED=0). Selected when
// WAYLAND_DISPLAY is unset and DISPLAY is set. The connection opens lazily
// on first Grab so backend resolution is testable without an X server.
type x11Backend struct {
	jpegQuality int
	conn        *xgb.Conn
}

func (b *x11Backend) Name() string             { return "x11" }
func (b *x11Backend) NeedsWaylandSocket() bool { return false }

func (b *x11Backend) connect() error {
	if b.conn != nil {
		return nil
	}
	conn, err := xgb.NewConn()
	if err != nil {
		return fmt.Errorf("x11 connect: %w", err)
	}
	b.conn = conn
	return nil
}

func (b *x11Backend) Grab(_ *sql.DB) ([]byte, error) {
	if err := b.connect(); err != nil {
		return nil, err
	}
	// Screen geometry is re-read per grab — the setup reply is a cached
	// getter, and a cached root shrinks silently under xrandr where a
	// grown root errors and reconnects anyway.
	s := xproto.Setup(b.conn).DefaultScreen(b.conn)
	img, err := xproto.GetImage(b.conn, xproto.ImageFormatZPixmap,
		xproto.Drawable(s.Root), 0, 0,
		s.WidthInPixels, s.HeightInPixels, ^uint32(0)).Reply()
	if err != nil {
		// A dead server kills the conn — drop it so the next grab
		// reconnects instead of failing on a poisoned socket forever.
		b.conn.Close()
		b.conn = nil
		return nil, fmt.Errorf("x11 getimage: %w", err)
	}
	bpp, stride, err := xWireFormat(xproto.Setup(b.conn).PixmapFormats, img.Depth, int(s.WidthInPixels))
	if err != nil {
		return nil, err
	}
	rgba, err := xImageToRGBA(img.Data, int(s.WidthInPixels), int(s.HeightInPixels), bpp, stride)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: b.jpegQuality}); err != nil {
		return nil, fmt.Errorf("x11 jpeg encode: %w", err)
	}
	return buf.Bytes(), nil
}

func (b *x11Backend) Close() error {
	if b.conn != nil {
		b.conn.Close()
		b.conn = nil
	}
	return nil
}

// xWireFormat resolves the wire layout for a GetImage reply: bytes per
// pixel and the padded scanline stride from the server's PixmapFormats
// entry for the reply depth. Inferring bpp from total payload length
// misreads odd-width frames because scanlines pad to a boundary.
func xWireFormat(fmts []xproto.Format, depth uint8, w int) (bpp, stride int, err error) {
	if w <= 0 {
		return 0, 0, fmt.Errorf("x11 frame: empty width %d", w)
	}
	var pad int
	for _, f := range fmts {
		if f.Depth == depth {
			bpp, pad = int(f.BitsPerPixel)/8, int(f.ScanlinePad)/8
			break
		}
	}
	if bpp != 3 && bpp != 4 {
		return 0, 0, fmt.Errorf("x11 frame: unsupported depth %d (bpp %d)", depth, bpp)
	}
	stride = w * bpp
	if rem := stride % pad; rem != 0 {
		stride += pad - rem
	}
	return bpp, stride, nil
}

// xImageToRGBA decodes a ZPixmap payload given its wire layout. TrueColor
// roots are almost always 32bpp BGRX; 24bpp BGR packed appears on some
// drivers — both are little-endian B,G,R[,X].
func xImageToRGBA(data []byte, w, h, bpp, stride int) (*image.RGBA, error) {
	if len(data) < stride*h {
		return nil, fmt.Errorf("x11 frame: short payload %d bytes for %dx%d@%dbpp", len(data), w, h, bpp*8)
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := data[y*stride:]
		for x := 0; x < w; x++ {
			s, o := x*bpp, (y*w+x)*4
			out.Pix[o+0] = row[s+2] // R from B,G,R[,X] little-endian
			out.Pix[o+1] = row[s+1]
			out.Pix[o+2] = row[s+0]
			out.Pix[o+3] = 0xff
		}
	}
	return out, nil
}
