package engine

import (
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"
)

// Health reports whether the engine is still working. Nothing here takes the engine lock: its
// whole point is to answer while the engine is stuck holding it.
type Health struct {
	panics    atomic.Int64
	lastPanic atomic.Pointer[PanicRecord]
	lastTick  atomic.Int64 // Unix seconds of the last monitor loop pass.
	started   atomic.Int64
}

// PanicRecord is one internal failure, kept for the interface to show and the log to explain.
type PanicRecord struct {
	When    int64  `json:"when"`
	Where   string `json:"where"`
	Message string `json:"message"`
}

// HealthReport is what the interface polls; it must stay answerable when everything else hangs.
type HealthReport struct {
	Panics         int64        `json:"panics"`
	LastPanic      *PanicRecord `json:"last_panic,omitempty"`
	StalledSeconds int64        `json:"stalled_seconds"`
}

// engineStallSeconds is how long the once-a-second monitor loop may miss before the engine counts
// as stuck. Verification and hashing keep it waiting on the lock, so this is generous.
const engineStallSeconds = 45

func newHealth() *Health {
	h := &Health{}
	now := time.Now().Unix()
	h.started.Store(now)
	h.lastTick.Store(now)
	return h
}

// Tick records that the engine's monitor loop came around again.
func (h *Health) Tick() {
	if h != nil {
		h.lastTick.Store(time.Now().Unix())
	}
}

// RecordPanic notes a recovered panic and writes it, with its stack, to the log.
func (h *Health) RecordPanic(where string, value any, stack []byte) {
	msg := panicMessage(value)
	log.Printf("💥 PANIC in %s: %s\n%s", where, msg, stack)
	if h == nil {
		return
	}
	h.panics.Add(1)
	h.lastPanic.Store(&PanicRecord{When: time.Now().Unix(), Where: where, Message: msg})
}

// Report describes the engine's health right now.
func (h *Health) Report() HealthReport {
	if h == nil {
		return HealthReport{}
	}
	rep := HealthReport{Panics: h.panics.Load(), LastPanic: h.lastPanic.Load()}
	// A torrent client that never started ticking is not stalled, it is young.
	if since := time.Now().Unix() - h.lastTick.Load(); since > engineStallSeconds {
		rep.StalledSeconds = since
	}
	return rep
}

// Health gives access to the engine's failure counters.
func (e *Engine) Health() *Health {
	if e == nil {
		return nil
	}
	return e.health
}

// RecoverPanic is deferred where a panic would otherwise be swallowed silently. The process keeps
// running, but the failure is counted, logged with its stack, and shown in the interface: a panic
// escaping the torrent client can leave its lock held, and then only a restart helps.
func (e *Engine) RecoverPanic(where string) {
	if r := recover(); r != nil {
		e.Health().RecordPanic(where, r, debug.Stack())
	}
}

// panicMessage is the first line of a panic value, short enough for a banner.
func panicMessage(v any) string {
	msg := strings.TrimSpace(strings.SplitN(fmt.Sprint(v), "\n", 2)[0])
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	return msg
}

// SetLastTickForTest backdates the last monitor pass, so the stall detector can be exercised.
func (h *Health) SetLastTickForTest(when time.Time) {
	h.lastTick.Store(when.Unix())
}
