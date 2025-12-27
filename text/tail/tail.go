// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package tail

import (
	"bufio"
	"context"
	"errors"
	"io"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Config holds configuration for the tail function.
type Config struct {
	// SplitFunc is the function used to split the input into tokens. If nil,
	// [bufio.ScanLines] is used.
	SplitFunc bufio.SplitFunc

	// RecheckDelay is the delay between retry attempts when the file is temporarily
	// unavailable or has been moved/deleted.
	RecheckDelay time.Duration

	// ReadFromStart, if true, causes the watcher to read from the beginning of the
	// file when it's newly created or truncated. If false (default), it skips the
	// content and only reads new data appended after the event.
	ReadFromStart bool

	// Logger is used for logging. If nil, no logging is performed.
	Logger *slog.Logger
}

// Watcher monitors a file and yields new lines as they are written.
type Watcher struct {
	config          *Config
	path            string
	file            *os.File
	scanner         *bufio.Scanner
	filePos         int64
	fileJustCreated bool
	watcher         *fsnotify.Watcher
}

// NewWatcher creates a new Watcher for the given path with the provided config.
func NewWatcher(config *Config, path string) (*Watcher, error) {
	if config == nil {
		config = &Config{}
	}

	if config.SplitFunc == nil {
		config.SplitFunc = bufio.ScanLines
	}

	if config.RecheckDelay <= 0 {
		config.RecheckDelay = 100 * time.Millisecond
	}

	if config.Logger == nil {
		config.Logger = slog.New(slog.DiscardHandler)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	// Create watcher for the directory.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Watch the directory, not the file directly.
	err = watcher.Add(filepath.Dir(absPath))
	if err != nil {
		_ = watcher.Close()
		return nil, err
	}

	return &Watcher{
		config:          config,
		path:            absPath,
		watcher:         watcher,
		fileJustCreated: false,
	}, nil
}

// Close closes the watcher and any open file handles.
func (w *Watcher) Close() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	if w.watcher != nil {
		return w.watcher.Close()
	}
	return nil
}

// Watch monitors a file and yields new lines as they are written. It returns an
// iterator sequence that yields []byte chunks (as split by SplitFunc) and error
// values.
//
// The function only returns errors for permission or access issues. It does not
// return errors if the file doesn't exist or EOF is hit; instead, it waits for
// the file to reappear or for new data.
//
// The function gracefully handles:
//   - File moved/renamed: waits for file to reappear at original path.
//   - File deleted: waits for file to reappear.
//   - File truncated: resets read position to beginning.
func Watch(ctx context.Context, config *Config, path string) iter.Seq2[[]byte, error] {
	w, err := NewWatcher(config, path)
	if err != nil {
		return func(yield func([]byte, error) bool) {
			yield(nil, err)
		}
	}
	return func(yield func([]byte, error) bool) {
		defer w.Close()
		for line, err := range w.Start(ctx) {
			if !yield(line, err) {
				return
			}
		}
	}
}

// Start begins monitoring the file and returns an iterator sequence. It yields
// []byte chunks (as split by SplitFunc) and error values.
//
// The function only returns errors for permission or access issues. It does not
// return errors if the file doesn't exist or EOF is hit; instead, it waits for
// the file to reappear or for new data.
//
// The function gracefully handles:
//   - File moved/renamed: waits for file to reappear at original path.
//   - File deleted: waits for file to reappear.
//   - File truncated: resets read position to beginning.
func (w *Watcher) Start(ctx context.Context) iter.Seq2[[]byte, error] { //nolint:gocognit
	return func(yield func([]byte, error) bool) {
		// Try to open file initially.
		err := w.openFile(ctx)
		if err != nil {
			if !yield(nil, err) {
				return
			}
			return
		}

		// If file doesn't exist initially, wait for it.
		if w.file == nil {
			if !w.waitForFile(ctx, yield) {
				return
			}
		}

		var event fsnotify.Event
		var ok bool

		for {
			select {
			case <-ctx.Done():
				if w.file != nil {
					_ = w.file.Close()
					w.file = nil
				}
				return
			case event, ok = <-w.watcher.Events:
				if !ok {
					if w.file != nil {
						_ = w.file.Close()
						w.file = nil
					}
					return
				}

				// Only process events for our target file.
				if filepath.Base(event.Name) != filepath.Base(w.path) {
					continue
				}

				w.config.Logger.DebugContext(ctx, "file event", "path", event.Name, "op", event.Op)

				switch {
				case event.Has(fsnotify.Write):
					if !w.handleWriteEvent(ctx, event, yield) {
						return
					}

				case event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename):
					if !w.handleRemoveRenameEvent(ctx, event, yield) {
						return
					}

				case event.Has(fsnotify.Create):
					if !w.handleCreateEvent(ctx, event, yield) {
						return
					}
				}
			case err, ok = <-w.watcher.Errors:
				if !ok {
					if w.file != nil {
						_ = w.file.Close()
						w.file = nil
					}
					return
				}
				w.config.Logger.DebugContext(ctx, "watcher error", "error", err)
				// Watcher errors are typically not fatal, continue monitoring.
			}
		}
	}
}

