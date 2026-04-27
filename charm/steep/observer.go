// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

type observer struct {
	mu                  sync.RWMutex
	model               tea.Model
	lastViewSnapshot    string
	observedMsgs        []tea.Msg
	lastReceivedMessage time.Time
}

func newObserver(model tea.Model) *observer {
	return &observer{
		model:               model,
		lastReceivedMessage: time.Now(),
	}
}

func (o *observer) Init() tea.Cmd {
	return o.currentModel().Init()
}

func (o *observer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	copiedMsg := msg

	o.mu.Lock()
	defer o.mu.Unlock()

	if msg, ok := msg.(mutateRequest); ok {
		err := o.mutateLocked(msg)
		o.lastReceivedMessage = time.Now()
		msg.respond(err)
		return o, nil
	}

	next, cmd := o.model.Update(msg)
	if next != nil {
		o.replaceLocked(next)
	}
	o.observedMsgs = append(o.observedMsgs, copiedMsg)
	o.lastReceivedMessage = time.Now()

	return o, cmd
}

func (o *observer) View() tea.View {
	o.mu.Lock()
	defer o.mu.Unlock()

	view := o.model.View()
	o.lastViewSnapshot = view.Content
	return view
}

func (o *observer) currentModel() tea.Model {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.model
}

// replaceLocked updates w.model after an Update return value. Caller must hold w.mu.
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

func (o *observer) replace(model tea.Model) {
	o.mu.Lock()
	o.replaceLocked(model)
	o.mu.Unlock()
}

func (o *observer) mutate(req mutateRequest) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.mutateLocked(req)
}

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

func (o *observer) messages() []tea.Msg {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return append([]tea.Msg(nil), o.observedMsgs...)
}
