// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"strconv"
	"time"
)

// VideoFormat is a recording format [CreateVideo] encodes through ffmpeg.
type VideoFormat string

const (
	// VideoMP4 is H.264 in MP4 (libx264), which plays nearly everywhere.
	VideoMP4 VideoFormat = "mp4"
	// VideoWebM is VP9 in WebM (libvpx-vp9).
	VideoWebM VideoFormat = "webm"
	// VideoWebP is animated WebP (libwebp). It keeps transparency and
	// [WithLoopCount], but ffmpeg builds without libwebp are common.
	VideoWebP VideoFormat = "webp"
)

var videoFormats = []VideoFormat{VideoMP4, VideoWebM, VideoWebP}

// videoProfile is how one [VideoFormat] is encoded.
type videoProfile struct {
	// muxer is ffmpeg's output format name.
	muxer string
	// encoders are the ffmpeg encoders the format can use, preferred first.
	encoders []string
	// alpha reports whether the format keeps transparency. Formats without it
	// are encoded as 4:2:0 (or 4:4:4) YUV, which also needs even dimensions at
	// 4:2:0.
	alpha bool
	// args returns the encoder arguments after "-c:v <encoder>".
	args func(o options) []string
}

var videoProfiles = map[VideoFormat]videoProfile{
	VideoMP4: {
		muxer:    "mp4",
		encoders: []string{"libx264"},
		args: func(o options) []string {
			crf := 18
			if o.qualitySet {
				crf = qualityToCRF(o.quality, 51)
			}
			return []string{
				"-crf", strconv.Itoa(crf),
				"-movflags", "+faststart",
				// MP4 otherwise picks a track timescale from a guessed frame
				// rate, which rounds the millisecond delays.
				"-video_track_timescale", "1000",
			}
		},
	},
	VideoWebM: {
		muxer:    "webm",
		encoders: []string{"libvpx-vp9"},
		args: func(o options) []string {
			crf := 30
			if o.qualitySet {
				crf = qualityToCRF(o.quality, 63)
			}
			args := []string{"-crf", strconv.Itoa(crf), "-b:v", "0", "-row-mt", "1"}
			if o.qualitySet && o.quality == MaxQuality {
				args = append(args, "-lossless", "1")
			}
			return args
		},
	},
	VideoWebP: {
		muxer: "webp",
		// libwebp_anim stores only what changed between frames, which makes
		// a terminal recording about a seventh the size of libwebp's full
		// frames. Both come with ffmpeg's libwebp support, so libwebp is a
		// fallback for unusual builds.
		encoders: []string{"libwebp_anim", "libwebp"},
		alpha:    true,
		// The loop count is not passed to ffmpeg: its webp muxer ignores it
		// for libwebp_anim, so [fixWebP] writes it instead.
		args: func(o options) []string {
			// Lossless is the default: on terminal frames it is smaller than
			// lossy at any quality worth using, and exact.
			if o.qualitySet && o.quality < MaxQuality {
				return []string{"-lossless", "0", "-quality", strconv.Itoa(o.quality)}
			}
			return []string{"-lossless", "1"}
		},
	},
}

// qualityToCRF maps [WithQuality] onto a CRF scale where 0 is best and
// worst is the encoder's largest value.
func qualityToCRF(q, worst int) int {
	return int(math.Round(float64(MaxQuality-q) * float64(worst) / MaxQuality))
}

// webpLoopCount converts a [WithLoopCount] value, which counts repeats like
// GIF, into WebP's count of plays (0 is forever).
func webpLoopCount(n int) int {
	switch n {
	case LoopForever:
		return 0
	case LoopOnce:
		return 1
	}
	return min(n+1, maxGIFLoopCount)
}

func lookupVideoProfile(format VideoFormat) (videoProfile, error) {
	p, ok := videoProfiles[format]
	if !ok {
		return videoProfile{}, fmt.Errorf("sink: unknown video format %q", format)
	}
	return p, nil
}

// findVideoEncoder probes ffmpeg and picks the first encoder p can use.
func findVideoEncoder(format VideoFormat, p videoProfile, o options) (*ffmpegInfo, string, error) {
	info, err := findFFmpeg(o.ffmpeg, format)
	if err != nil {
		return nil, "", err
	}
	for _, enc := range p.encoders {
		if info.encoders[enc] {
			return info, enc, nil
		}
	}
	return nil, "", &MissingEncoderError{Format: format, Encoders: p.encoders, FFmpeg: info.path}
}

// CheckVideo reports whether [CreateVideo] can encode format with opts: that
// ffmpeg is installed and has an encoder for it. It runs ffmpeg only to probe
// it, once per binary.
func CheckVideo(format VideoFormat, opts ...Option) error {
	cfg, err := collectOptions(validateVideo, opts...)
	if err != nil {
		return err
	}
	p, err := lookupVideoProfile(format)
	if err != nil {
		return err
	}
	_, _, err = findVideoEncoder(format, p, cfg)
	return err
}

