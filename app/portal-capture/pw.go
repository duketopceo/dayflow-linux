package main

/*
#cgo pkg-config: libpipewire-0.3
#include "pwbridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"image"
	"log"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// Go-side state for each PipeWire node: the most recent decoded frame and
// the negotiated format. The C process callback delivers raw spans; we copy
// into an image.RGBA immediately so the PipeWire buffer can be requeued.
type nodeFrame struct {
	mu   sync.Mutex
	img  *image.RGBA
	w, h int
	// fresh gates decode cost: set on decode, cleared when compose()
	// consumes the frame. Buffers arriving while a frame is still
	// unconsumed are skipped — at ~1fps emit cadence most would be
	// discarded anyway.
	fresh bool
}

var (
	framesMu sync.Mutex
	frames   = map[int]*nodeFrame{}

	streamAlive = map[int]bool{}
	streamMu    sync.Mutex

	// stopping suppresses the state callback during intentional teardown —
	// quitting the loop transitions streams through error/unconnected and
	// would otherwise emit a spurious "stream-dead" on a clean exit.
	stopping atomic.Bool
)

func nodeState(node int) *nodeFrame {
	framesMu.Lock()
	defer framesMu.Unlock()
	f, ok := frames[node]
	if !ok {
		f = &nodeFrame{}
		frames[node] = f
	}
	return f
}

//export goStreamFrame
func goStreamFrame(node C.int, w, h, stride, format C.int, data unsafe.Pointer, length C.int) {
	if w <= 0 || h <= 0 || data == nil {
		return
	}
	f := nodeState(int(node))
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fresh {
		// Unconsumed frame still queued — skip the multi-MB copy+swizzle;
		// the C side requeues the buffer either way.
		return
	}
	// Only BGRA is offered in stream negotiation (pwbridge.c), so the
	// negotiated format is always 4 bytes/pixel B,R,G,A — swap B and R.
	const bpp = 4
	rowLen := int(w) * bpp
	s := int(stride)
	if s < rowLen {
		s = rowLen
	}
	// Zero-copy view of the PipeWire span — GoBytes would add a full
	// multi-MB copy before the per-row copy below; the buffer is valid
	// until this callback returns.
	span := int(length)
	if max := s * int(h); span > max {
		span = max
	}
	if span < 0 {
		span = 0
	}
	src := unsafe.Slice((*byte)(data), span)
	if f.img == nil || f.w != int(w) || f.h != int(h) {
		f.img = image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
		f.w, f.h = int(w), int(h)
	}
	for y := 0; y < int(h); y++ {
		srcOff := y * s
		dstOff := y * f.img.Stride
		if srcOff+rowLen > len(src) || dstOff+rowLen > len(f.img.Pix) {
			break
		}
		row := src[srcOff : srcOff+rowLen]
		dst := f.img.Pix[dstOff : dstOff+rowLen]
		// BGRA/BGRx → RGBA: swap B and R.
		for x := 0; x < rowLen; x += bpp {
			dst[x+0] = row[x+2]
			dst[x+1] = row[x+1]
			dst[x+2] = row[x+0]
			dst[x+3] = 255
		}
	}
	f.fresh = true
}

//export goStreamFormat
func goStreamFormat(node, w, h C.int) {
	log.Printf("stream node %d format %dx%d", int(node), int(w), int(h))
}

//export goStreamState
func goStreamState(node, state C.int, msg *C.char) {
	// pw_stream_state: 0 error, 1 unconnected, 2 connecting, 3 paused,
	// 4 streaming. Paused is NOT death — producers pause idle streams; only
	// the error state means the stream is gone.
	st := int(state)
	if stopping.Load() {
		return
	}
	streamMu.Lock()
	streamAlive[int(node)] = st != 0
	streamMu.Unlock()
	if st == 0 {
		log.Printf("stream node %d error: %s", int(node), C.GoString(msg))
		emitStatus("stream-dead")
	} else if st == 4 {
		log.Printf("stream node %d streaming", int(node))
	}
}

// pipewireRun owns the C bridge lifetime: connect on the portal fd, add one
// stream per node, run the main loop until stopped.
type pipewireSession struct {
	b    *C.pw_bridge
	done chan struct{}
}

func pipewireStart(fd int, nodes []uint32) (*pipewireSession, error) {
	C.pw_bridge_init()
	b := C.pw_bridge_connect(C.int(fd))
	if b == nil {
		return nil, fmt.Errorf("pw_context_connect_fd failed")
	}
	s := &pipewireSession{b: b, done: make(chan struct{})}
	for _, n := range nodes {
		if C.pw_bridge_add_stream(b, C.uint32_t(n)) != 0 {
			C.pw_bridge_free(b)
			return nil, fmt.Errorf("stream connect failed for node %d", n)
		}
	}
	return s, nil
}

func (s *pipewireSession) run() {
	C.pw_bridge_run(s.b)
	close(s.done)
}

// shutdown stops the main loop, waits for its thread to return (libpipewire
// demands destroy calls happen outside loop context), then frees.
// pw_main_loop_run re-arms its running flag on entry, so a quit landing
// before run() starts is silently dropped — retry until the loop actually
// returns. Bounded: if the loop never exits, the caller is on a die() path
// and process teardown reaps it anyway.
func (s *pipewireSession) shutdown() {
	stopping.Store(true)
	deadline := time.Now().Add(2 * time.Second)
	for {
		C.pw_bridge_stop(s.b)
		select {
		case <-s.done:
			C.pw_bridge_free(s.b)
			return
		case <-time.After(20 * time.Millisecond):
			if time.Now().After(deadline) {
				return
			}
		}
	}
}
