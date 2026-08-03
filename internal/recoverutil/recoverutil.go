// Package recoverutil provides panic-recovery wrappers for goroutines and
// gateway event handlers. A panic in any handler or spawned goroutine is
// caught, logged with a stack trace, and the goroutine exits cleanly — the
// process never crashes.
package recoverutil

import (
	"runtime/debug"

	"github.com/jettapindika/zoeyDCBot/internal/logging"
)

var log = logging.Component("panic")

// Guard runs fn and recovers from any panic. The recovered panic is logged
// with the goroutine label and a stack trace. Use it to wrap any code that
// runs in a goroutine whose panic would otherwise crash the process:
//
//	go recoverutil.Guard("worker", func() { … })
func Guard(label string, fn func()) {
	defer recoverPanic(label)
	fn()
}

// GuardGo launches fn in a new goroutine wrapped with Guard. It is a
// shorthand for `go Guard(label, fn)`.
func GuardGo(label string, fn func()) {
	go Guard(label, fn)
}

// Recover is a deferred-only helper. Call it as the first line of a function:
//
//	func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
//	    defer recoverutil.Recover("onMessageCreate")
//	    // ... handler body ...
//	}
//
// If the function panics, Recover catches it, logs it with a stack trace,
// and the function returns normally — the caller (e.g. discordgo's event
// loop) is never disrupted.
func Recover(label string) {
	r := recover()
	if r == nil {
		return
	}
	log.Error("panic recovered",
		"label", label,
		"panic", r,
		"stack", string(debug.Stack()),
	)
}

// Handler wraps a discordgo event handler function with panic recovery.
// The returned function has the same signature as the input, so it can be
// passed directly to sess.AddHandler:
//
//	sess.AddHandler(recoverutil.Handler("onMessageCreate", b.onMessageCreate))
//
// On panic, the handler logs the error and returns normally — the gateway
// event loop is never disrupted.
func Handler(label string, fn func()) func() {
	return func() {
		defer recoverPanic(label)
		fn()
	}
}

// Handler2 wraps a discordgo event handler that takes one argument (the most
// common form: func(*discordgo.Session, *discordgo.XxxCreate)).
func Handler2[A any](label string, fn func(s any, a A)) func(any, A) {
	return func(s any, a A) {
		defer recoverPanic(label)
		fn(s, a)
	}
}

// recoverPanic is the shared recovery logic. It must be called with defer.
func recoverPanic(label string) {
	r := recover()
	if r == nil {
		return
	}
	log.Error("panic recovered",
		"label", label,
		"panic", r,
		"stack", string(debug.Stack()),
	)
}
