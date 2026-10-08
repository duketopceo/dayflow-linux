package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	xdraw "golang.org/x/image/draw"
)

// x11Backend captures the X11 root window via pure-Go wire protocol (xgb —
// no Xlib/cgo, the engine binary stays CGO_ENABLED=0). Selected when
// WAYLAND_DISPLAY is unset and DISPLAY is set. The connection opens lazily
// on first Grab so backend resolution is testable without an X server.
type x11Backend struct {
	jpegQuality int
	frameMaxDim int
	conn        *xgb.Conn
}

func (b *x11Backend) Name() string             { return "x11" }
func (b *x11Backend) NeedsWaylandSocket() bool { return false }

// dropConn releases a poisoned connection so the next grab reconnects
// instead of failing on a dead socket forever.
func (b *x11Backend) dropConn() {
	if b.conn != nil {
		b.conn.Close()
		b.conn = nil
	}
}

func (b *x11Backend) connect() error {
	if b.conn != nil {
		return nil
	}
	// NewConn blocks on the socket handshake — a remote TCP DISPLAY waits
	// on kernel retransmit (~2min) otherwise, wedging the capture tick.
	type res struct {
		conn *xgb.Conn
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := xgb.NewConn()
		ch <- res{c, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return fmt.Errorf("x11 connect: %w", r.err)
		}
		b.conn = r.conn
		return nil
	case <-time.After(grabFrameTimeout):
		// Reap a late-arriving conn — NewConn can still succeed after the
		// deadline and nothing else reads ch, so without this each timed-out
		// tick leaks a socket plus xgb's reader goroutines.
		go func() {
			if r := <-ch; r.conn != nil {
				r.conn.Close()
			}
		}()
		return fmt.Errorf("x11 connect: timed out after %s", grabFrameTimeout)
	}
}

// x11MaxFrameBytes bounds the root geometry we will pull over the wire —
// the argv path caps subprocess output at 64MB and this keeps an absurd
// server-advertised root from allocating unboundedly.
const x11MaxFrameBytes = 256 << 20

func (b *x11Backend) Grab(_ *sql.DB) ([]byte, error) {
	if err := b.connect(); err != nil {
		return nil, err
	}
	// The root drawable comes from the connect-time setup reply (fixed);
	// its GEOMETRY must be re-queried per grab — xproto.Setup reparses the
	// cached setup bytes, so after an xrandr grow a cached width/height
	// silently captures only the top-left crop. GetGeometry is a live
	// round-trip.
	s := xproto.Setup(b.conn).DefaultScreen(b.conn)
	img, w, h, err := b.grabRoot(b.conn, s)
	if err != nil {
		return nil, err
	}
	bpp, stride, err := xWireFormat(xproto.Setup(b.conn).PixmapFormats, img.Depth, w)
	if err != nil {
		return nil, err
	}
	rgba, err := xImageToRGBA(img.Data, w, h, bpp, stride)
	if err != nil {
		return nil, err
	}
	// Downscale to frame_max_dim before encoding — storedFrame would
	// discard the full-res JPEG and re-encode after the same downscale,
	// so encoding at store size stores Grab's bytes verbatim instead.
	if b.frameMaxDim > 0 {
		long := w
		if h > w {
			long = h
		}
		if long > b.frameMaxDim {
			scale := float64(b.frameMaxDim) / float64(long)
			dw, dh := int(math.Round(float64(w)*scale)), int(math.Round(float64(h)*scale))
			if dw < 1 {
				dw = 1
			}
			if dh < 1 {
				dh = 1
			}
			dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
			xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), rgba, rgba.Bounds(), xdraw.Over, nil)
			rgba = dst
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: b.jpegQuality}); err != nil {
		return nil, fmt.Errorf("x11 jpeg encode: %w", err)
	}
	return buf.Bytes(), nil
}

// grabRoot runs the GetGeometry→GetImage pair and bounds the combined
// round-trip — Reply() waits on the conn's read channel with no deadline,
// and the argv backends all honor grabFrameTimeout, so an unresponsive X
// server must not park the whole capture loop (heartbeat, signals, and
// retention share it).
func (b *x11Backend) grabRoot(conn *xgb.Conn, s *xproto.ScreenInfo) (*xproto.GetImageReply, int, int, error) {
	type res struct {
		img  *xproto.GetImageReply
		w, h int
		err  error
	}
	ch := make(chan res, 1)
	// The worker uses the conn captured at call time, never b.conn — the
	// timeout path nils the field via dropConn while this goroutine is
	// still in-flight, and reading the field here would race and panic.
	go func() {
		geo, err := xproto.GetGeometry(conn, xproto.Drawable(s.Root)).Reply()
		if err != nil {
			ch <- res{err: fmt.Errorf("x11 getgeometry: %w", err)}
			return
		}
		w, h := int(geo.Width), int(geo.Height)
		if int64(w)*int64(h)*4 > x11MaxFrameBytes {
			ch <- res{err: fmt.Errorf("x11 frame: root geometry %dx%d exceeds cap", w, h)}
			return
		}
		img, err := xproto.GetImage(conn, xproto.ImageFormatZPixmap,
			xproto.Drawable(s.Root), 0, 0,
			uint16(w), uint16(h), ^uint32(0)).Reply()
		if err != nil {
			ch <- res{err: fmt.Errorf("x11 getimage: %w", err)}
			return
		}
		ch <- res{img: img, w: w, h: h}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			b.dropConn()
			return nil, 0, 0, r.err
		}
		return r.img, r.w, r.h, nil
	case <-time.After(grabFrameTimeout):
		b.dropConn() // unblocks the Reply goroutine's read
		return nil, 0, 0, fmt.Errorf("x11 grab: timed out after %s", grabFrameTimeout)
	}
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
	// pad < 8 bits divides to 0 — protocol-legal per the spec's bitfield
	// and server-controlled, so guard rather than crash on %0.
	if pad > 0 {
		if rem := stride % pad; rem != 0 {
			stride += pad - rem
		}
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