// openFile opens and positions the file at the end.
func (w *Watcher) openFile(ctx context.Context) error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
		w.scanner = nil
	}

	f, err := os.Open(w.path)
	if err != nil {
		// Check if it's a permission/access error.
		if errors.Is(err, os.ErrPermission) {
			// Permission error should be returned.
			return err
		}
		// ErrNotExist is not a permission error, we'll wait for file to appear.
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		// Check for other access-related errors.
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			// Check if it's an access-related error.
			if errors.Is(pathErr.Err, os.ErrPermission) {
				return err
			}
		}
		// For other errors, return them as they might be access issues.
		return err
	}

	// Seek to end of file (like tail -f).
	pos, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		_ = f.Close()
		return err
	}

	w.file = f
	w.filePos = pos
	w.scanner = bufio.NewScanner(w.file)
	w.scanner.Split(w.config.SplitFunc)

	w.config.Logger.DebugContext(ctx, "opened file", "path", w.path, "position", pos)

	return nil
}

// waitForFile waits for the file to appear if it doesn't exist initially.
func (w *Watcher) waitForFile(ctx context.Context, yield func([]byte, error) bool) bool {
	w.config.Logger.DebugContext(ctx, "file does not exist, waiting", "path", w.path)

	w.fileJustCreated = true // File doesn't exist, so when it's created, it's "just created"

	// Wait for file to appear.
	for w.file == nil {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(w.config.RecheckDelay):
			err := w.openFile(ctx)
			if err != nil {
				if !yield(nil, err) {
					return false
				}
				return false
			}
		case event, ok := <-w.watcher.Events:
			if !ok {
				return false
			}
			// Only process events for our target file.
			if filepath.Base(event.Name) == filepath.Base(w.path) && event.Has(fsnotify.Create) {
				err := w.openFile(ctx)
				if err != nil {
					if !yield(nil, err) {
						return false
					}
					return false
				}
			}
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return false
			}
			w.config.Logger.DebugContext(ctx, "watcher error", "error", err)
		}
	}
	return true
}

