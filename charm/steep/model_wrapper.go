// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

type modelWrapper struct {
	mu                  sync.RWMutex
	model               tea.Model
	lastViewSnapshot    string
	observedMsgs        []tea.Msg
	lastReceivedMessage time.Time
}

func newModelWrapper(model tea.Model) *modelWrapper {
	w := &modelWrapper{
		model:               model,
		lastReceivedMessage: time.Now(),
	}
	return w
}

func (w *modelWrapper) Init() tea.Cmd {
	cmd := w.currentModel().Init()
	return cmd
}

func (w *modelWrapper) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	copiedMsg := msg

	w.mu.Lock()
	defer w.mu.Unlock()

	next, cmd := w.model.Update(msg)
	if next != nil {
		if wrapper, ok := next.(*modelWrapper); ok {
			if wrapper == w {
				next = w.model
			} else {
				next = wrapper.currentModel()
			}
		}
		w.model = next
	}
	w.observedMsgs = append(w.observedMsgs, copiedMsg)
	w.lastReceivedMessage = time.Now()

	return w, cmd
}

func (w *modelWrapper) View() tea.View {
	w.mu.Lock()
	defer w.mu.Unlock()

	view := w.model.View()
	w.lastViewSnapshot = view.Content
	return view
}

func (w *modelWrapper) currentModel() tea.Model {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.model
}

func (w *modelWrapper) replace(model tea.Model) {
	if model == nil {
		return
	}
	if wrapper, ok := model.(*modelWrapper); ok {
		model = wrapper.currentModel()
	}

	w.mu.Lock()
	w.model = model
	w.mu.Unlock()
}

func (w *modelWrapper) messages() []tea.Msg {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return append([]tea.Msg(nil), w.observedMsgs...)
}
