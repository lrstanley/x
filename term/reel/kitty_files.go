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
	"strconv"
	"strings"

	"github.com/lrstanley/x/term/reel/vt"
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

// kittyFileChunk is how many base64 bytes one chunk of a rewritten
// transmission carries.
const kittyFileChunk = 4096

// kittyFiles rewrites Kitty graphics commands that send an image by file
// into commands that send it in the output. Commands may be split across
// writes, so the start of an unfinished one is held until it ends.
type kittyFiles struct {
	// limit is the most bytes one image may read.
	limit int
	// held is the start of a command, or of what may be one, that the last
	// write ended in.
	held []byte
	// skip is set while passing on the rest of a command too long to hold.
	skip bool
}

func newKittyFiles(limit uint64) *kittyFiles {
	if limit == 0 {
		limit = vt.DefaultKittyStorage
	}
	return &kittyFiles{limit: int(min(limit, 1<<30))} //nolint:gosec // capped to fit an int.
}

// rewrite returns p, with any held bytes before it, as it should reach the
// terminal: file transmissions rewritten, and the start of an unfinished
// command held back for the next call.
func (k *kittyFiles) rewrite(p []byte) []byte {
	data := p
	if len(k.held) > 0 {
		k.held = append(k.held, p...)
		data, k.held = k.held, nil
	}
	var out []byte
	for len(data) > 0 {
		if k.skip {
			end := bytes.Index(data, []byte("\x1b\\"))
			if end < 0 {
				return append(out, data...)
			}
			k.skip = false
			out, data = append(out, data[:end+2]...), data[end+2:]
			continue
		}
		start := bytes.Index(data, []byte("\x1b_G"))
		if start < 0 {
			// Hold a trailing ESC or ESC _, which may start a command.
			keep := partialPrefix(data, "\x1b_G")
			k.held = append(k.held, data[len(data)-keep:]...)
			return append(out, data[:len(data)-keep]...)
		}
		out, data = append(out, data[:start]...), data[start:]
		end := bytes.Index(data, []byte("\x1b\\"))
		if end < 0 {
			if len(data) > k.limit*2 {
				// Too long for any image we would read: not a file.
				k.skip = true
				return append(out, data...)
			}
			k.held = append(k.held, data...)
			return out
		}
		out = append(out, k.command(data[:end+2])...)
		data = data[end+2:]
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
func partialPrefix(data []byte, prefix string) int {
	for n := min(len(prefix)-1, len(data)); n > 0; n-- {
		if string(data[len(data)-n:]) == prefix[:n] {
			return n
		}
	}
	return 0
}

// command returns cmd, a whole command from ESC _ G to ESC \, as it should
// reach the terminal: rewritten to send its image in the output if it sent
// one by file, and as it was otherwise.
func (k *kittyFiles) command(cmd []byte) []byte {
	body := cmd[3 : len(cmd)-2]
	ctrl, payload, _ := bytes.Cut(body, []byte(";"))
	keys := strings.Split(string(ctrl), ",")
	medium, offset, size := "", 0, 0
	kept := keys[:0:0]
	for _, kv := range keys {
		key, v, _ := strings.Cut(kv, "=")
		switch key {
		case "t":
			medium = v
		case "O":
			offset, _ = strconv.Atoi(v)
		case "S":
			size, _ = strconv.Atoi(v)
		default:
			kept = append(kept, kv)
		}
	}
	if medium != "f" && medium != "t" && medium != "s" {
		return cmd
	}
	name, err := base64.StdEncoding.DecodeString(string(payload))
	if err != nil {
		return cmd
	}
	data, err := k.read(medium, string(name), offset, size)
	if err != nil || checkKittyImage(kept, data) != nil {
		return cmd
	}
	return directKittyCommand(kept, data)
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
func checkKittyImage(keys []string, data []byte) error {
	format, w, h, zlibbed := 32, 0, 0, false
	for _, kv := range keys {
		key, v, _ := strings.Cut(kv, "=")
		n, _ := strconv.Atoi(v)
		switch key {
		case "f":
			format = n
		case "s":
			w = n
		case "v":
			h = n
		case "o":
			zlibbed = v == "z"
		}
	}
	var r io.Reader = bytes.NewReader(data)
	if zlibbed {
		zr, err := zlib.NewReader(r)
		if err != nil {
			return err
		}
		r = zr
	}
	if format == 100 {
		_, err := png.DecodeConfig(r)
		return err
	}
	bpp := map[int]int{24: 3, 32: 4}[format]
	if bpp == 0 || w <= 0 || h <= 0 {
		return errors.New("unknown image format or size")
	}
	want := int64(w) * int64(h) * int64(bpp)
	n, err := io.Copy(io.Discard, io.LimitReader(r, want+1))
	if err != nil {
		return err
	}
	if n != want {
		return fmt.Errorf("%d bytes of pixels, want %d", n, want)
	}
	return nil
}

// directKittyCommand returns the command with control keys keys that sends
// data in the output, in chunks.
func directKittyCommand(keys []string, data []byte) []byte {
	enc := base64.StdEncoding.EncodeToString(data)
	var b bytes.Buffer
	for first := true; first || enc != ""; first = false {
		chunk := enc[:min(len(enc), kittyFileChunk)]
		enc = enc[len(chunk):]
		more := "m=0"
		if enc != "" {
			more = "m=1"
		}
		ctrl := more
		if first {
			ctrl = strings.Join(append(keys, more), ",")
		}
		fmt.Fprintf(&b, "\x1b_G%s;%s\x1b\\", ctrl, chunk)
	}
	return b.Bytes()
}
