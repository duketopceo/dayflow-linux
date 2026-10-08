// dayflow-portal — supervised capture helper for xdg-desktop-portal
// ScreenCast sessions. Contract with the engine supervisor:
//
//	stdout: length-prefixed JPEG frames ([u32 BE len][jpeg]) at ~1fps
//	stderr: log lines + structured status lines, one per line:
//	  status consent-needed | streaming | denied | stream-dead | parked
//	  token <restore_token>        — persist for silent re-Start
//
// Exit codes: 0 clean stop, 2 consent denied/dismissed, 3 stream dead —
// everything else (including 4, portal/bus unreachable) is left to the
// supervisor's restart policy.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	exitDenied     = 2
	exitStreamDead = 3
	exitFatal      = 4
)

func emitStatus(s string) { fmt.Fprintf(os.Stderr, "status %s\n", s) }
func emitToken(t string)  { fmt.Fprintf(os.Stderr, "token %s\n", t) }

// die logs, emits the terminal status, tears down PipeWire, exits.
func die(pw *pipewireSession, status string, code int, err error) {
	log.Printf("%v", err)
	emitStatus(status)
	if pw != nil {
		pw.shutdown()
	}
	os.Exit(code)
}

func main() {
	var (
		quality = flag.Int("jpeg-quality", 55, "JPEG quality 1-100")
		maxDim  = flag.Int("max-dim", 1920, "bound longer edge; 0 = native")
		period  = flag.Duration("period", time.Second, "frame emit cadence")
		token   = flag.String("token", "", "portal restore_token for silent re-Start")
		once    = flag.Bool("once", false, "capture one frame and exit (smoke test)")
		timeout = flag.Duration("timeout", 30*time.Second, "give up if no frame arrives")
	)
	flag.Parse()
	log.SetOutput(os.Stderr)

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		die(nil, "parked", exitFatal, fmt.Errorf("session bus: %w", err))
	}
	defer conn.Close()

	emitStatus("consent-needed")
	sess, err := openPortalSession(conn, *token)
	var denied portalDenied
	if errors.As(err, &denied) {
		die(nil, "denied", exitDenied, err)
	}
	if err != nil {
		die(nil, "parked", exitFatal, fmt.Errorf("portal session: %w", err))
	}
	if sess.token != "" && sess.token != *token {
		emitToken(sess.token)
	}
	log.Printf("portal session up: %d stream(s)", len(sess.slots))

	fd, err := openPipeWireFD(conn, sess.session)
	if err != nil {
		die(nil, "stream-dead", exitStreamDead, fmt.Errorf("pipewire remote: %w", err))
	}
	pw, err := pipewireStart(fd, sess.nodeIDs)
	if err != nil {
		die(nil, "stream-dead", exitStreamDead, fmt.Errorf("pipewire: %w", err))
	}
	go pw.run()
	defer pw.shutdown()

	emitStatus("streaming")

	out := bufio.NewWriter(os.Stdout)
	sigs := sigChan()
	deadline := time.Now().Add(*timeout)
	grabbed := false
	var lastJPEG []byte
	for {
		if img := compose(sess.slots); img != nil {
			img = downscale(img, *maxDim)
			jpeg, err := encodeFrame(img, *quality)
			if err != nil {
				die(pw, "stream-dead", exitStreamDead, fmt.Errorf("encode frame: %w", err))
			}
			lastJPEG = jpeg
			grabbed = true
		}
		// Emit every period even when nothing changed (compose nil) — the
		// supervisor's per-grab deadline assumes a live stream; a silent
		// helper on a static screen would read as capture errors.
		if lastJPEG != nil {
			if err := emitFrame(out, lastJPEG); err != nil {
				die(pw, "stream-dead", exitStreamDead, fmt.Errorf("write frame: %w", err))
			}
			if err := out.Flush(); err != nil {
				die(pw, "stream-dead", exitStreamDead, fmt.Errorf("flush: %w", err))
			}
			if *once {
				return
			}
		}
		if !grabbed && time.Now().After(deadline) {
			die(pw, "stream-dead", exitStreamDead, fmt.Errorf("no frame within %s", *timeout))
		}
		select {
		case <-time.After(*period):
		case sig := <-sigs:
			log.Printf("stopping on %s", sig)
			emitStatus("parked")
			return
		}
		// a stream in the error state is dead → exit so the supervisor
		// can restart us with a fresh session (the state callback already
		// emitted stream-dead)
		streamMu.Lock()
		dead := false
		for _, alive := range streamAlive {
			if !alive {
				dead = true
			}
		}
		streamMu.Unlock()
		if dead {
			log.Printf("stream died — exiting for restart")
			pw.shutdown()
			os.Exit(exitStreamDead)
		}
	}
}

func sigChan() <-chan os.Signal {
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	return ch
}
