// dayflow-portal — supervised capture helper for xdg-desktop-portal
// ScreenCast sessions. Contract with the engine supervisor:
//
//	stdout: length-prefixed JPEG frames ([u32 BE len][jpeg]) at ~1fps
//	stderr: log lines + structured status lines, one per line:
//	  status consent-needed | streaming | denied | stream-dead | parked
//	  token <restore_token>        — persist for silent re-Start
//
// Exit codes: 0 clean stop, 2 consent denied/dismissed, 3 stream dead,
// 4 portal/bus unreachable (fatal — restart policy decides).
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

const exitDenied = 2

func emitStatus(s string) { fmt.Fprintf(os.Stderr, "status %s\n", s) }
func emitToken(t string)  { fmt.Fprintf(os.Stderr, "token %s\n", t) }

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
		log.Printf("session bus: %v", err)
		emitStatus("parked")
		os.Exit(4)
	}
	defer conn.Close()

	emitStatus("consent-needed")
	sess, err := openPortalSession(conn, *token)
	var denied portalDenied
	if errors.As(err, &denied) {
		log.Printf("%v", err)
		emitStatus("denied")
		os.Exit(exitDenied)
	}
	if err != nil {
		log.Printf("portal session: %v", err)
		emitStatus("parked")
		os.Exit(4)
	}
	if sess.token != "" && sess.token != *token {
		emitToken(sess.token)
	}
	log.Printf("portal session up: %d stream(s)", len(sess.slots))

	fd, err := openPipeWireFD(conn, sess.session)
	if err != nil {
		log.Printf("pipewire remote: %v", err)
		emitStatus("stream-dead")
		os.Exit(3)
	}
	pw, err := pipewireStart(fd, sess.nodeIDs)
	if err != nil {
		log.Printf("pipewire: %v", err)
		emitStatus("stream-dead")
		os.Exit(3)
	}
	go pw.run()
	defer pw.shutdown()

	emitStatus("streaming")

	out := bufio.NewWriter(os.Stdout)
	deadline := time.Now().Add(*timeout)
	grabbed := false
	for {
		img := compose(sess.slots)
		if img != nil {
			img = downscale(img, *maxDim)
			if err := writeFrame(out, img, *quality); err != nil {
				log.Printf("write frame: %v", err)
				emitStatus("stream-dead")
				pw.shutdown()
				os.Exit(3)
			}
			out.Flush()
			grabbed = true
			if *once {
				return
			}
		}
		if !grabbed && time.Now().After(deadline) {
			log.Printf("no frame within %s", *timeout)
			emitStatus("stream-dead")
			pw.shutdown()
			os.Exit(3)
		}
		select {
		case <-time.After(*period):
		case sig := <-sigChan():
			log.Printf("stopping on %s", sig)
			emitStatus("parked")
			return
		}
		// a stream in the error state is dead → exit so the supervisor
		// can restart us with a fresh session
		streamMu.Lock()
		dead := false
		for _, alive := range streamAlive {
			if !alive {
				dead = true
			}
		}
		streamMu.Unlock()
		if dead {
			emitStatus("stream-dead")
			pw.shutdown()
			os.Exit(3)
		}
	}
}

func sigChan() <-chan os.Signal {
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	return ch
}
