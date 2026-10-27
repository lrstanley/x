// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeFFmpegEnv makes the test binary act as an ffmpeg 4.4 build that has
// libx264 only and refuses every encode, so missing-encoder and failed-encode
// paths run without a real ffmpeg on any platform.
const fakeFFmpegEnv = "EXPORT_TEST_FAKE_FFMPEG"

func TestMain(m *testing.M) {
	if os.Getenv(fakeFFmpegEnv) != "" {
		os.Exit(fakeFFmpeg(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeFFmpeg(args []string) int {
	switch {
	case slices.Contains(args, "-version"):
		fmt.Fprintln(os.Stdout, "ffmpeg version 4.4.2-0ubuntu0.22.04.1 Copyright (c) 2000-2021 the FFmpeg developers")
	case slices.Contains(args, "-encoders"):
		fmt.Fprint(os.Stdout, "Encoders:\n V..... = Video\n ------\n V....D libx264              libx264 H.264\n A....D aac                  AAC\n")
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(os.Stdin, 1<<10))
		fmt.Fprintln(os.Stderr, "fake ffmpeg: refusing to encode")
		return 1
	}
	return 0
}

// useFakeFFmpeg returns a WithFFmpeg option for the fake build. The child
// inherits the environment, so callers cannot run in parallel.
func useFakeFFmpeg(t *testing.T) Option {
	t.Helper()
	t.Setenv(fakeFFmpegEnv, "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return WithFFmpeg(exe)
}

// requireVideo skips the test unless ffmpeg can encode format.
func requireVideo(t *testing.T, format Format) *ffmpegInfo {
	t.Helper()
	if err := CheckVideo(format); err != nil {
		t.Skipf("skipping %s: %v", format, err)
	}
	info, err := findFFmpeg("", format)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// verifyVideo fails the test unless ffmpeg decodes path without errors.
// ffmpeg cannot decode animated WebP, so WebP is checked by [webpDelays].
func verifyVideo(t *testing.T, info *ffmpegInfo, path string) {
	t.Helper()
	out, err := exec.Command(info.path, "-v", "error", "-i", path, "-f", "null", "-").CombinedOutput() //nolint:gosec // test.
	if err != nil || len(bytes.TrimSpace(out)) > 0 {
		t.Fatalf("ffmpeg could not decode %s: %v\n%s", path, err, out)
	}
}

var (
	showinfoPTS   = regexp.MustCompile(`pts_time:\s*([0-9.]+)`)
	inputDuration = regexp.MustCompile(`Duration: (\d+):(\d+):([0-9.]+)`)
)

// videoTimeline decodes path and returns each frame's start time and the
// container duration.
func videoTimeline(t *testing.T, info *ffmpegInfo, path string) (starts []time.Duration, total time.Duration) {
	t.Helper()
	out, err := exec.Command(info.path, "-hide_banner", "-nostdin", "-i", path, "-vf", "showinfo", "-f", "null", "-").CombinedOutput() //nolint:gosec // test.
	if err != nil {
		t.Fatalf("decoding %s: %v\n%s", path, err, out)
	}
	for _, m := range showinfoPTS.FindAllSubmatch(out, -1) {
		starts = append(starts, parseSeconds(t, string(m[1])))
	}
	m := inputDuration.FindSubmatch(out)
	if m == nil {
		t.Fatalf("no duration for %s:\n%s", path, out)
	}
	h, _ := strconv.Atoi(string(m[1]))
	mins, _ := strconv.Atoi(string(m[2]))
	total = time.Duration(h)*time.Hour + time.Duration(mins)*time.Minute + parseSeconds(t, string(m[3]))
	return starts, total
}

func parseSeconds(t *testing.T, s string) time.Duration {
	t.Helper()
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatal(err)
	}
	return time.Duration(math.Round(f*1000)) * time.Millisecond
}

// decodeRGB decodes every frame of path as packed RGB.
func decodeRGB(t *testing.T, info *ffmpegInfo, path string, w, h int) [][]byte {
	t.Helper()
	args := append([]string{"-v", "error", "-i", path}, info.passthroughArgs()...)
	args = append(args, "-f", "rawvideo", "-pix_fmt", "rgb24", "-")
	out, err := exec.Command(info.path, args...).Output() //nolint:gosec // test.
	if err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	size := w * h * 3
	if len(out)%size != 0 {
		t.Fatalf("decoded %d bytes, not a multiple of %dx%d RGB frames", len(out), w, h)
	}
	var frames [][]byte
	for len(out) > 0 {
		frames = append(frames, out[:size])
		out = out[size:]
	}
	return frames
}

// webpDelays returns the frame durations and loop count of an animated WebP.
func webpDelays(t *testing.T, path string) (delays []time.Duration, loops int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		t.Fatalf("%s is not a WebP file", path)
	}
	loops = -1
	for i := 12; i+8 <= len(data); {
		id := string(data[i : i+4])
		size := int(binary.LittleEndian.Uint32(data[i+4:]))
		body := data[i+8 : min(i+8+size, len(data))]
		switch id {
		case "ANIM":
			loops = int(binary.LittleEndian.Uint16(body[4:]))
		case "ANMF":
			ms := int(body[12]) | int(body[13])<<8 | int(body[14])<<16
			delays = append(delays, time.Duration(ms)*time.Millisecond)
		}
		i += 8 + size + size&1
	}
	return delays, loops
}

var videoTestColors = []color.NRGBA{
	{R: 200, G: 40, B: 40, A: 0xff},
	{R: 40, G: 180, B: 90, A: 0xff},
	{R: 30, G: 60, B: 200, A: 0xff},
	{R: 230, G: 200, B: 40, A: 0xff},
}

var videoTestDelays = []time.Duration{2 * time.Second, 30 * time.Millisecond, 500 * time.Millisecond, 1500 * time.Millisecond}

// writeTestVideo encodes videoTestColors as w×h frames for videoTestDelays.
func writeTestVideo(t *testing.T, format Format, w, h int, opts ...Option) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out."+string(format))
	enc, err := CreateVideo(path, format, opts...)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range videoTestColors {
		if err := enc.WriteFrame(Frame{Image: solidNRGBA(w, h, c), Delay: videoTestDelays[i]}); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	assertOnlyFile(t, path)
	return path
}

// assertOnlyFile fails unless path is the only file in its directory, so no
// temporary output was left behind.
func assertOnlyFile(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != filepath.Base(path) {
		t.Fatalf("directory holds %v, want only %s", names, filepath.Base(path))
	}
}

func TestVideoTimelineFollowsDelays(t *testing.T) {
	t.Parallel()

	for _, format := range []Format{FormatMP4, FormatWebM} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			info := requireVideo(t, format)
			// An odd size covers the even-size pad.
			path := writeTestVideo(t, format, 63, 31)
			verifyVideo(t, info, path)

			starts, total := videoTimeline(t, info, path)
			want := []time.Duration{0, 2000 * time.Millisecond, 2030 * time.Millisecond, 2530 * time.Millisecond}
			if !slices.Equal(starts, want) {
				t.Fatalf("frame starts = %v, want %v", starts, want)
			}
			if total != 4030*time.Millisecond {
				t.Fatalf("duration = %v, want 4.03s", total)
			}
		})
	}
}

