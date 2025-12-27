// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package tail

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatch_Basic(t *testing.T) {
	tmpdir := t.TempDir()
	path := filepath.Join(tmpdir, "test.log")

	err := os.WriteFile(path, []byte("line1\nline2\n"), 0o644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := &Config{
		SplitFunc:    bufio.ScanLines,
		RecheckDelay: 50 * time.Millisecond,
		Logger:       slog.Default(),
	}

	var lines []string
	for line, err := range Watch(ctx, config, path) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		lines = append(lines, string(line))
		if len(lines) >= 2 {
			break
		}
	}

	if len(lines) > 0 {
		t.Logf("received lines: %v", lines)
	}
}

func TestWatch_NewLines(t *testing.T) {
	tmpdir := t.TempDir()
	path := filepath.Join(tmpdir, "test.log")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := &Config{
		SplitFunc:    bufio.ScanLines,
		RecheckDelay: 50 * time.Millisecond,
	}

	done := make(chan bool)
	var receivedLines []string

	go func() {
		for line, err := range Watch(ctx, config, path) {
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			receivedLines = append(receivedLines, string(line))
			if len(receivedLines) >= 3 {
				done <- true
				return
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("failed to open file for writing: %v", err)
	}

	_, _ = file.WriteString("line1\n")
	_ = file.Sync()
	time.Sleep(100 * time.Millisecond)

	_, _ = file.WriteString("line2\n")
	_ = file.Sync()
	time.Sleep(100 * time.Millisecond)

	_, _ = file.WriteString("line3\n")
	_ = file.Sync()
	file.Close()

	select {
	case <-done:
		if len(receivedLines) != 3 {
			t.Errorf("expected 3 lines, got %d: %v", len(receivedLines), receivedLines)
		}
		if receivedLines[0] != "line1" {
			t.Errorf("expected 'line1', got %q", receivedLines[0])
		}
		if receivedLines[1] != "line2" {
			t.Errorf("expected 'line2', got %q", receivedLines[1])
		}
		if receivedLines[2] != "line3" {
			t.Errorf("expected 'line3', got %q", receivedLines[2])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for lines")
	}
}

func TestWatch_FileDeleted(t *testing.T) {
	tmpdir := t.TempDir()
	path := filepath.Join(tmpdir, "test.log")

	err := os.WriteFile(path, []byte("initial\n"), 0o644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := &Config{
		SplitFunc:     bufio.ScanLines,
		RecheckDelay:  50 * time.Millisecond,
		ReadFromStart: true,
	}

	done := make(chan bool)
	var receivedLines []string

	go func() {
		for line, err := range Watch(ctx, config, path) {
			if err != nil {
				if errors.Is(err, os.ErrPermission) {
					t.Errorf("unexpected permission error: %v", err)
					return
				}
				t.Logf("error received: %v", err)
				continue
			}
			receivedLines = append(receivedLines, string(line))
		}
		done <- true
	}()

	time.Sleep(100 * time.Millisecond)

	err = os.Remove(path)
	if err != nil {
		t.Fatalf("failed to remove file: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	err = os.WriteFile(path, []byte("recreated\n"), 0o644)
	if err != nil {
		t.Fatalf("failed to recreate file: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("failed to open file for writing: %v", err)
	}
	_, _ = file.WriteString("newline\n")
	_ = file.Sync()
	file.Close()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
		t.Logf("received lines: %v", receivedLines)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestWatch_FileTruncated(t *testing.T) {
	tmpdir := t.TempDir()
	path := filepath.Join(tmpdir, "test.log")

	err := os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := &Config{
		SplitFunc:     bufio.ScanLines,
		RecheckDelay:  50 * time.Millisecond,
		ReadFromStart: true,
	}

	done := make(chan bool)
	var receivedLines []string

	go func() {
		for line, err := range Watch(ctx, config, path) {
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			receivedLines = append(receivedLines, string(line))
			if len(receivedLines) >= 3 {
				done <- true
				return
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)

	err = os.Truncate(path, 0)
	if err != nil {
		t.Fatalf("failed to truncate file: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	file, err := os.OpenFile(path, os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("failed to open file for writing: %v", err)
	}
	_, _ = file.WriteString("new1\nnew2\nnew3\n")
	_ = file.Sync()
	file.Close()

	select {
	case <-done:
		if len(receivedLines) != 3 {
			t.Errorf("expected 3 lines, got %d: %v", len(receivedLines), receivedLines)
		}
		for i, line := range receivedLines {
			expected := "new" + string(rune('1'+i))
			if line != expected {
				t.Errorf("line %d: expected %q, got %q", i, expected, line)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for lines")
	}
}

func TestWatch_FileMoved(t *testing.T) {
	tmpdir := t.TempDir()
	path := filepath.Join(tmpdir, "test.log")
	newPath := filepath.Join(tmpdir, "test_moved.log")

	err := os.WriteFile(path, []byte("initial\n"), 0o644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := &Config{
		SplitFunc:     bufio.ScanLines,
		RecheckDelay:  50 * time.Millisecond,
		ReadFromStart: true,
	}

	done := make(chan bool)
	var receivedLines []string

	go func() {
		for line, err := range Watch(ctx, config, path) {
			if err != nil {
				if errors.Is(err, os.ErrPermission) {
					t.Errorf("unexpected permission error: %v", err)
					return
				}
				if errors.Is(err, os.ErrNotExist) {
					t.Errorf("should not return ErrNotExist, should wait instead: %v", err)
					return
				}
				t.Logf("error received: %v", err)
				continue
			}
			receivedLines = append(receivedLines, string(line))
		}
		done <- true
	}()

	time.Sleep(100 * time.Millisecond)

	err = os.Rename(path, newPath)
	if err != nil {
		t.Fatalf("failed to move file: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	err = os.WriteFile(path, []byte("recreated\n"), 0o644)
	if err != nil {
		t.Fatalf("failed to recreate file: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("failed to open file for writing: %v", err)
	}
	_, _ = file.WriteString("newline\n")
	_ = file.Sync()
	file.Close()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
		t.Logf("received lines: %v", receivedLines)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestWatch_FileNotExistsInitially(t *testing.T) {
	tmpdir := t.TempDir()
	path := filepath.Join(tmpdir, "test.log")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := &Config{
		SplitFunc:     bufio.ScanLines,
		RecheckDelay:  50 * time.Millisecond,
		ReadFromStart: true,
	}

	done := make(chan bool)
	var receivedLines []string

	go func() {
		for line, err := range Watch(ctx, config, path) {
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					t.Errorf("should not return ErrNotExist, should wait instead: %v", err)
					return
				}
				if errors.Is(err, os.ErrPermission) {
					t.Errorf("unexpected permission error: %v", err)
					return
				}
				t.Logf("error received: %v", err)
				continue
			}
			receivedLines = append(receivedLines, string(line))
			if len(receivedLines) >= 2 {
				done <- true
				return
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)

	err := os.WriteFile(path, []byte("line1\n"), 0o644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("failed to open file for writing: %v", err)
	}
	_, _ = file.WriteString("line2\n")
	_ = file.Sync()
	file.Close()

	select {
	case <-done:
		if len(receivedLines) != 2 {
			t.Errorf("expected 2 lines, got %d: %v", len(receivedLines), receivedLines)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for lines")
	}
}

func TestWatch_CustomSplitFunc(t *testing.T) {
	tmpdir := t.TempDir()
	path := filepath.Join(tmpdir, "test.log")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := &Config{
		SplitFunc: func(data []byte, atEOF bool) (advance int, token []byte, err error) {
			if atEOF && len(data) == 0 {
				return 0, nil, nil
			}
			if i := bytes.IndexByte(data, '|'); i >= 0 {
				return i + 1, data[0:i], nil
			}
			if atEOF {
				return len(data), data, nil
			}
			return 0, nil, nil
		},
		RecheckDelay: 50 * time.Millisecond,
	}

	done := make(chan bool)
	var receivedTokens []string

	go func() {
		for token, err := range Watch(ctx, config, path) {
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			receivedTokens = append(receivedTokens, string(token))
			if len(receivedTokens) >= 3 {
				done <- true
				return
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("failed to open file for writing: %v", err)
	}

	_, _ = file.WriteString("token1|token2|token3|")
	_ = file.Sync()
	file.Close()

	select {
	case <-done:
		if len(receivedTokens) != 3 {
			t.Errorf("expected 3 tokens, got %d: %v", len(receivedTokens), receivedTokens)
		}
		if receivedTokens[0] != "token1" {
			t.Errorf("expected 'token1', got %q", receivedTokens[0])
		}
		if receivedTokens[1] != "token2" {
			t.Errorf("expected 'token2', got %q", receivedTokens[1])
		}
		if receivedTokens[2] != "token3" {
			t.Errorf("expected 'token3', got %q", receivedTokens[2])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for tokens")
	}
}

func TestWatch_DefaultSplitFunc(t *testing.T) {
	tmpdir := t.TempDir()
	path := filepath.Join(tmpdir, "test.log")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := &Config{
		SplitFunc:    nil,
		RecheckDelay: 50 * time.Millisecond,
	}

	done := make(chan bool)
	var receivedLines []string

	go func() {
		for line, err := range Watch(ctx, config, path) {
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			receivedLines = append(receivedLines, string(line))
			if len(receivedLines) >= 2 {
				done <- true
				return
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Write lines
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("failed to open file for writing: %v", err)
	}

	_, _ = file.WriteString("line1\nline2\n")
	_ = file.Sync()
	file.Close()

	select {
	case <-done:
		if len(receivedLines) != 2 {
			t.Errorf("expected 2 lines, got %d: %v", len(receivedLines), receivedLines)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for lines")
	}
}

func TestWatch_ContextCancellation(t *testing.T) {
	tmpdir := t.TempDir()
	path := filepath.Join(tmpdir, "test.log")

	err := os.WriteFile(path, []byte("line1\n"), 0o644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	config := &Config{
		SplitFunc:    bufio.ScanLines,
		RecheckDelay: 50 * time.Millisecond,
	}

	done := make(chan bool)

	go func() {
		count := 0
		for _, err := range Watch(ctx, config, path) {
			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
					t.Logf("error received: %v", err)
				}
				continue
			}
			count++
		}
		done <- true
	}()

	time.Sleep(100 * time.Millisecond)

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for cancellation")
	}
}

func TestWatch_ReadFromStartFalse(t *testing.T) {
	tmpdir := t.TempDir()
	path := filepath.Join(tmpdir, "test.log")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := &Config{
		SplitFunc:     bufio.ScanLines,
		RecheckDelay:  50 * time.Millisecond,
		ReadFromStart: false,
	}

	done := make(chan bool)
	var receivedLines []string

	go func() {
		for line, err := range Watch(ctx, config, path) {
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			receivedLines = append(receivedLines, string(line))
			if len(receivedLines) >= 2 {
				done <- true
				return
			}
		}
	}()

	// Give it a moment to start watching
	time.Sleep(100 * time.Millisecond)

	// Create file with initial content - this should NOT be read
	err := os.WriteFile(path, []byte("skipped1\nskipped2\n"), 0o644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Wait for the file to be opened
	time.Sleep(100 * time.Millisecond)

	// Append new content - this SHOULD be read
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("failed to open file for appending: %v", err)
	}
	file.WriteString("line1\n")
	file.Sync()
	time.Sleep(100 * time.Millisecond)
	file.WriteString("line2\n")
	file.Sync()
	file.Close()

	select {
	case <-done:
		if len(receivedLines) != 2 {
			t.Errorf("expected 2 lines, got %d: %v", len(receivedLines), receivedLines)
		}
		if receivedLines[0] != "line1" {
			t.Errorf("expected 'line1', got %q", receivedLines[0])
		}
		if receivedLines[1] != "line2" {
			t.Errorf("expected 'line2', got %q", receivedLines[1])
		}
		// Verify we did NOT receive the skipped lines
		for _, line := range receivedLines {
			if line == "skipped1" || line == "skipped2" {
				t.Errorf("received skipped line: %q", line)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for lines, received: %v", receivedLines)
	}
}

func TestWatch_ReadFromStartFalse_Truncation(t *testing.T) {
	tmpdir := t.TempDir()
	path := filepath.Join(tmpdir, "test.log")

	// Create file with initial content
	err := os.WriteFile(path, []byte("initial1\ninitial2\ninitial3\n"), 0o644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := &Config{
		SplitFunc:     bufio.ScanLines,
		RecheckDelay:  50 * time.Millisecond,
		ReadFromStart: false,
	}

	done := make(chan bool)
	var receivedLines []string

	go func() {
		for line, err := range Watch(ctx, config, path) {
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			receivedLines = append(receivedLines, string(line))
			if len(receivedLines) >= 2 {
				done <- true
				return
			}
		}
	}()

	// Give it a moment to start watching
	time.Sleep(100 * time.Millisecond)

	// Truncate the file
	err = os.Truncate(path, 0)
	if err != nil {
		t.Fatalf("failed to truncate file: %v", err)
	}

	// Wait for truncation to be detected
	time.Sleep(150 * time.Millisecond)

	// Append content after truncation - this SHOULD be read
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("failed to open file for appending: %v", err)
	}
	file.WriteString("line1\n")
	file.Sync()
	time.Sleep(100 * time.Millisecond)
	file.WriteString("line2\n")
	file.Sync()
	file.Close()

	select {
	case <-done:
		if len(receivedLines) != 2 {
			t.Errorf("expected 2 lines, got %d: %v", len(receivedLines), receivedLines)
		}
		if receivedLines[0] != "line1" {
			t.Errorf("expected 'line1', got %q", receivedLines[0])
		}
		if receivedLines[1] != "line2" {
			t.Errorf("expected 'line2', got %q", receivedLines[1])
		}
		// Verify we did NOT receive the initial content
		for _, line := range receivedLines {
			if line == "initial1" || line == "initial2" || line == "initial3" {
				t.Errorf("received initial line that should have been skipped: %q", line)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for lines, received: %v", receivedLines)
	}
}