// VideoEncoder is an [Encoder] that encodes frames with ffmpeg as they
// arrive. Frames stream to ffmpeg uncompressed, with their delays, so a hold
// stays a single frame and the timeline matches the delays given. The canvas
// is the first frame's size: later frames of another size are anchored at the
// top left, cropped or padded with the background.
//
// ffmpeg writes to a temporary file beside the destination, which
// [VideoEncoder.Close] renames into place once the encode succeeds, so a
// failed or discarded encode leaves an existing file alone.
type VideoEncoder struct {
	opts    options
	format  VideoFormat
	profile videoProfile
	info    *ffmpegInfo
	encoder string
	file    *pendingFile

	job    *ffmpegJob
	mkv    mkvWriter
	bounds image.Rectangle
	// canvas holds frames that cannot be streamed as is.
	canvas *image.NRGBA
	// background is the compositing and padding color.
	background color.NRGBA
	frames     int
	// err is the first encode error; once set, frames are rejected.
	err  error
	done bool
}

var _ Encoder = (*VideoEncoder)(nil)

// CreateVideo returns an encoder that writes frames to path as format. It
// finds and probes ffmpeg first, so a missing binary (wrapping
// [ErrFFmpegNotFound]) or encoder ([MissingEncoderError]) is reported here,
// before any frames are recorded. ffmpeg itself starts with the first frame.
//
// It reads [WithQuality], [WithFullChroma], [WithBackground], [WithFFmpeg],
// and, for WebP, [WithLoopCount]. Other options are ignored.
func CreateVideo(path string, format VideoFormat, opts ...Option) (*VideoEncoder, error) {
	cfg, err := collectOptions(validateVideo, opts...)
	if err != nil {
		return nil, err
	}
	p, err := lookupVideoProfile(format)
	if err != nil {
		return nil, err
	}
	info, encoder, err := findVideoEncoder(format, p, cfg)
	if err != nil {
		return nil, err
	}
	file, err := createPending(path)
	if err != nil {
		return nil, fmt.Errorf("sink: creating %s output: %w", format, err)
	}
	// ffmpeg opens the file by name, so it is the only writer.
	if err := file.f.Close(); err != nil {
		_ = file.abort()
		return nil, err
	}
	s := &VideoEncoder{
		opts:       cfg,
		format:     format,
		profile:    p,
		info:       info,
		encoder:    encoder,
		file:       file,
		background: color.NRGBA{A: 0xff},
	}
	if cfg.backgroundSet {
		s.background = opaqueNRGBA(cfg.background)
	}
	return s, nil
}

