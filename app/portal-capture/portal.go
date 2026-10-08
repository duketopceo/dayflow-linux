package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// org.freedesktop.portal.ScreenCast session flow: CreateSession →
// SelectSources → Start → OpenPipeWireRemote. Every mutating call returns a
// Request object that resolves asynchronously via a Response signal; the
// path is predictable from the bus unique name + our token, so the match is
// registered before the call (no response race).
const (
	portalDest    = "org.freedesktop.portal.Desktop"
	portalPath    = "/org/freedesktop/portal/desktop"
	screenCastIfc = "org.freedesktop.portal.ScreenCast"
	requestIfc    = "org.freedesktop.portal.Request"
)

var reqSeq uint32
var reqMu sync.Mutex

func nextToken(prefix string) string {
	reqMu.Lock()
	defer reqMu.Unlock()
	reqSeq++
	return fmt.Sprintf("dayflow_%s_%d_%d", prefix, time.Now().UnixNano(), reqSeq)
}

// senderToken converts the bus unique name ":1.234" → "1_234" for request
// and session object paths.
func senderToken(conn *dbus.Conn) string {
	return strings.ReplaceAll(strings.TrimPrefix(conn.Names()[0], ":"), ".", "_")
}

func requestPath(conn *dbus.Conn, token string) dbus.ObjectPath {
	return dbus.ObjectPath(fmt.Sprintf("%s/request/%s/%s", portalPath, senderToken(conn), token))
}

// portalRequest issues a portal call and blocks for its Response signal.
// Returns the response code (0 = success) and results dict.
func portalRequest(conn *dbus.Conn, obj dbus.BusObject, method string,
	callOpts map[string]dbus.Variant, args ...interface{}) (uint32, map[string]dbus.Variant, error) {

	token := nextToken("req")
	reqPath := requestPath(conn, token)
	if callOpts == nil {
		callOpts = map[string]dbus.Variant{}
	}
	callOpts["handle_token"] = dbus.MakeVariant(token)

	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(reqPath),
		dbus.WithMatchInterface(requestIfc),
		dbus.WithMatchMember("Response"),
	); err != nil {
		return 0, nil, fmt.Errorf("add match: %w", err)
	}
	ch := make(chan *dbus.Signal, 4)
	conn.Signal(ch)
	defer func() {
		conn.RemoveSignal(ch)
		conn.RemoveMatchSignal(
			dbus.WithMatchObjectPath(reqPath),
			dbus.WithMatchInterface(requestIfc),
			dbus.WithMatchMember("Response"),
		)
	}()

	call := obj.Call(method, 0, append(args, callOpts)...)
	if call.Err != nil {
		return 0, nil, fmt.Errorf("%s: %w", method, call.Err)
	}
	var handle dbus.ObjectPath
	if err := call.Store(&handle); err != nil {
		return 0, nil, fmt.Errorf("%s handle: %w", method, err)
	}
	if handle != reqPath {
		// Some portals return a different handle than the predicted path —
		// match on the returned one too.
		conn.AddMatchSignal(
			dbus.WithMatchObjectPath(handle),
			dbus.WithMatchInterface(requestIfc),
			dbus.WithMatchMember("Response"),
		)
	}

	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	for {
		select {
		case sig := <-ch:
			if sig.Name != requestIfc+".Response" {
				continue
			}
			if sig.Path != reqPath && sig.Path != handle {
				continue
			}
			if len(sig.Body) < 2 {
				return 0, nil, fmt.Errorf("%s: malformed Response", method)
			}
			code, ok := sig.Body[0].(uint32)
			if !ok {
				return 0, nil, fmt.Errorf("%s: bad Response code", method)
			}
			results, _ := sig.Body[1].(map[string]dbus.Variant)
			return code, results, nil
		case <-timer.C:
			return 0, nil, fmt.Errorf("%s: timed out waiting for portal response", method)
		}
	}
}

type portalSession struct {
	session dbus.ObjectPath
	nodeIDs []uint32
	// positions/sizes are screen coordinates when the portal reports them —
	// used for layout-accurate compositing, horizontal fallback otherwise.
	slots []streamSlot
	token string // fresh restore_token when persist_mode was honored
}

type streamSlot struct {
	node   uint32
	x, y   int
	hasPos bool
}

