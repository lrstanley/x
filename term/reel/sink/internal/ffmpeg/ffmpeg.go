// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package ffmpeg probes ffmpeg binaries ([Probe]) and runs encodes ([Start])
// that read uncompressed Matroska ([MKV]) on stdin. Its errors reach sink's
// callers unwrapped, so they start with "sink: ".
package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// probeTimeout bounds each probe run, so a wedged binary fails setup.
const probeTimeout = 30 * time.Second

// stderrLimit is how much of the end of ffmpeg's stderr an encode error
// keeps.
const stderrLimit = 16 << 10

// Info is what probing one ffmpeg binary found.
type Info struct {
	Path     string
	encoders map[string]bool
	// fpsMode reports -fps_mode support (ffmpeg 5.1 and later). Older builds
	// only take -vsync, which newer ones deprecate.
	fpsMode bool
}

type cachedProbe struct {
	once sync.Once
	info *Info
	err  error
}

// probes caches probes by resolved binary path.
var (
	probesMu sync.Mutex
	probes   = map[string]*cachedProbe{}
)

// HasEncoder reports whether the binary has the video encoder name.
func (i *Info) HasEncoder(name string) bool { return i.encoders[name] }

// PassthroughArgs keeps every frame at its own timestamp, instead of ffmpeg
// resampling MP4 to a constant rate.
func (i *Info) PassthroughArgs() []string {
	if i.fpsMode {
		return []string{"-fps_mode", "passthrough"}
	}
	return []string{"-vsync", "passthrough"}
}

// Probe runs the ffmpeg binary at path for its version and encoders, once
// per path for the life of the process.
func Probe(path string) (*Info, error) {
	probesMu.Lock()
	p := probes[path]
	if p == nil {
		p = &cachedProbe{}
		probes[path] = p
	}
	probesMu.Unlock()
	p.once.Do(func() {
		p.info, p.err = probeBinary(path)
	})
	return p.info, p.err
}

func probeBinary(path string) (*Info, error) {
	version, err := run(path, "-hide_banner", "-version")
	if err != nil {
		return nil, err
	}
	encoders, err := run(path, "-hide_banner", "-encoders")
	if err != nil {
		return nil, err
	}
	return &Info{
		Path:     path,
		encoders: parseEncoders(encoders),
		fpsMode:  hasFPSMode(version),
	}, nil
}

func run(path string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sink: running %s %s: %w%s", path, strings.Join(args, " "), err, stderrSuffix(stderr.Bytes()))
	}
	return out, nil
}

// parseEncoders reads the video encoder names from "ffmpeg -encoders": lines
// after a "------" separator, as "<flags> <name> <description>", where video
// flags start with V.
func parseEncoders(out []byte) map[string]bool {
	encoders := make(map[string]bool)
	listed := false
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if !listed {
			listed = len(fields) == 1 && strings.HasPrefix(fields[0], "---")
			continue
		}
		if len(fields) >= 2 && strings.HasPrefix(fields[0], "V") {
			encoders[fields[1]] = true
		}
	}
	return encoders
}

// hasFPSMode reports whether "ffmpeg -version" output is from 5.1 or later.
// Versions that are not a release number (git snapshots) are assumed recent.
func hasFPSMode(version []byte) bool {
	line, _, _ := bytes.Cut(version, []byte("\n"))
	fields := strings.Fields(string(line))
	if len(fields) < 3 || fields[0] != "ffmpeg" || fields[1] != "version" {
		return true
	}
	v := strings.TrimPrefix(fields[2], "n")
	majorStr, rest, _ := strings.Cut(v, ".")
	major, err := strconv.Atoi(majorStr)
	if err != nil {
		return true
	}
	if i := strings.IndexFunc(rest, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
		rest = rest[:i]
	}
	minor, _ := strconv.Atoi(rest)
	return major > 5 || (major == 5 && minor >= 1)
}

// Job is one running encode: ffmpeg reads a stream on stdin and writes
// the output file itself.
type Job struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stderr   *tailBuffer
	waitOnce sync.Once
	waitErr  error
}

// Start runs the ffmpeg binary at path with args, which name the output.
func Start(path string, args []string) (*Job, error) {
	// No context: the encode runs until its input closes, and Kill stops it.
	cmd := exec.Command(path, args...) //nolint:gosec,noctx // path is from LookPath, args from fixed profiles.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stderr := &tailBuffer{limit: stderrLimit}
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sink: starting %s: %w", path, err)
	}
	return &Job{cmd: cmd, stdin: stdin, stderr: stderr}, nil
}

// Stdin is ffmpeg's input, which [Job.End] and [Job.Kill] close.
func (j *Job) Stdin() io.Writer { return j.stdin }

func (j *Job) wait() error {
	j.waitOnce.Do(func() {
		j.waitErr = j.cmd.Wait()
	})
	return j.waitErr
}

// End closes ffmpeg's input and waits for the encode to finish. cause is why
// the caller is ending early, such as a failed write; ffmpeg's exit error wins
// over it, since a write usually fails because ffmpeg exited.
func (j *Job) End(cause error) error {
	if err := j.stdin.Close(); cause == nil && !errors.Is(err, os.ErrClosed) {
		cause = err
	}
	if err := j.wait(); err != nil {
		return j.wrap(err)
	}
	if cause != nil {
		return j.wrap(cause)
	}
	return nil
}

// Kill stops ffmpeg without letting it finish the file.
func (j *Job) Kill() {
	_ = j.cmd.Process.Kill()
	_ = j.stdin.Close()
	_ = j.wait()
}

func (j *Job) wrap(err error) error {
	return fmt.Errorf("sink: ffmpeg encode failed: %w%s", err, stderrSuffix(j.stderr.Bytes()))
}

func stderrSuffix(stderr []byte) string {
	stderr = bytes.TrimSpace(stderr)
	if len(stderr) == 0 {
		return ""
	}
	return "\nffmpeg output:\n" + string(stderr)
}

// tailBuffer keeps the last limit bytes written to it. It is locked, as
// os/exec writes stderr from its own goroutine.
type tailBuffer struct {
	mu    sync.Mutex
	limit int
	buf   []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.limit; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) Bytes() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return bytes.Clone(t.buf)
}
