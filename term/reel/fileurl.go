// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"encoding/hex"
	"errors"
	"fmt"
	"image/color"
	"maps"
	"math"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/lrstanley/x/term/reel/sink"
)

// fileParam is a query parameter of [ParseFile].
type fileParam struct {
	name string
	// formats are the output formats it applies to (see [formatOf]).
	formats []sink.Format
	repeat  bool
	apply   func(f *File, values []string) error
}

// fileParams are the query parameters of [ParseFile], in the order their
// options are added.
var fileParams = []fileParam{
	{name: "version", formats: []sink.Format{formatCast}, apply: func(f *File, v []string) error {
		switch v[0] {
		case "2":
			f.Cast = CastV2
		case "3":
			f.Cast = CastV3
		default:
			return fmt.Errorf("%q is not 2 or 3", v[0])
		}
		return nil
	}},
	{name: "marks", formats: stillFormats, repeat: true, apply: func(f *File, v []string) error {
		if slices.Contains(v, "") {
			return errors.New("a mark label is empty")
		}
		f.Marks = v
		return nil
	}},
	{name: "match", formats: stillFormats, repeat: true, apply: func(f *File, v []string) error {
		for _, pattern := range v {
			if _, err := path.Match(pattern, ""); err != nil || pattern == "" {
				return fmt.Errorf("%q is not a pattern", pattern)
			}
		}
		f.MarkMatch = v
		return nil
	}},
	{name: "optimize", formats: []sink.Format{sink.FormatGIF, sink.FormatPNG}, repeat: true, apply: func(f *File, v []string) error {
		flags, err := parseOptimize(v)
		if err == nil {
			f.Options = append(f.Options, sink.WithOptimize(flags))
		}
		return err
	}},
	{name: "max-frames", formats: []sink.Format{sink.FormatGIF}, apply: func(f *File, v []string) error {
		n, err := strconv.Atoi(v[0])
		if err != nil || n < 0 {
			return fmt.Errorf("%q is not a frame count", v[0])
		}
		f.Options = append(f.Options, sink.WithMaxFrames(n))
		return nil
	}},
	{name: "max-bytes", formats: []sink.Format{sink.FormatGIF}, apply: func(f *File, v []string) error {
		n, err := parseBytes(v[0])
		if err == nil {
			f.Options = append(f.Options, sink.WithMaxBytes(n))
		}
		return err
	}},
	{name: "loop", formats: []sink.Format{sink.FormatGIF, sink.FormatWebP}, apply: func(f *File, v []string) error {
		var n int
		switch v[0] {
		case "forever":
			n = sink.LoopForever
		case "once":
			n = sink.LoopOnce
		default:
			var err error
			n, err = strconv.Atoi(v[0])
			if err != nil || n < 1 || n > math.MaxUint16 {
				return fmt.Errorf("%q is not forever, once, or a repeat count from 1 to %d", v[0], math.MaxUint16)
			}
		}
		f.Options = append(f.Options, sink.WithLoopCount(n))
		return nil
	}},
	{name: "quality", formats: []sink.Format{sink.FormatMP4, sink.FormatWebM, sink.FormatWebP}, apply: func(f *File, v []string) error {
		q, err := strconv.Atoi(v[0])
		if err != nil || q < 0 || q > sink.MaxQuality {
			return fmt.Errorf("%q is not from 0 to %d", v[0], sink.MaxQuality)
		}
		f.Options = append(f.Options, sink.WithQuality(q))
		return nil
	}},
	{name: "full-chroma", formats: []sink.Format{sink.FormatMP4, sink.FormatWebM}, apply: func(f *File, v []string) error {
		on, err := strconv.ParseBool(v[0])
		if err != nil {
			return fmt.Errorf("%q is not a boolean", v[0])
		}
		f.Options = append(f.Options, sink.WithFullChroma(on))
		return nil
	}},
	{name: "background", formats: []sink.Format{sink.FormatGIF, sink.FormatMP4, sink.FormatWebM, sink.FormatWebP}, apply: func(f *File, v []string) error {
		c, err := parseHexColor(v[0])
		if err == nil {
			f.Options = append(f.Options, sink.WithBackground(c))
		}
		return err
	}},
}

