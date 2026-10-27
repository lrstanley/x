// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

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

// ErrFFmpegNotFound is wrapped by the error [CreateVideo] and [CheckVideo]
// return when the ffmpeg binary is not on PATH (or not at [WithFFmpeg]).
var ErrFFmpegNotFound = errors.New("sink: ffmpeg not found")

// ffmpegProbeTimeout bounds each probe run, so a wedged binary fails setup
// instead of hanging it.
const ffmpegProbeTimeout = 30 * time.Second

// ffmpegStderrLimit is how much of ffmpeg's stderr an encode error keeps: the
// end, where ffmpeg reports why it stopped.
const ffmpegStderrLimit = 16 << 10

// MissingEncoderError reports an ffmpeg build without any encoder a format
// can use.
type MissingEncoderError struct {
	// Format is the requested video format.
	Format Format
	// Encoders are the ffmpeg encoders the format can use, preferred first.
	Encoders []string
	// FFmpeg is the path of the ffmpeg binary that was probed.
	FFmpeg string
}

func (e *MissingEncoderError) Error() string {
	return fmt.Sprintf(
		"sink: %s needs ffmpeg's %s encoder, but %s was built without it; install an ffmpeg build that includes it",
		e.Format, strings.Join(e.Encoders, " or "), e.FFmpeg,
	)
}

// ffmpegInfo is what probing one ffmpeg binary found.
type ffmpegInfo struct {
	path     string
	encoders map[string]bool
	// fpsMode reports -fps_mode support (ffmpeg 5.1 and later). Older builds
	// only take -vsync, which newer ones deprecate.
	fpsMode bool
}

type ffmpegProbe struct {
	once sync.Once
	info *ffmpegInfo
	err  error
}

// ffmpegProbes caches probes by resolved binary path, so every recording in a
// process probes a binary once.
var (
	ffmpegProbesMu sync.Mutex
	ffmpegProbes   = map[string]*ffmpegProbe{}
)

// findFFmpeg resolves the ffmpeg binary (name on PATH, or a path) and probes
// it once.
func findFFmpeg(name string, format Format) (*ffmpegInfo, error) {
	if name == "" {
		name = "ffmpeg"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %s export needs ffmpeg; install it (https://ffmpeg.org/download.html or your package manager) and put it on PATH, or point WithFFmpeg at it (%w)",
			ErrFFmpegNotFound, format, err,
		)
	}
	ffmpegProbesMu.Lock()
	probe := ffmpegProbes[path]
	if probe == nil {
		probe = &ffmpegProbe{}
		ffmpegProbes[path] = probe
	}
	ffmpegProbesMu.Unlock()
	probe.once.Do(func() {
		probe.info, probe.err = probeFFmpeg(path)
	})
	return probe.info, probe.err
}

// passthroughArgs keeps every frame at its own timestamp. Without them,
// ffmpeg resamples MP4 to a constant rate by repeating and dropping frames.
func (i *ffmpegInfo) passthroughArgs() []string {
	if i.fpsMode {
		return []string{"-fps_mode", "passthrough"}
	}
	return []string{"-vsync", "passthrough"}
}

func probeFFmpeg(path string) (*ffmpegInfo, error) {
	version, err := runFFmpeg(path, "-hide_banner", "-version")
	if err != nil {
		return nil, err
	}
	encoders, err := runFFmpeg(path, "-hide_banner", "-encoders")
	if err != nil {
		return nil, err
	}
	return &ffmpegInfo{
		path:     path,
		encoders: parseFFmpegEncoders(encoders),
		fpsMode:  ffmpegHasFPSMode(version),
	}, nil
}

func runFFmpeg(path string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ffmpegProbeTimeout)
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

// parseFFmpegEncoders reads the video encoder names from "ffmpeg -encoders".
// Each entry follows a "------" separator as "<flags> <name> <description>",
// where flags start with V for video.
func parseFFmpegEncoders(out []byte) map[string]bool {
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

// ffmpegHasFPSMode reports whether "ffmpeg -version" output is from 5.1 or
// later. Builds whose version is not a release number (git snapshots such as
// "N-113000-g..." or "2024-01-01-git-...") are assumed to be recent.
func ffmpegHasFPSMode(version []byte) bool {
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

// ffmpegJob is one running encode: ffmpeg reads a stream on stdin and writes
// the output file itself.
type ffmpegJob struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *tailBuffer
	// waitErr holds the result of cmd.Wait once it has run.
	waitOnce sync.Once
	waitErr  error
}

func startFFmpeg(path string, args []string) (*ffmpegJob, error) {
	// No context: the encode runs until its input closes, and kill stops it.
	cmd := exec.Command(path, args...) //nolint:gosec,noctx // path is from LookPath, args from fixed profiles.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stderr := &tailBuffer{limit: ffmpegStderrLimit}
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sink: starting %s: %w", path, err)
	}
	return &ffmpegJob{cmd: cmd, stdin: stdin, stderr: stderr}, nil
}

func (j *ffmpegJob) wait() error {
	j.waitOnce.Do(func() {
		j.waitErr = j.cmd.Wait()
	})
	return j.waitErr
}

// end closes ffmpeg's input, so it flushes the encode, and waits for it.
// cause is why the caller is ending early, such as a failed write; ffmpeg's
// own exit error wins over it, since a write usually fails because ffmpeg
// exited.
func (j *ffmpegJob) end(cause error) error {
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

// kill stops ffmpeg without letting it finish the file.
func (j *ffmpegJob) kill() {
	_ = j.cmd.Process.Kill()
	_ = j.stdin.Close()
	_ = j.wait()
}

func (j *ffmpegJob) wrap(err error) error {
	return fmt.Errorf("sink: ffmpeg encode failed: %w%s", err, stderrSuffix(j.stderr.Bytes()))
}

func stderrSuffix(stderr []byte) string {
	stderr = bytes.TrimSpace(stderr)
	if len(stderr) == 0 {
		return ""
	}
	return "\nffmpeg output:\n" + string(stderr)
}

// tailBuffer keeps the last limit bytes written to it. os/exec copies stderr
// from its own goroutine, so it is locked.
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