// WriteFrame encodes f for f.Delay, truncated to whole milliseconds (at least
// one). It returns once ffmpeg has read the frame, so f.Image may be reused.
// [Frame.Isolated] is ignored.
func (s *VideoEncoder) WriteFrame(f Frame) error {
	img := f.Image
	switch {
	case s.done:
		return errors.New("sink: write to closed video encoder")
	case s.err != nil:
		return s.err
	case img == nil:
		return errNilFrame
	case img.Bounds().Empty():
		return errEmptyFrame
	}
	if s.job == nil {
		if err := s.start(img.Bounds().Size()); err != nil {
			s.err = err
			return err
		}
	}
	src := s.streamable(img)
	w, h := s.bounds.Dx(), s.bounds.Dy()
	err := s.mkv.writeFrame(w*h*4, f.Delay, func(out io.Writer) error {
		row := w * 4
		off := src.PixOffset(src.Rect.Min.X, src.Rect.Min.Y)
		if src.Stride == row {
			_, err := out.Write(src.Pix[off : off+row*h])
			return err
		}
		for y := range h {
			if _, err := out.Write(src.Pix[off+y*src.Stride : off+y*src.Stride+row]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.err = s.job.end(err)
		return s.err
	}
	s.frames++
	return nil
}

// start launches ffmpeg for a canvas of size and writes the stream header.
func (s *VideoEncoder) start(size image.Point) error {
	s.bounds = image.Rectangle{Max: size}
	job, err := startFFmpeg(s.info.path, s.args())
	if err != nil {
		return err
	}
	s.job = job
	s.mkv = mkvWriter{w: job.stdin}
	if err := s.mkv.writeHeader(size.X, size.Y); err != nil {
		return job.end(err)
	}
	return nil
}

// args is the full ffmpeg command line after the binary.
func (s *VideoEncoder) args() []string {
	args := []string{
		"-hide_banner", "-nostdin", "-loglevel", "error",
		"-f", "matroska", "-i", "pipe:0",
		"-map", "0:v:0", "-an", "-sn", "-dn",
	}
	args = append(args, s.info.passthroughArgs()...)
	// Encode in the stream's millisecond time base rather than one derived
	// from a guessed frame rate.
	args = append(args, "-enc_time_base", "-1")
	if !s.profile.alpha {
		pixFmt := "yuv420p"
		filter := fmt.Sprintf(
			"pad=ceil(iw/2)*2:ceil(ih/2)*2:color=0x%02x%02x%02x,",
			s.background.R, s.background.G, s.background.B,
		)
		if s.opts.fullChroma {
			pixFmt, filter = "yuv444p", ""
		}
		// BT.709 is what players assume for untagged and HD video, so convert
		// with it and say so; swscale's default (BT.601) shifts colors.
		args = append(args,
			"-vf", filter+"scale=out_color_matrix=bt709:out_range=tv,format="+pixFmt,
			"-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709", "-color_range", "tv",
		)
	}
	args = append(args, "-c:v", s.encoder)
	args = append(args, s.profile.args(s.opts)...)
	return append(args, "-f", s.profile.muxer, "-y", s.file.f.Name())
}

// streamable returns img as tightly packed NRGBA on the canvas, composited
// onto the background when the format has no alpha or [WithBackground] is
// set. Opaque NRGBA frames of the canvas size, the common case for the render
// package's renderer, are returned as is.
func (s *VideoEncoder) streamable(img image.Image) *image.NRGBA {
	composite := !s.profile.alpha || s.opts.backgroundSet
	if src, ok := img.(*image.NRGBA); ok && src.Rect.Size() == s.bounds.Size() && (!composite || src.Opaque()) {
		return src
	}
	src := asNRGBA(img)
	if s.canvas == nil {
		s.canvas = image.NewNRGBA(s.bounds)
	}
	dst := s.canvas
	w := min(dst.Rect.Dx(), src.Rect.Dx())
	h := min(dst.Rect.Dy(), src.Rect.Dy())
	if w < dst.Rect.Dx() || h < dst.Rect.Dy() {
		// Only a smaller frame leaves part of the canvas uncovered.
		fill := color.NRGBA{}
		if composite {
			fill = s.background
		}
		fillNRGBA(dst, fill)
	}
	for y := range h {
		si := src.PixOffset(src.Rect.Min.X, src.Rect.Min.Y+y)
		srcRow := src.Pix[si : si+w*4]
		dstRow := dst.Pix[y*dst.Stride : y*dst.Stride+w*4]
		if !composite {
			copy(dstRow, srcRow)
			continue
		}
		compositeRow(dstRow, srcRow, s.background)
	}
	return dst
}

// fillNRGBA sets every pixel of the tightly packed img to c. draw.Draw has no
// fast path for an NRGBA destination.
func fillNRGBA(img *image.NRGBA, c color.NRGBA) {
	row := img.Pix[:img.Rect.Dx()*4]
	for i := 0; i < len(row); i += 4 {
		row[i], row[i+1], row[i+2], row[i+3] = c.R, c.G, c.B, c.A
	}
	for y := 1; y < img.Rect.Dy(); y++ {
		copy(img.Pix[y*img.Stride:], row)
	}
}

// compositeRow blends NRGBA src over the opaque bg into dst. Runs of opaque
// pixels, most of a terminal frame, are copied in one go.
func compositeRow(dst, src []byte, bg color.NRGBA) {
	dst = dst[:len(src)]
	for i := 0; i+3 < len(src); i += 4 {
		a := uint32(src[i+3])
		switch a {
		case 0xff:
			j := i + 4
			for j+3 < len(src) && src[j+3] == 0xff {
				j += 4
			}
			copy(dst[i:j], src[i:j])
			i = j - 4
			continue
		case 0:
			dst[i], dst[i+1], dst[i+2] = bg.R, bg.G, bg.B
		default:
			inv := 0xff - a
			dst[i] = uint8((uint32(src[i])*a + uint32(bg.R)*inv + 0x7f) / 0xff)     //nolint:gosec // <= 0xff.
			dst[i+1] = uint8((uint32(src[i+1])*a + uint32(bg.G)*inv + 0x7f) / 0xff) //nolint:gosec // <= 0xff.
			dst[i+2] = uint8((uint32(src[i+2])*a + uint32(bg.B)*inv + 0x7f) / 0xff) //nolint:gosec // <= 0xff.
		}
		dst[i+3] = 0xff
	}
}

// Close finishes the encode and moves it to the destination path. With no
// frames written, it writes nothing and returns nil. If the encode failed, the
// destination is left alone and the error includes ffmpeg's output. Later
// calls return the same result.
func (s *VideoEncoder) Close() error {
	if s.done {
		return s.err
	}
	s.done = true
	if s.job != nil {
		if err := s.job.end(nil); err != nil && s.err == nil {
			s.err = err
		}
	}
	if s.err != nil || s.frames == 0 {
		_ = s.file.abort()
		return s.err
	}
	if s.format == VideoWebP {
		total := time.Duration(s.mkv.pts) * mkvTick //nolint:gosec // milliseconds of recording.
		if err := fixWebP(s.file.f.Name(), s.opts.loopCount, total); err != nil {
			_ = s.file.abort()
			s.err = fmt.Errorf("sink: finishing %s: %w", s.format, err)
			return s.err
		}
	}
	if err := s.file.commit(); err != nil {
		s.err = fmt.Errorf("sink: moving %s into place: %w", s.format, err)
	}
	return s.err
}

// Discard stops ffmpeg and removes its output without touching the
// destination path. It returns nil after [VideoEncoder.Close] has run.
func (s *VideoEncoder) Discard() error {
	if s.done {
		return nil
	}
	s.done = true
	if s.job != nil {
		s.job.kill()
	}
	return s.file.abort()
}