func TestVideoWebPDelaysAndLoop(t *testing.T) {
	t.Parallel()
	requireVideo(t, FormatWebP)

	for _, tc := range []struct {
		loop, want int
	}{
		{LoopForever, 0},
		{LoopOnce, 1},
		{2, 3},
	} {
		path := writeTestVideo(t, FormatWebP, 63, 31, WithLoopCount(tc.loop))
		delays, loops := webpDelays(t, path)
		if !slices.Equal(delays, videoTestDelays) {
			t.Fatalf("delays = %v, want %v", delays, videoTestDelays)
		}
		if loops != tc.want {
			t.Fatalf("WithLoopCount(%d) wrote loop count %d, want %d", tc.loop, loops, tc.want)
		}
	}
}

// TestVideoWebPMergedFrames covers libwebp_anim folding identical frames into
// one: the last frame still ends at the total of every delay.
func TestVideoWebPMergedFrames(t *testing.T) {
	t.Parallel()
	requireVideo(t, FormatWebP)

	path := filepath.Join(t.TempDir(), "merged.webp")
	enc, err := CreateVideo(path, FormatWebP, WithQuality(60))
	if err != nil {
		t.Fatal(err)
	}
	a, b := solidNRGBA(32, 32, videoTestColors[0]), solidNRGBA(32, 32, videoTestColors[1])
	for _, f := range []struct {
		img   image.Image
		delay time.Duration
	}{{a, 200 * time.Millisecond}, {b, 300 * time.Millisecond}, {b, 400 * time.Millisecond}, {b, 500 * time.Millisecond}} {
		if err := enc.WriteFrame(Frame{Image: f.img, Delay: f.delay}); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	delays, _ := webpDelays(t, path)
	var total time.Duration
	for _, d := range delays {
		total += d
	}
	if total != 1400*time.Millisecond || delays[0] != 200*time.Millisecond {
		t.Fatalf("delays = %v (total %v), want the first at 200ms and 1.4s in all", delays, total)
	}
}

// TestVideoColors checks that frames decode to the colors they were drawn
// with, which catches a wrong pixel layout or a color conversion that
// disagrees with the color tags players decode by.
func TestVideoColors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		format Format
		opts   []Option
	}{
		{FormatMP4, nil},
		{FormatMP4, []Option{WithFullChroma(true)}},
		{FormatWebM, nil},
		{FormatWebM, []Option{WithQuality(MaxQuality), WithFullChroma(true)}},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.format, len(tc.opts)), func(t *testing.T) {
			t.Parallel()
			info := requireVideo(t, tc.format)
			const w, h = 64, 48
			frames := decodeRGB(t, info, writeTestVideo(t, tc.format, w, h, tc.opts...), w, h)
			if len(frames) != len(videoTestColors) {
				t.Fatalf("decoded %d frames, want %d", len(frames), len(videoTestColors))
			}
			for i, want := range videoTestColors {
				px := frames[i][(h/2*w+w/2)*3:]
				got := color.NRGBA{R: px[0], G: px[1], B: px[2], A: 0xff}
				// Limited-range YUV rounds each channel by a step or two; a
				// mismatched matrix is off by far more on these colors.
				if !colorNear(got, want, 9) {
					t.Fatalf("frame %d center = %v, want %v", i, got, want)
				}
			}
		})
	}
}

