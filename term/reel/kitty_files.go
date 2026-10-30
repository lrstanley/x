// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/lrstanley/x/term/reel/internal/kittycmd"
)

// WithKittyFiles lets the program send Kitty graphics as a file, a temporary
// file, or (on Linux) a shared memory object, rather than in the output. The
// session reads each one as it arrives, deletes the temporary ones as a
// terminal must, and passes it on as an image sent in the output, so the
// timeline holds the image itself and replays show it anywhere.
//
// It is off by default, as the program could otherwise have reel read any
// file the user can, into a recording that may be shared. Even on, only
// regular files outside /proc, /sys, and /dev are read, temporary files must
// be in the temporary directory and have "tty-graphics-protocol" in their
// path, as in kitty, and what is read must be an image of the size the
// program says. Anything else is passed on as sent, which the terminal
// refuses, as it does without the option.
func WithKittyFiles() SessionOption {
	return func(cfg *sessionConfig) { cfg.kittyFiles = true }
}

// maxFileCommand is the longest command held to see whether it sends an
// image by file. A file command's payload is a path, and its control keys
// come first, so anything longer is passed on as sent.
const maxFileCommand = 16 << 10

var (
	apcStart = []byte("\x1b_G")
	apcEnd   = []byte("\x1b\\")
)

// kittyFiles rewrites Kitty graphics commands that send an image by file
// into commands that send it in the output. Commands may be split across
// writes, so the start of one is held until it is known to send a file,
// and then until it ends.
type kittyFiles struct {
	// limit is the most bytes one image may read.
	limit int
	// held is the start of a command, or of what may be one, that the last
	// write ended in.
	held []byte
	// passing is set inside a command that does not send a file, which is
	// passed on as it arrives.
	passing bool
}

func newKittyFiles(limit uint64) *kittyFiles {
	return &kittyFiles{limit: int(min(limit, 1<<30))} //nolint:gosec // capped to fit an int.
}

// rewrite returns p, after any held bytes, as it should reach the terminal:
// file transmissions rewritten, and the start of a command that may be one
// held back for the next call. Output with no command in it is returned as
// it is.
func (k *kittyFiles) rewrite(p []byte) []byte {
	data := p
	if len(k.held) > 0 {
		data = slices.Concat(k.held, p)
		k.held = nil
	}
	var out []byte
	for len(data) > 0 {
		if k.passing {
			end := bytes.Index(data, apcEnd)
			if end < 0 {
				return append(out, data...)
			}
			k.passing = false
			out, data = append(out, data[:end+len(apcEnd)]...), data[end+len(apcEnd):]
			continue
		}
		start := bytes.Index(data, apcStart)
		if start < 0 {
			// Hold a trailing ESC or ESC _, which may start a command.
			keep := partialPrefix(data, apcStart)
			k.held = bytes.Clone(data[len(data)-keep:])
			if out == nil {
				return data[:len(data)-keep]
			}
			return append(out, data[:len(data)-keep]...)
		}
		out, data = append(out, data[:start]...), data[start:]
		end := bytes.Index(data, apcEnd)
		file, known := sendsFile(data, end)
		switch {
		case known && !file, end < 0 && len(data) > maxFileCommand:
			k.passing = true
		case end < 0:
			k.held = bytes.Clone(data)
			return out
		default:
			out, data = append(out, k.command(data[:end+len(apcEnd)])...), data[end+len(apcEnd):]
		}
	}
	return out
}

// flush returns what is held, as sent.
func (k *kittyFiles) flush() []byte {
	held := k.held
	k.held = nil
	return held
}

// partialPrefix is the length of the longest end of data that starts
// prefix.
func partialPrefix(data, prefix []byte) int {
	for n := min(len(prefix)-1, len(data)); n > 0; n-- {
		if bytes.HasSuffix(data, prefix[:n]) {
			return n
		}
	}
	return 0
}

// sendsFile reports whether the command data starts with, which ends at end
// or has not ended when end is negative, sends an image by file. known is
// false while its control keys have not all arrived.
func sendsFile(data []byte, end int) (file, known bool) {
	ctrl := data[len(apcStart):]
	if end >= 0 {
		ctrl = data[len(apcStart):end]
	}
	ctrl, _, cut := bytes.Cut(ctrl, []byte(";"))
	if !cut && end < 0 {
		return false, false
	}
	switch parseKittyKeys(string(ctrl)).medium {
	case "f", "t", "s":
		return true, true
	}
	return false, true
}

