// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

type mutateRequest interface {
	mutateAny(any) (any, error)
	mutateTeaModel(tea.Model) (tea.Model, error)
	respond(error)
}

type mutateMsg[M any] struct {
	fn   func(M) M
	done chan error
}

func (msg mutateMsg[M]) respond(err error) {
	if msg.done != nil {
		msg.done <- err
	}
}

type mutatableModel interface {
	mutate(mutateRequest) error
}

// Mutate applies fn to the current underlying model from within the [tea.Program]'s
// Update handling. This should be used sparingly, and only when necessary to test
// a specific scenario that is otherwise not possible or would pollute your
// model with unnecessary state. It uses the [testing.TB] from [NewHarness] or
// [NewComponentHarness] (generics cannot be expressed as a method on [Harness]).
//
// fn should use the same model type originally passed to [NewHarness] or
// [NewComponentHarness], such as func(Model) Model or func(*Model) *Model.
func Mutate[M any](h *Harness, fn func(M) M, opts ...Option) *Harness {
	h.tb.Helper()
	h.requireProgram()

	if fn == nil {
		h.tb.Fatalf("mutate function must not be nil")
	}

	cfg := collectOptions(h.mergedOpts(opts...)...)
	done := make(chan error, 1)
	h.SendProgram(mutateMsg[M]{fn: fn, done: done})

	ctx := h.tb.Context()
	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	select {
	case err := <-done:
		if err != nil {
			h.tb.Fatalf("mutate failed: %v", err)
		}
	case <-timer.C:
		h.tb.Fatalf("timeout waiting for mutation after %s", cfg.timeout)
	case <-ctx.Done():
		h.tb.Fatalf("test context canceled waiting for mutation: %v", ctx.Err())
	}

	return h
}

func (msg mutateMsg[M]) mutateAny(model any) (any, error) {
	current, ok := model.(M)
	if !ok {
		var zero M
		return nil, fmt.Errorf("mutate function must accept %T, current model is %T", zero, model)
	}

	return msg.fn(current), nil
}

func (msg mutateMsg[M]) mutateTeaModel(model tea.Model) (tea.Model, error) {
	next, err := msg.mutateAny(model)
	if err != nil {
		return nil, err
	}

	nextModel, ok := next.(tea.Model)
	if !ok {
		return nil, fmt.Errorf("mutate function must return tea.Model for root harnesses, got %T", next)
	}
	return nextModel, nil
}

func (cw *componentWrapper[M]) mutate(req mutateRequest) error {
	next, err := req.mutateAny(cw.model)
	if err != nil {
		return err
	}

	nextModel, ok := next.(M)
	if !ok {
		return fmt.Errorf("mutate function must return %T, got %T", cw.model, next)
	}

	cw.model = nextModel
	return nil
}
