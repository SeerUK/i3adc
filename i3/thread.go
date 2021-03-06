package i3

import (
	"context"

	"github.com/seeruk/i3adc/event"
	"github.com/seeruk/i3adc/logging"
	"go.i3wm.org/i3"
)

// Thread is a background thread designed to push output events from the i3 IPC into a channel to
// trigger other functionality in i3adc.
type Thread struct {
	ctx    context.Context
	cfn    context.CancelFunc
	logger logging.Logger
	msgCh  chan<- event.Event
	rcvr   *i3.EventReceiver
}

// NewThread creates a new output event thread instance.
func NewThread(logger logging.Logger) (*Thread, <-chan event.Event) {
	logger = logger.With("module", "i3/thread")
	msgCh := make(chan event.Event, 1)

	return &Thread{
		logger: logger,
		msgCh:  msgCh,
	}, msgCh
}

// Start begins waiting for events from i3, pushing them onto the message channel when possible.
func (t *Thread) Start() error {
	t.logger.Info("thread started")
	t.msgCh <- event.Event{IsStartup: true} // Send initial message at startup.

	t.ctx, t.cfn = context.WithCancel(context.Background())

	t.rcvr = i3.Subscribe(i3.OutputEventType)
	defer t.rcvr.Close()

	// TODO(seeruk): Can something be done here to avoid processing loads of events that come though
	// in quick succession?

	// Use a goroutine to allow this thread to be stopped. This goroutine will not die though, which
	// is very unfortunate, but shouldn't be a problem for i3adc, given it's current implementation.
	go func() {
		for t.rcvr.Next() {
			// Check context here so that we break out of the loop if possible. It may not always be
			// the case, meaning sometimes we may have a routine being leaked, at least for 5
			// seconds. This should only really happen when we're quitting anyway.
			select {
			case <-t.ctx.Done():
				break
			default:
			}

			t.logger.Debugw("received event from i3", "event", t.rcvr.Event())
			t.msgCh <- event.Event{}
		}
	}()

	// Wait for stop signal.
	<-t.ctx.Done()

	t.logger.Info("thread stopped")

	return nil
}

// Stop attempts to stop this thread.
func (t *Thread) Stop() error {
	t.logger.Infow("thread stopping")

	if t.ctx != nil && t.cfn != nil {
		t.cfn()
	}

	return nil
}
