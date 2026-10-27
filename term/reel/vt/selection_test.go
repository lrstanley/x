// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"errors"
	"strings"
	"testing"
)

func TestSelectWordLineAndAll(t *testing.T) {
	t.Parallel()
	term := openTest(t, 20, 4)
	if _, err := term.Write([]byte("hello world\r\nsecond")); err != nil {
		t.Fatal(err)
	}
	word, err := term.SelectWord(Point{Tag: PointActive, X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}
	text, err := term.SelectionText(word)
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello" {
		t.Fatalf("word = %q", text)
	}
	if word.Start.Y != 0 || word.End.X != 4 {
		t.Fatalf("word span = %+v %+v", word.Start, word.End)
	}
	order, err := term.SelectionOrder(word)
	if err != nil {
		t.Fatal(err)
	}
	if order != SelectionOrderForward {
		t.Fatalf("order = %d", order)
	}
	inside, err := term.SelectionContains(word, Point{Tag: PointActive, X: 1, Y: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !inside {
		t.Fatal("word did not contain column 1")
	}
	between, err := term.SelectWordBetween(Point{Tag: PointActive}, Point{Tag: PointActive, X: 8, Y: 0})
	if err != nil {
		t.Fatal(err)
	}
	betweenText, err := term.SelectionText(between)
	if err != nil {
		t.Fatal(err)
	}
	if betweenText != "hello" {
		t.Fatalf("word between = %q", betweenText)
	}
	line, err := term.SelectLine(Point{Tag: PointActive, X: 0, Y: 1})
	if err != nil {
		t.Fatal(err)
	}
	lineText, err := term.SelectionText(line)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lineText, "second") {
		t.Fatalf("line = %q", lineText)
	}
	all, err := term.SelectAll()
	if err != nil {
		t.Fatal(err)
	}
	allText, err := term.SelectionText(all)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(allText, "hello") || !strings.Contains(allText, "second") {
		t.Fatalf("all = %q", allText)
	}
	_, err = term.SelectWord(Point{Tag: PointActive, X: 19, Y: 3})
	if !errors.Is(err, ErrNoValue) {
		t.Fatalf("empty select = %v", err)
	}
}

func TestSelectOutput(t *testing.T) {
	t.Parallel()
	term := openTest(t, 20, 6)
	// A starts a prompt, I ends it at the next line, and the following
	// text is command output. This is the sequence libghostty marks
	// with semantic content output.
	seq := "\x1b]133;A\x1b\\hello\x1b]133;I\x1b\\\r\nworld"
	if _, err := term.Write([]byte(seq)); err != nil {
		t.Fatal(err)
	}
	sel, err := term.SelectOutput(Point{Tag: PointActive, Y: 1})
	if err != nil {
		t.Fatal(err)
	}
	text, err := term.SelectionText(sel)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "world") {
		t.Fatalf("output = %q", text)
	}
}