// handleWriteEvent handles file write events.
func (w *Watcher) handleWriteEvent(ctx context.Context, _ fsnotify.Event, yield func([]byte, error) bool) bool {
	// File was written to.
	wasNil := w.file == nil

	// If file was just created (even if already opened by CREATE event), read initial data
	// if configured to read from start.
	if w.file != nil && w.fileJustCreated {
		w.fileJustCreated = false
		if w.config.ReadFromStart {
			return w.readInitialData(ctx, yield)
		}
		// Not reading from start, so just continue to check for new writes
	}

	if w.file == nil {
		// File was created or reappeared.
		err := w.openFile(ctx)
		if err != nil {
			return yield(nil, err)
		}
		if w.file == nil {
			// Still doesn't exist, wait.
			return true
		}
		// After opening and seeking to end, check if there's data that was written
		// during creation (before we opened it). Only read from beginning if file
		// was just created (didn't exist initially).
		if w.fileJustCreated {
			readData := w.readInitialData(ctx, yield)
			if !readData {
				return false
			}
			w.fileJustCreated = false
			// Don't continue to read below - we've already read initial data. The
			// Write event that triggered this might have more data, but we'll
			// catch it on the next Write event.
			return true
		} else if wasNil {
			// File was reopened but wasn't "just created", reset flag.
			w.fileJustCreated = false
		}
	}

	// Check for truncation and get current file size.
	info, err := w.file.Stat()
	if err != nil {
		// File might have been deleted
		if errors.Is(err, os.ErrNotExist) {
			_ = w.file.Close()
			w.file = nil
			w.scanner = nil
			return true
		}
		return yield(nil, err)
	}

	// Check for truncation.
	if !w.checkTruncation(ctx, info, yield) {
		return false
	}

	// Ensure scanner is set up.
	if w.scanner == nil {
		w.scanner = bufio.NewScanner(w.file)
		w.scanner.Split(w.config.SplitFunc)
	}

	// Read all available new data.
	return w.readNewData(ctx, yield)
}

// readInitialData reads initial data from a just-created file.
func (w *Watcher) readInitialData(_ context.Context, yield func([]byte, error) bool) bool {
	info, err := w.file.Stat()
	if err != nil || info.Size() == 0 {
		return true
	}

	// File has data. For tail behavior, we should read data written after we start
	// watching. Since the file was just created, all its data is "new". Reset to
	// beginning to read all data.
	_, err = w.file.Seek(0, io.SeekStart)
	if err != nil {
		return true
	}

	w.filePos = 0
	w.scanner = bufio.NewScanner(w.file)
	w.scanner.Split(w.config.SplitFunc)

	// Read all existing data.
	for w.scanner.Scan() {
		data := w.scanner.Bytes()
		dataCopy := make([]byte, len(data))
		copy(dataCopy, data)
		if !yield(dataCopy, nil) {
			return false
		}
		w.filePos, _ = w.file.Seek(0, io.SeekCurrent)
	}

	// After reading, seek to end for future tailing.
	w.filePos, _ = w.file.Seek(0, io.SeekEnd)
	w.scanner = bufio.NewScanner(w.file)
	w.scanner.Split(w.config.SplitFunc)

	return true
}

// checkTruncation checks if the file was truncated and handles it.
func (w *Watcher) checkTruncation(ctx context.Context, info os.FileInfo, yield func([]byte, error) bool) bool {
	// If file size is less than our position, it was truncated.
	if info.Size() >= w.filePos {
		return true
	}

	w.config.Logger.DebugContext(
		ctx, "file truncated, resetting position",
		"path", w.path,
		"old_pos", w.filePos,
		"new_size", info.Size(),
	)

	// Reset to beginning.
	_, err := w.file.Seek(0, io.SeekStart)
	if err != nil {
		return yield(nil, err)
	}
	w.filePos = 0
	w.scanner = bufio.NewScanner(w.file)
	w.scanner.Split(w.config.SplitFunc)

	if info.Size() == 0 {
		return true
	}

	// After truncation, read all available data if configured to read from start.
	if w.config.ReadFromStart {
		for w.scanner.Scan() {
			data := w.scanner.Bytes()
			dataCopy := make([]byte, len(data))
			copy(dataCopy, data)
			if !yield(dataCopy, nil) {
				return false
			}
			w.filePos, _ = w.file.Seek(0, io.SeekCurrent)
		}

		err = w.scanner.Err()
		if err != nil && !errors.Is(err, io.EOF) {
			if errors.Is(err, os.ErrPermission) {
				return yield(nil, err)
			}
			w.config.Logger.DebugContext(ctx, "scanner error after truncation", "error", err)
		}
	} else {
		// Not reading from start after truncation. Seek to end to only read new appends.
		w.filePos, _ = w.file.Seek(0, io.SeekEnd)
		w.scanner = bufio.NewScanner(w.file)
		w.scanner.Split(w.config.SplitFunc)
	}
	return true
}