// stillFormats are the formats that can be stills at marks.
var stillFormats = []sink.Format{
	sink.FormatGIF, sink.FormatPNG, sink.FormatMP4, sink.FormatWebM, sink.FormatWebP, formatSnap, formatJSON,
}

// schemeRe matches a URL scheme. One letter is a Windows drive, not a
// scheme.
var schemeRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]+)://`)

// fileDriveRe matches the slash before a Windows drive in a file URL, as in
// "file:///C:/out.gif".
var fileDriveRe = regexp.MustCompile(`^/[A-Za-z]:/`)

// ParseFile parses an output written as a path with optional query
// parameters for the file's options, such as
// "out/{name}.gif?max-bytes=5MB&loop=once". Everything before the first "?"
// is the path, used as is. Path variables (see [Export]) are left in it to
// be expanded on export; query values are literal.
//
// The output may also be written as a "file://" URL, such as
// "file://./demo.cast?version=3", whose path is everything after the
// scheme, percent-decoded ("file:///C:/x.gif" is "C:/x.gif"). Other schemes
// are errors.
//
// Each parameter applies only to some formats, and is an error on others,
// as is an unknown or repeated one ("marks" and "optimize" repeat):
//
//   - version=2|3: [File.Cast] (".cast").
//   - marks=label: adds to [File.Marks]; the path must hold {mark}. Not
//     for ".cast".
//   - match=pattern: adds to [File.MarkMatch], such as "input-*"; the path
//     must hold {mark}. Not for ".cast".
//   - optimize=none|all|frames|dirty|quantize: [sink.WithOptimize], the
//     flags OR'd together (".gif", ".png").
//   - max-frames=n: [sink.WithMaxFrames] (".gif").
//   - max-bytes=size: [sink.WithMaxBytes], a byte count with an optional
//     unit: B, KB, MB, GB (powers of 1000) or KiB, MiB, GiB (powers of
//     1024), such as "5MB" or "1.5MiB" (".gif").
//   - loop=forever|once|n: [sink.WithLoopCount], n being the number of
//     repeats, from 1 (".gif", ".webp").
//   - quality=0..100: [sink.WithQuality] (".mp4", ".webm", ".webp").
//   - full-chroma=bool: [sink.WithFullChroma] (".mp4", ".webm").
//   - background=#rgb|#rrggbb: [sink.WithBackground] (".gif", ".mp4",
//     ".webm", ".webp").
//
// Profile-wide settings, such as [Profile.FPS] and [Profile.Speed], are not
// parameters, since every output of a profile shares them.
//
// For outputs written in code, see [MustFiles].
func ParseFile(raw string) (File, error) {
	var f File
	name, query, _ := strings.Cut(raw, "?")
	name, err := FilePath(name)
	if err != nil {
		return f, err
	}
	f.Path = name

	format, err := formatOf(name)
	if err != nil {
		return f, fmt.Errorf("reel: output %q: %w", raw, err)
	}

	values, err := url.ParseQuery(query)
	if err != nil {
		return f, fmt.Errorf("reel: output %q: %w", raw, err)
	}
	for _, name := range slices.Sorted(maps.Keys(values)) {
		if !slices.ContainsFunc(fileParams, func(p fileParam) bool { return p.name == name }) {
			return f, fmt.Errorf("reel: output %q: unknown parameter %q", raw, name)
		}
	}
	for _, p := range fileParams {
		v, ok := values[p.name]
		if !ok {
			continue
		}
		if !slices.Contains(p.formats, format) {
			return f, fmt.Errorf("reel: output %q: parameter %q does not apply to %s files", raw, p.name, filepath.Ext(name))
		}
		if len(v) > 1 && !p.repeat {
			return f, fmt.Errorf("reel: output %q: parameter %q is repeated", raw, p.name)
		}
		if err = p.apply(&f, v); err != nil {
			return f, fmt.Errorf("reel: output %q: %s: %w", raw, p.name, err)
		}
	}
	return f, f.checkMarkPath()
}

