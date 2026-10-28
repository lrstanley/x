// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/steep/internal/broker"
)

type observer struct {
	tb       testing.TB
	mu       sync.RWMutex
	model    tea.Model
	messages *broker.Broker[uv.Event]

	// firstEvent closes once the first message, other than a mutate
	// request, has been through Update.
	firstEvent     chan struct{}
	firstEventOnce sync.Once
	// lastViewSnapshot is the most recent View content.
	lastViewSnapshot string
	// onMessage, when set, is called with each message but mutate requests
	// before the wrapped model receives it, without mu held. It is set
	// before the program starts.
	onMessage func(uv.Event)
}

// newObserver returns a [tea.Model] wrapping model, which records every
// message in a broker and serves [Harness.Mutate] requests.
func newObserver(tb testing.TB, model tea.Model) *observer {
	return &observer{
		tb:         tb,
		firstEvent: make(chan struct{}),
		model:      model,
		messages:   broker.New[uv.Event](broker.WithMaxHistory(0)),
	}
}

func (o *observer) Init() tea.Cmd {
	o.tb.Helper()
	return o.currentModel().Init()
}

// Update serves mutate requests itself, and passes other messages to the
// wrapped model before publishing them to the broker.
func (o *observer) Update(msg uv.Event) (tea.Model, tea.Cmd) {
	o.tb.Helper()
	copiedMsg := msg

	// Recorded before taking the lock, which View waits on. Update only runs
	// on the program's goroutine, so the order is kept.
	if _, ok := msg.(mutateRequest); !ok && o.onMessage != nil {
		o.onMessage(msg)
	}

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

func (o *observer) View() tea.View {
	o.tb.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()

	view := o.model.View()
	o.lastViewSnapshot = view.Content
	return view
}

// currentModel returns the wrapped model, which callers must treat as
// read-only.
func (o *observer) currentModel() tea.Model {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.model
}

// replaceLocked sets the wrapped model, unwrapping an observer. Caller must
// hold mu.
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

// replace is replaceLocked, taking mu.
func (o *observer) replace(model tea.Model) {
	o.mu.Lock()
	o.replaceLocked(model)
	o.mu.Unlock()
}

// mutate applies req to the wrapped model.
func (o *observer) mutate(req mutateRequest) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.mutateLocked(req)
}

// mutateLocked is mutate with mu held.
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