// readNewData reads all available new data from the file.
func (w *Watcher) readNewData(ctx context.Context, yield func([]byte, error) bool) bool {
	// Create a fresh scanner to pick up new data. The scanner maintains internal
	// EOF state, so we need to recreate it when the file has grown.
	w.scanner = bufio.NewScanner(w.file)
	w.scanner.Split(w.config.SplitFunc)

	// Read all available new data. Keep reading until we've consumed all new data.
	maxIterations := 100 // Prevent infinite loops.
	iteration := 0
	readSomething := false
	for iteration < maxIterations {
		iteration++

		// Check current position and file size.
		currentPos, err := w.file.Seek(0, io.SeekCurrent)
		if err != nil {
			break
		}

		// Re-check file size in case more data was written.
		newInfo, err := w.file.Stat()
		if err != nil {
			break
		}

		// If we're at or past the end, check if we read anything.
		if currentPos >= newInfo.Size() {
			// Update filePos.
			w.filePos = currentPos
			// If we read something, we might have more in the scanner buffer. If
			// we didn't read anything and we're at the end, we're done.
			if !readSomething {
				break
			}
			// Continue to try reading from scanner buffer.
		}

		// Try to scan a line/token.
		if !w.scanner.Scan() {
			if scanErr := w.scanner.Err(); scanErr != nil {
				if errors.Is(scanErr, io.EOF) {
					// EOF is expected when we've read all available data.
					w.filePos, _ = w.file.Seek(0, io.SeekCurrent)
					break
				}
				// Check if it's a permission/access error.
				if errors.Is(scanErr, os.ErrPermission) {
					if !yield(nil, scanErr) {
						return false
					}
					break
				}
				// Other errors might be transient, log and continue.
				w.config.Logger.DebugContext(ctx, "scanner error", "error", scanErr)
				break
			}
			// No more data to read right now. Update position.
			w.filePos, _ = w.file.Seek(0, io.SeekCurrent)

			// Check if we're at or past EOF to avoid infinite loop.
			checkPos, _ := w.file.Seek(0, io.SeekCurrent)
			checkInfo, _ := w.file.Stat()
			if checkInfo != nil && checkPos >= checkInfo.Size() {
				// At EOF, no more data available.
				break
			}

			// If we read something, check file size again in case more was written.
			if readSomething {
				continue
			}
			break
		}

		readSomething = true

		// Got a token, yield it.
		data := w.scanner.Bytes()

		// Make a copy since scanner reuses the buffer.
		dataCopy := make([]byte, len(data))
		copy(dataCopy, data)
		if !yield(dataCopy, nil) {
			return false
		}

		// Update position after reading.
		newPos, err := w.file.Seek(0, io.SeekCurrent)
		if err == nil {
			w.filePos = newPos
		} else {
			break
		}
	}
	return true
}

// handleRemoveRenameEvent handles file remove/rename events.
func (w *Watcher) handleRemoveRenameEvent(ctx context.Context, _ fsnotify.Event, yield func([]byte, error) bool) bool {
	// File was removed or renamed.
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
		w.scanner = nil
	}

	w.config.Logger.DebugContext(ctx, "file removed/renamed, waiting for reappearance", "path", w.path)

	// Wait for file to reappear.
	fileReappeared := false
	for !fileReappeared {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(w.config.RecheckDelay):
			err := w.openFile(ctx)
			if err != nil {
				if !yield(nil, err) {
					return false
				}
				return false
			}
			if w.file != nil {
				// File reappeared. Mark as just created so initial content can be read if configured.
				w.config.Logger.DebugContext(ctx, "file reappeared", "path", w.path)
				w.fileJustCreated = true
				fileReappeared = true
			}
		}
	}
	return true
}

// handleCreateEvent handles file create events.
func (w *Watcher) handleCreateEvent(ctx context.Context, _ fsnotify.Event, yield func([]byte, error) bool) bool {
	if w.file != nil {
		return true
	}
	err := w.openFile(ctx)
	if err != nil {
		return yield(nil, err)
	}
	if w.file == nil {
		return true
	}

	// After opening and seeking to end, check if there's data. If file was
	// created with content, we're at the end, so no data to read But if data
	// is written after creation, we'll catch it in Write event.
	return true
}
