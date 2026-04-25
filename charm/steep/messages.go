// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// MessageLog exposes messages observed by a test harness.
type MessageLog interface {
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

// WaitForMessage waits until at least one message with the same concrete type
// as T has been observed, then returns the first match.
func WaitForMessage[T tea.Msg](tb testing.TB, log MessageLog, opts ...Option) T {
	tb.Helper()

	return WaitForMessages[T](tb, log, opts...)[0]
}

// WaitForMessages waits until at least one message with the same concrete type
// as T has been observed, then returns all current matches.
func WaitForMessages[T tea.Msg](tb testing.TB, log MessageLog, opts ...Option) []T {
	tb.Helper()

	return waitForMessages(tb, log, func(T) bool { return true }, opts...)
}

// WaitForMessageWhere waits until a message with the same concrete type as T
// has been observed and match returns true.
func WaitForMessageWhere[T tea.Msg](tb testing.TB, log MessageLog, match func(T) bool, opts ...Option) T {
	tb.Helper()

	if match == nil {
		tb.Fatalf("message matcher must not be nil")
	}
	return waitForMessages(tb, log, match, opts...)[0]
}

func waitForMessages[T tea.Msg](tb testing.TB, log MessageLog, match func(T) bool, opts ...Option) []T {
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
	sort.Strings(names)
	return strings.Join(names, ", ")
}