// TestVideoCanvasFollowsFirstFrame covers frames that change size: each is
// drawn at the top left of the first frame's canvas and the rest padded.
func TestVideoCanvasFollowsFirstFrame(t *testing.T) {
	t.Parallel()
	info := requireVideo(t, FormatMP4)

	path := filepath.Join(t.TempDir(), "resize.mp4")
	enc, err := CreateVideo(path, FormatMP4, WithBackground(color.White), WithFullChroma(true))
	if err != nil {
		t.Fatal(err)
	}
	red := color.NRGBA{R: 220, A: 0xff}
	half := color.NRGBA{B: 0xff, A: 0x80}
	frames := []image.Image{
		solidNRGBA(32, 32, red),
		solidNRGBA(16, 16, red), // padded with white
		solidNRGBA(48, 48, red), // cropped
		solidNRGBA(32, 32, half),
		image.NewGray(image.Rect(0, 0, 32, 32)),
	}
	for _, img := range frames {
		if err := enc.WriteFrame(Frame{Image: img, Delay: 100 * time.Millisecond}); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	got := decodeRGB(t, info, path, 32, 32)
	if len(got) != len(frames) {
		t.Fatalf("decoded %d frames, want %d", len(got), len(frames))
	}
	at := func(frame, x, y int) color.NRGBA {
		px := got[frame][(y*32+x)*3:]
		return color.NRGBA{R: px[0], G: px[1], B: px[2], A: 0xff}
	}
	for _, tc := range []struct {
		frame, x, y int
		want        color.NRGBA
	}{
		{1, 4, 4, red},
		{1, 28, 28, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}},
		{2, 28, 28, red},
		{3, 16, 16, color.NRGBA{R: 0x7f, G: 0x7f, B: 0xff, A: 0xff}},
		{4, 16, 16, color.NRGBA{A: 0xff}},
	} {
		if c := at(tc.frame, tc.x, tc.y); !colorNear(c, tc.want, 9) {
			t.Fatalf("frame %d at %d,%d = %v, want %v", tc.frame, tc.x, tc.y, c, tc.want)
		}
	}
}

// TestVideoReusedImage checks that the encoder does not keep a frame's image
// past WriteFrame: one buffer, painted over after each frame, records every
// color.
func TestVideoReusedImage(t *testing.T) {
	t.Parallel()
	info := requireVideo(t, FormatWebM)

	path := filepath.Join(t.TempDir(), "rec.webm")
	enc, err := CreateVideo(path, FormatWebM)
	if err != nil {
		t.Fatal(err)
	}
	buf := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for i, c := range videoTestColors {
		paintRect(buf, buf.Rect, c)
		if err = enc.WriteFrame(Frame{Image: buf, Delay: videoTestDelays[i]}); err != nil {
			t.Fatal(err)
		}
		paintRect(buf, buf.Rect, color.NRGBA{A: 0xff})
	}
	if err = enc.Close(); err != nil {
		t.Fatal(err)
	}
	frames := decodeRGB(t, info, path, 40, 20)
	for i, want := range videoTestColors {
		px := frames[i][(10*40+20)*3:]
		if got := (color.NRGBA{R: px[0], G: px[1], B: px[2], A: 0xff}); !colorNear(got, want, 9) {
			t.Fatalf("frame %d = %v, want %v", i, got, want)
		}
	}
}