// openPortalSession runs the full ScreenCast consent flow. On denial the
// caller receives portalDenied; any D-Bus/portal absence surfaces as a
// plain error so the supervisor can park the backend.
func openPortalSession(conn *dbus.Conn, restoreToken string) (*portalSession, error) {
	obj := conn.Object(portalDest, portalPath)

	// CreateSession
	sessionToken := nextToken("sess")
	code, results, err := portalRequest(conn, obj, screenCastIfc+".CreateSession",
		map[string]dbus.Variant{
			"session_handle_token": dbus.MakeVariant(sessionToken),
		})
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, portalDenied{code: code, phase: "create-session"}
	}
	var session dbus.ObjectPath
	if v, ok := results["session_handle"]; ok {
		if s, ok := v.Value().(string); ok {
			session = dbus.ObjectPath(s)
		}
	}
	if session == "" {
		session = dbus.ObjectPath(fmt.Sprintf("%s/session/%s/%s", portalPath, senderToken(conn), sessionToken))
	}

	// SelectSources — monitor capture, cursor as metadata (pointer stays out
	// of frames), persist until revoked, restore token if we hold one.
	selectOpts := map[string]dbus.Variant{
		"types":        dbus.MakeVariant(uint32(1)), // MONITOR
		"multiple":     dbus.MakeVariant(true),
		"cursor_mode":  dbus.MakeVariant(uint32(2)), // metadata
		"persist_mode": dbus.MakeVariant(uint32(2)), // until revoked
	}
	if restoreToken != "" {
		selectOpts["restore_token"] = dbus.MakeVariant(restoreToken)
	}
	code, _, err = portalRequest(conn, obj, screenCastIfc+".SelectSources", selectOpts, session)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, portalDenied{code: code, phase: "select-sources"}
	}

	// Start — this is the user-visible consent prompt.
	code, results, err = portalRequest(conn, obj, screenCastIfc+".Start",
		map[string]dbus.Variant{}, session, "")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, portalDenied{code: code, phase: "start"}
	}

	ps := &portalSession{session: session}
	if v, ok := results["restore_token"]; ok {
		if s, ok := v.Value().(string); ok {
			ps.token = s
		}
	}
	rawStreams, ok := results["streams"]
	if !ok {
		return nil, fmt.Errorf("Start response carried no streams")
	}
	// a(ua{sv}) — godbus decodes each tuple as []interface{}{node, props}.
	tuples, ok := rawStreams.Value().([][]interface{})
	if !ok {
		return nil, fmt.Errorf("streams variant type %T unexpected", rawStreams.Value())
	}
	for _, tuple := range tuples {
		if len(tuple) < 2 {
			continue
		}
		node, _ := tuple[0].(uint32)
		props, _ := tuple[1].(map[string]dbus.Variant)
		slot := streamSlot{node: node}
		applyStreamProps(&slot, props)
		ps.slots = append(ps.slots, slot)
	}
	if len(ps.slots) == 0 {
		return nil, fmt.Errorf("Start returned zero streams")
	}
	for _, s := range ps.slots {
		ps.nodeIDs = append(ps.nodeIDs, s.node)
	}
	return ps, nil
}

// applyStreamProps reads the optional position member the portal may attach
// to each stream (a{sv} inside the stream tuple). Reported "size" is
// ignored — the decoded frame bounds are authoritative.
func applyStreamProps(slot *streamSlot, props map[string]dbus.Variant) {
	if props == nil {
		return
	}
	if v, ok := props["position"]; ok {
		if xy, ok := v.Value().([]interface{}); ok && len(xy) == 2 {
			slot.x, _ = toInt(xy[0])
			slot.y, _ = toInt(xy[1])
			slot.hasPos = true
		}
	}
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case uint32:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

// openPipeWireFD asks the portal for the PipeWire remote fd. The helper must
// connect PipeWire through THIS fd (pw_context_connect_fd), never the
// ambient socket — that is what keeps strict snap confinement viable.
func openPipeWireFD(conn *dbus.Conn, session dbus.ObjectPath) (int, error) {
	obj := conn.Object(portalDest, portalPath)
	call := obj.Call(screenCastIfc+".OpenPipeWireRemote", 0, session,
		map[string]dbus.Variant{})
	if call.Err != nil {
		return -1, fmt.Errorf("OpenPipeWireRemote: %w", call.Err)
	}
	var fd dbus.UnixFD
	if err := call.Store(&fd); err != nil {
		return -1, fmt.Errorf("OpenPipeWireRemote fd: %w", err)
	}
	return int(fd), nil
}

// portalDenied marks user refusal/cancel/dismissal — the supervisor parks
// rather than retrying, since another prompt without a user action is spam.
type portalDenied struct {
	code  uint32
	phase string
}

func (e portalDenied) Error() string {
	reason := map[uint32]string{1: "cancelled", 2: "dismissed"}[e.code]
	if reason == "" {
		reason = fmt.Sprintf("response code %d", e.code)
	}
	return fmt.Sprintf("portal %s: %s", e.phase, reason)
}