// kittyKeys are the control keys of a command, as the rewrite needs them.
type kittyKeys struct {
	// medium, offset, and size are t, O, and S.
	medium       string
	offset, size int
	// format, width, height, and zlib are f, s, v, and o=z.
	format, width, height int
	zlib                  bool
	// kept are the keys to send on, all but t, O, and S.
	kept []string
}

func parseKittyKeys(ctrl string) kittyKeys {
	k := kittyKeys{format: 32}
	for kv := range strings.SplitSeq(ctrl, ",") {
		key, v, _ := strings.Cut(kv, "=")
		n, _ := strconv.Atoi(v)
		switch key {
		case "t":
			k.medium = v
			continue
		case "O":
			k.offset = n
			continue
		case "S":
			k.size = n
			continue
		case "f":
			k.format = n
		case "s":
			k.width = n
		case "v":
			k.height = n
		case "o":
			k.zlib = v == "z"
		}
		k.kept = append(k.kept, kv)
	}
	return k
}

// command returns cmd, a whole command from ESC _ G to ESC \, as it should
// reach the terminal: rewritten to send its image in the output if it sent
// one by file, and as it was otherwise.
func (k *kittyFiles) command(cmd []byte) []byte {
	ctrl, payload, _ := bytes.Cut(cmd[len(apcStart):len(cmd)-len(apcEnd)], []byte(";"))
	keys := parseKittyKeys(string(ctrl))
	name, err := base64.StdEncoding.DecodeString(string(payload))
	if err != nil {
		return cmd
	}
	data, err := k.read(keys.medium, string(name), keys.offset, keys.size)
	if err != nil || checkKittyImage(keys, data) != nil {
		return cmd
	}
	var b bytes.Buffer
	kittycmd.AppendTransmit(&b, keys.kept, data)
	return b.Bytes()
}

// read returns the bytes of the file, temporary file, or shared memory
// object name, from offset, size of them if size is not zero, and removes
// the ones a terminal must.
func (k *kittyFiles) read(medium, name string, offset, size int) ([]byte, error) {
	var path string
	switch medium {
	case "f":
		path = name
	case "t":
		if !strings.Contains(name, "tty-graphics-protocol") || !inDir(name, os.TempDir()) {
			return nil, errors.New("temporary file outside the temporary directory")
		}
		path = name
	case "s":
		if runtime.GOOS != "linux" || !strings.HasPrefix(name, "/") || strings.Contains(name[1:], "/") || len(name) < 2 {
			return nil, errors.New("shared memory is not supported here")
		}
		path = "/dev/shm" + name
	}
	if medium != "s" {
		for _, dir := range []string{"/proc", "/sys", "/dev"} {
			if inDir(path, dir) {
				return nil, fmt.Errorf("%s is under %s", path, dir)
			}
		}
	}
	if medium != "f" {
		defer os.Remove(path)
	}
	f, err := os.Open(path) //nolint:gosec // G703: the program names the file, which WithKittyFiles allows within the rules above.
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, serr := f.Stat(); serr != nil || !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if offset < 0 || size < 0 {
		return nil, errors.New("negative offset or size")
	}
	n := k.limit + 1
	if size > 0 {
		n = min(size, n)
	}
	data, err := io.ReadAll(io.LimitReader(io.NewSectionReader(f, int64(offset), 1<<62), int64(n)))
	if err != nil {
		return nil, err
	}
	if len(data) > k.limit {
		return nil, errors.New("image too large")
	}
	return data, nil
}

// inDir reports whether path, made absolute and resolved, is inside dir.
func inDir(path, dir string) bool {
	if p, err := filepath.EvalSymlinks(path); err == nil {
		path = p
	}
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// checkKittyImage reports an error unless data is the image keys describe:
// a PNG for f=100, or, for raw pixels, s by v pixels of the format, once
// inflated for o=z.
func checkKittyImage(keys kittyKeys, data []byte) error {
	var r io.Reader = bytes.NewReader(data)
	if keys.zlib {
		zr, err := zlib.NewReader(r)
		if err != nil {
			return err
		}
		r = zr
	}
	var bpp int
	switch keys.format {
	case 100:
		_, err := png.DecodeConfig(r)
		return err
	case 24:
		bpp = 3
	case 32:
		bpp = 4
	}
	if bpp == 0 || keys.width <= 0 || keys.height <= 0 {
		return errors.New("unknown image format or size")
	}
	want := int64(keys.width) * int64(keys.height) * int64(bpp)
	n, err := io.Copy(io.Discard, io.LimitReader(r, want+1))
	if err != nil {
		return err
	}
	if n != want {
		return fmt.Errorf("%d bytes of pixels, want %d", n, want)
	}
	return nil
}
