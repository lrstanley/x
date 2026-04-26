// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// MessageCollector exposes messages observed by a test harness.
type MessageCollector interface {
	Messages() []tea.Msg
}

// MessagesOfType returns messages with the same concrete type as T.
func MessagesOfType[T tea.Msg](messages []tea.Msg) []T {
	var out []T
	target := messageReflectType[T]()

	for _, msg := range messages {
		if reflect.TypeOf(msg) != target {
			continue
		}
		typed, ok := msg.(T)
		if ok {
			out = append(out, typed)
		}
	}
	return out
}

// WaitMessage waits until at least one message with the same concrete type
// as T has been observed, then returns the first match.
func WaitMessage[T tea.Msg](tb testing.TB, log MessageCollector, opts ...Option) T {
	tb.Helper()

	return WaitMessages[T](tb, log, opts...)[0]
}

// WaitMessages waits until at least one message with the same concrete type
// as T has been observed, then returns all current matches.
func WaitMessages[T tea.Msg](tb testing.TB, log MessageCollector, opts ...Option) []T {
	tb.Helper()

	return waitMessages(tb, log, func(T) bool { return true }, opts...)
}

// WaitMessageWhere waits until a message with the same concrete type as T
// has been observed and match returns true.
func WaitMessageWhere[T tea.Msg](tb testing.TB, log MessageCollector, match func(T) bool, opts ...Option) T {
	tb.Helper()

	if match == nil {
		tb.Fatalf("message matcher must not be nil")
	}
	return waitMessages(tb, log, match, opts...)[0]
}

func waitMessages[T tea.Msg](tb testing.TB, log MessageCollector, match func(T) bool, opts ...Option) []T {
	tb.Helper()

	cfg := collectOptions(opts...)
	deadline := time.Now().Add(cfg.timeout)

	for {
		messages := log.Messages()
		matches := MessagesOfType[T](messages)
		if match != nil {
			matches = filterMessages(matches, match)
		}
		if len(matches) > 0 {
			return matches
		}
		if time.Now().After(deadline) {
			tb.Fatalf(
				"timeout waiting for message of type %s\nobserved message types: %s",
				messageTypeName[T](),
				observedMessageTypes(messages),
			)
		}
		time.Sleep(cfg.checkInterval)
	}
}

func filterMessages[T tea.Msg](messages []T, match func(T) bool) []T {
	out := make([]T, 0, len(messages))
	for _, msg := range messages {
		if match(msg) {
			out = append(out, msg)
		}
	}
	return out
}

func messageTypeName[T tea.Msg]() string {
	typ := messageReflectType[T]()
	if typ == nil {
		return "<nil>"
	}
	return typ.String()
}

func messageReflectType[T tea.Msg]() reflect.Type {
	var zero T
	return reflect.TypeOf(zero)
}

func observedMessageTypes(messages []tea.Msg) string {
	if len(messages) == 0 {
		return "none"
	}

	seen := make(map[string]struct{}, len(messages))
	for _, msg := range messages {
		seen[fmt.Sprintf("%T", msg)] = struct{}{}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}
