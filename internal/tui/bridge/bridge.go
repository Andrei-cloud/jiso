// Package bridge pumps typed events from the internal/app observation bus
// (internal/app/events) into a Bubble Tea program as tea.Msg values.
//
// # Lifecycle
//
// A Bridge owns exactly one pump goroutine, started by Cmd (the tea.Cmd
// handed to the program from Init/Update). The pump selects on three stops:
//
//	context cancellation (the program's ctx — program exit),
//	the Bridge's own done channel (Stop),
//	closure of the source channel (bus Close/unsubscribe).
//
// Whichever fires, the goroutine returns and Wait observes it, so a caller
// (or a runtime.NumGoroutine test) can prove no leak. After the pump stops
// the Bridge sends nothing further.
//
// The program is reached through the injectable Sender func — production
// passes (*tea.Program).Send; unit tests pass a collector. A *tea.Program is
// never required, keeping the bridge unit-testable and import-light.
package bridge

import (
	"context"
	"sync"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app/events"
)

// Msg is the tea.Msg wrapper carrying one bus event into the program's
// Update. Models type-switch on Msg, then on the sealed Event taxonomy
// inside it; the wrapper never copies or mutates the event.
type Msg struct {
	Event events.Event
}

// Sender delivers one message to the program. It matches
// (*tea.Program).Send's signature exactly; after program exit that method is
// a documented no-op, so a racing late send is safe.
type Sender func(msg tea.Msg)

// Bridge pumps <-chan events.Event into a Sender. Construct with New, start
// with Cmd, stop with Stop (or by cancelling ctx / closing the source).
type Bridge struct {
	ctx  context.Context
	src  <-chan events.Event
	send Sender

	done     chan struct{}
	doneOnce sync.Once
}

// New wires the bridge. A nil send is replaced by a sink that drops
// messages (useful when a model is armed without a program, e.g. pure
// Update tests). A nil ctx is treated as context.Background.
func New(ctx context.Context, src <-chan events.Event, send Sender) *Bridge {
	if ctx == nil {
		ctx = context.Background()
	}
	if send == nil {
		send = func(tea.Msg) {}
	}

	return &Bridge{ctx: ctx, src: src, send: send, done: make(chan struct{})}
}

// Cmd returns the tea.Cmd that IS the pump: Bubble Tea executes commands in
// their own goroutine, so the bridge owns exactly one goroutine while the
// program is alive, and Update stays side-effect free. The command blocks
// until one of the three stops fires, then returns nil (a nil msg the model
// ignores). Callers that need a join point run the Cmd themselves and wait
// on their goroutine (or poll runtime.NumGoroutine, as the leak test does);
// Executing the Cmd twice runs two pumps — a model arms once (the root
// model's pending flag does exactly that).
func (b *Bridge) Cmd() tea.Cmd {
	return func() tea.Msg {
		b.pump()

		return nil
	}
}

// pump is the single goroutine: select on ctx/done/source, forward as
// Msg. It never blocks on send for longer than the consumer takes,
// and exits on any of the three stops without draining the source.
func (b *Bridge) pump() {
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-b.done:
			return
		case ev, ok := <-b.src:
			if !ok {
				return // source closed: bus closed or unsubscribed
			}
			b.send(Msg{Event: ev})
		}
	}
}

// Stop signals the pump to exit and is safe to call multiple times. It does
// not wait for the goroutine: the pump's owning goroutine (the program's
// command runner, or a test's go statement) observes the return.
func (b *Bridge) Stop() { b.doneOnce.Do(func() { close(b.done) }) }