func TestVideoNoFramesWritesNothing(t *testing.T) {
	t.Parallel()
	requireVideo(t, FormatMP4)

	dir := t.TempDir()
	enc, err := CreateVideo(filepath.Join(dir, "empty.mp4"), FormatMP4)
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("dir holds %d entries, want none", len(entries))
	}
}

func TestVideoDiscardKeepsExistingFile(t *testing.T) {
	t.Parallel()
	requireVideo(t, FormatMP4)

	path := filepath.Join(t.TempDir(), "keep.mp4")
	if err := os.WriteFile(path, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	enc, err := CreateVideo(path, FormatMP4)
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteFrame(Frame{Image: solidNRGBA(16, 16, videoTestColors[0]), Delay: time.Second}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Discard(); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteFrame(Frame{Image: solidNRGBA(16, 16, videoTestColors[0]), Delay: time.Second}); err == nil {
		t.Fatal("expected error writing after Discard")
	}
	if data, _ := os.ReadFile(path); string(data) != "previous" {
		t.Fatalf("existing file = %q, want it untouched", data)
	}
	assertOnlyFile(t, path)
}

func TestVideoMissingFFmpeg(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "out.mp4")
	missing := WithFFmpeg(filepath.Join(dir, "no-ffmpeg"))
	for _, format := range videoFormats {
		if err := CheckVideo(format, missing); !errors.Is(err, ErrFFmpegNotFound) {
			t.Fatalf("CheckVideo(%s) = %v, want ErrFFmpegNotFound", format, err)
		}
		_, err := CreateVideo(path, format, missing)
		if !errors.Is(err, ErrFFmpegNotFound) {
			t.Fatalf("CreateVideo(%s) = %v, want ErrFFmpegNotFound", format, err)
		}
		if !strings.Contains(err.Error(), string(format)) || !strings.Contains(err.Error(), "WithFFmpeg") {
			t.Fatalf("error does not say what needs ffmpeg or how to set it: %v", err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("dir holds %d entries, want none", len(entries))
	}
}

func TestVideoMissingEncoder(t *testing.T) {
	fake := useFakeFFmpeg(t)

	dir := t.TempDir()
	_, err := CreateVideo(filepath.Join(dir, "out.webp"), FormatWebP, fake)
	var missing *MissingEncoderError
	if !errors.As(err, &missing) {
		t.Fatalf("CreateVideo = %v, want MissingEncoderError", err)
	}
	msg := err.Error()
	for _, want := range []string{"libwebp_anim or libwebp", "install an ffmpeg build"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not mention %q", msg, want)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("dir holds %d entries, want none", len(entries))
	}
	if err := CheckVideo(FormatMP4, fake); err != nil {
		t.Fatalf("CheckVideo(mp4) with libx264 = %v", err)
	}
}

// TestVideoFailedEncodeKeepsExistingFile runs an encode that ffmpeg rejects:
// the error carries ffmpeg's output and the destination is not replaced.
func TestVideoFailedEncodeKeepsExistingFile(t *testing.T) {
	fake := useFakeFFmpeg(t)

	path := filepath.Join(t.TempDir(), "keep.mp4")
	if err := os.WriteFile(path, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	enc, err := CreateVideo(path, FormatMP4, fake)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(enc.args(), "-vsync") {
		t.Fatalf("ffmpeg 4.4 args %v should use -vsync", enc.args())
	}
	// The fake exits after reading a little input, so a large frame fails
	// once the pipe closes. Small ones may only fail at Close.
	var writeErr error
	for range 4 {
		if writeErr = enc.WriteFrame(Frame{Image: solidNRGBA(256, 256, videoTestColors[0]), Delay: time.Second}); writeErr != nil {
			break
		}
	}
	closeErr := enc.Close()
	if closeErr == nil {
		t.Fatal("expected Close to fail")
	}
	if writeErr != nil && !errors.Is(closeErr, writeErr) {
		t.Fatalf("Close = %v, want the write error %v", closeErr, writeErr)
	}
	if !strings.Contains(closeErr.Error(), "refusing to encode") {
		t.Fatalf("error does not include ffmpeg's output: %v", closeErr)
	}
	if data, _ := os.ReadFile(path); string(data) != "previous" {
		t.Fatalf("existing file = %q, want it untouched", data)
	}
	assertOnlyFile(t, path)
}

func TestVideoRejectsBadOptions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "out.mp4")
	for _, opt := range []Option{WithQuality(-1), WithQuality(MaxQuality + 1), WithLoopCount(LoopOnce - 1)} {
		if _, err := CreateVideo(path, FormatMP4, opt); err == nil {
			t.Fatal("expected error for out-of-range option")
		}
	}
	if _, err := CreateVideo(path, "avi"); err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestVideoQualityArgs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		format Format
		opts   []Option
		want   []string
	}{
		{FormatMP4, nil, []string{"-crf", "18"}},
		{FormatMP4, []Option{WithQuality(0)}, []string{"-crf", "51"}},
		{FormatMP4, []Option{WithQuality(MaxQuality)}, []string{"-crf", "0"}},
		{FormatWebM, nil, []string{"-crf", "30"}},
		{FormatWebM, []Option{WithQuality(MaxQuality)}, []string{"-lossless", "1"}},
		{FormatWebP, nil, []string{"-lossless", "1"}},
		{FormatWebP, []Option{WithQuality(MaxQuality)}, []string{"-lossless", "1"}},
		{FormatWebP, []Option{WithQuality(50)}, []string{"-lossless", "0", "-quality", "50"}},
	} {
		cfg, err := collectOptions(validateVideo, tc.opts...)
		if err != nil {
			t.Fatal(err)
		}
		args := videoProfiles[tc.format].args(cfg)
		joined := " " + strings.Join(args, " ") + " "
		if !strings.Contains(joined, " "+strings.Join(tc.want, " ")+" ") {
			t.Fatalf("%s %d opts: args %v do not contain %v", tc.format, len(tc.opts), args, tc.want)
		}
	}
}

func TestFFmpegVersionParsing(t *testing.T) {
	t.Parallel()

	for version, want := range map[string]bool{
		"ffmpeg version 4.4.2-0ubuntu0.22.04.1 Copyright": false,
		"ffmpeg version 5.0.1 Copyright":                  false,
		"ffmpeg version 5.1.2 Copyright":                  true,
		"ffmpeg version n6.1 Copyright":                   true,
		"ffmpeg version 7.1-full_build-www.gyan.dev":      true,
		"ffmpeg version N-113000-g0123456789 Copyright":   true,
		"ffmpeg version 2024-01-01-git-abc Copyright":     true,
		"something else entirely":                         true,
	} {
		if got := ffmpegHasFPSMode([]byte(version + "\nbuilt with gcc")); got != want {
			t.Errorf("%q: fps_mode = %v, want %v", version, got, want)
		}
	}
}

func TestCompositeRow(t *testing.T) {
	t.Parallel()

	bg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	src := []byte{10, 20, 30, 0xff, 0, 0, 0, 0, 0, 0, 0xff, 0x80}
	dst := make([]byte, len(src))
	compositeRow(dst, src, bg)
	want := []byte{10, 20, 30, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f, 0x7f, 0xff, 0xff}
	if !bytes.Equal(dst, want) {
		t.Fatalf("compositeRow = %v, want %v", dst, want)
	}
}

func TestMkvSizes(t *testing.T) {
	t.Parallel()

	for n, want := range map[uint64][]byte{
		0:     {0x80},
		126:   {0xFE},
		127:   {0x40, 0x7F}, // 0xFF alone means unknown size
		16382: {0x7F, 0xFE},
		16383: {0x20, 0x3F, 0xFF},
	} {
		if got := appendMkvSize([]byte{0xAA}, n); !bytes.Equal(got, append([]byte{0xAA}, want...)) {
			t.Errorf("size %d = % x, want aa % x", n, got, want)
		}
	}
	if got := appendMkvID(nil, mkvBlock); !bytes.Equal(got, []byte{0xA1}) {
		t.Errorf("ID %x = % x", mkvBlock, got)
	}
	if got := appendMkvID([]byte{1}, mkvEBML); !bytes.Equal(got, []byte{1, 0x1A, 0x45, 0xDF, 0xA3}) {
		t.Errorf("ID %x = % x", mkvEBML, got)
	}
	if got := appendMkvUint(nil, 0); !bytes.Equal(got, []byte{0}) {
		t.Errorf("uint 0 = % x", got)
	}
	if got := appendMkvUint(nil, 1_000_000); !bytes.Equal(got, []byte{0x0F, 0x42, 0x40}) {
		t.Errorf("uint 1e6 = % x", got)
	}
}
