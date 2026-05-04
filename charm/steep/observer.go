// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/steep/internal/broker"
)

type observer struct {
	tb       testing.TB
	mu       sync.RWMutex
	model    tea.Model
	messages *broker.Broker[uv.Event]

	// firstEvent closes after the first user-visible message reaches Update.
	firstEvent chan struct{}
	// firstEventOnce guarantees firstEvent closes exactly once.
	firstEventOnce sync.Once
	// lastViewSnapshot stores the most recent View content observed by the renderer.
	lastViewSnapshot string
}

// newObserver returns a tea.Model wrapper around model that records every
// message passed to Update in a broker, captures the latest view text, and
// coordinates harness-driven mutations.
func newObserver(tb testing.TB, model tea.Model) *observer {
	return &observer{
		tb:         tb,
		firstEvent: make(chan struct{}),
		model:      model,
		messages:   broker.New[uv.Event](broker.WithMaxHistory(0)),
	}
}

// Init implements [tea.Model] by delegating to the wrapped model.
func (o *observer) Init() tea.Cmd {
	o.tb.Helper()
	return o.currentModel().Init()
}

// Update implements [tea.Model]. It serves [mutateRequest] without forwarding
// them to the wrapped model, delegates all other messages to the wrapped
// model, publishes a copy of each inbound message to the broker (in receipt
// order), and closes firstEvent after the first Update completes.
func (o *observer) Update(msg uv.Event) (tea.Model, tea.Cmd) {
	o.tb.Helper()
	copiedMsg := msg

	o.mu.Lock()
	defer o.mu.Unlock()

	if msg, ok := msg.(mutateRequest); ok {
		err := o.mutateLocked(msg)
		msg.respond(err)
		return o, nil
	}

	next, cmd := o.model.Update(msg)
	if next != nil {
		o.replaceLocked(next)
	}
	o.messages.Publish(copiedMsg)
	o.firstEventOnce.Do(func() { close(o.firstEvent) })

	return o, cmd
}

// View implements [tea.Model], stores the rendered content for harness
// assertions, and returns the wrapped model's view.
func (o *observer) View() tea.View {
	o.tb.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()

	view := o.model.View()
	o.lastViewSnapshot = view.Content
	return view
}

// currentModel returns the wrapped [tea.Model]. Callers should treat it as
// read-only unless they hold [observer.mu] exclusively while mutating the wrapped
// model through the observer's own helpers.
func (o *observer) currentModel() tea.Model {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.model
}

// replaceLocked updates o.model from an Update return value or mutation.
// Caller must hold [observer.mu].
func (o *observer) replaceLocked(model tea.Model) {
	if model == nil {
		return
	}

	if wrapper, ok := model.(*observer); ok {
		if wrapper == o {
			return
		}
		model = wrapper.currentModel()
	}

	o.model = model
}

// replace sets the wrapped model, holding [observer.mu] for the duration.
func (o *observer) replace(model tea.Model) {
	o.mu.Lock()
	o.replaceLocked(model)
	o.mu.Unlock()
}

// mutate applies a test-only mutate request against the current tree of models.
func (o *observer) mutate(req mutateRequest) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.mutateLocked(req)
}

// mutateLocked implements mutate while assuming [observer.mu] is held.
func (o *observer) mutateLocked(req mutateRequest) error {
	if wrapper, ok := o.model.(*observer); ok && wrapper != o {
		return wrapper.mutate(req)
	}

	if model, ok := o.model.(mutatableModel); ok {
		return model.mutate(req)
	}

	next, err := req.mutateTeaModel(o.model)
	if err != nil {
		return err
	}
	o.replaceLocked(next)
	return nil
}