// ParseFiles parses a comma-separated list of outputs, each as [ParseFile]
// does, skipping empty items and the spaces around each, such as
// "{name}.gif, {name}_{mark}.snap?match=input-*". An output with a comma in
// its path cannot be written this way.
func ParseFiles(raw string) ([]File, error) {
	var files []File
	for part := range strings.SplitSeq(raw, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		f, err := ParseFile(part)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// FilePath returns the path of an output written as a path or "file://" URL
// (see [ParseFile]), with no query.
func FilePath(raw string) (string, error) {
	name := raw
	if m := schemeRe.FindStringSubmatch(name); m != nil {
		if !strings.EqualFold(m[1], "file") {
			return "", fmt.Errorf("reel: path %q: unsupported scheme %q", raw, m[1])
		}
		var err error
		if name, err = url.PathUnescape(name[len(m[0]):]); err != nil {
			return "", fmt.Errorf("reel: path %q: %w", raw, err)
		}
		if fileDriveRe.MatchString(name) {
			name = name[1:]
		}
	}
	if name == "" {
		return "", fmt.Errorf("reel: path %q is empty", raw)
	}
	return name, nil
}

// parseOptimize ORs together optimize parameter values.
func parseOptimize(values []string) (sink.OptimizeFlags, error) {
	var flags sink.OptimizeFlags
	for _, v := range values {
		switch v {
		case "none":
			if len(values) > 1 {
				return 0, errors.New("none cannot be combined with other flags")
			}
			return sink.OptimizeNone, nil
		case "all":
			flags |= sink.OptimizeAll
		case "frames":
			flags |= sink.OptimizeFrames
		case "dirty":
			flags |= sink.OptimizeDirtyRects
		case "quantize":
			flags |= sink.OptimizeColorQuantization
		default:
			return 0, fmt.Errorf("%q is not none, all, frames, dirty, or quantize", v)
		}
	}
	return flags, nil
}

var byteUnits = map[string]float64{
	"": 1, "b": 1,
	"kb": 1e3, "mb": 1e6, "gb": 1e9,
	"kib": 1 << 10, "mib": 1 << 20, "gib": 1 << 30,
}

// parseBytes parses a byte count with an optional unit, such as "5MB".
func parseBytes(s string) (int64, error) {
	i := strings.IndexFunc(s, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if i < 0 {
		i = len(s)
	}
	n, err := strconv.ParseFloat(s[:i], 64)
	unit, ok := byteUnits[strings.ToLower(strings.TrimSpace(s[i:]))]
	n = math.Round(n * unit)
	// float64(math.MaxInt64) is 2^63, one past the largest int64. A size
	// rounding to 0 would be no limit.
	if err != nil || !ok || n >= math.MaxInt64 || n == 0 && strings.Trim(s[:i], "0.") != "" {
		return 0, fmt.Errorf("%q is not a size such as 5MB", s)
	}
	return int64(n), nil
}

// parseHexColor parses #rgb or #rrggbb.
func parseHexColor(s string) (color.Color, error) {
	digits, ok := strings.CutPrefix(s, "#")
	if ok && len(digits) == 3 {
		digits = string([]byte{digits[0], digits[0], digits[1], digits[1], digits[2], digits[2]})
	}
	rgb, err := hex.DecodeString(digits)
	if !ok || err != nil || len(rgb) != 3 {
		return nil, fmt.Errorf("%q is not #rgb or #rrggbb", s)
	}
	return color.NRGBA{R: rgb[0], G: rgb[1], B: rgb[2], A: 0xff}, nil
}
