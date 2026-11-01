// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/cmd/reel/internal/look"
	"github.com/lrstanley/x/term/reel/effects"
	"github.com/lrstanley/x/term/reel/sink"
)

// readCast reads the asciicast file at path, or standard input for "-".
func readCast(path string) (*reel.Timeline, error) {
	if path == "-" {
		return reel.ReadTimeline(bufio.NewReader(os.Stdin))
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tl, err := reel.ReadTimeline(bufio.NewReader(f))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return tl, nil
}

// writeCast writes tl to path as asciicast version v, or to standard output
// for "-".
func writeCast(tl *reel.Timeline, path string, v reel.CastVersion) error {
	if path == "-" {
		_, err := tl.WriteCast(os.Stdout, v)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = tl.WriteCast(f, v)
	return errors.Join(err, f.Close())
}

// WindowFlags pick a window of a timeline between two marks.
type WindowFlags struct {
	Between string `name:"between" placeholder:"FROM,TO" help:"Only the window from the first mark FROM to the mark TO after it; either may be empty for the start or end."`
}

// window returns the part of tl that the flags pick.
func (f WindowFlags) window(tl *reel.Timeline) (*reel.Timeline, error) {
	if f.Between == "" {
		return tl, nil
	}
	from, to, ok := strings.Cut(f.Between, ",")
	if !ok {
		return nil, fmt.Errorf("--between %q is not FROM,TO", f.Between)
	}
	return tl.Between(from, to)
}

// ExportCmd renders an asciicast file.
type ExportCmd struct {
	WindowFlags `embed:""`
	RenderFlags `embed:""`

	Cast          string `arg:"" placeholder:"CAST" help:"Asciicast file (v2 or v3), or - for standard input."`
	NoCastEffects bool   `name:"no-cast-effects" help:"Ignore the effects the asciicast file records."`
}

// Run exports the file.
func (c *ExportCmd) Run(ctx context.Context) error {
	tl, err := readCast(c.Cast)
	if err != nil {
		return err
	}
	var base []reel.Effect
	if !c.NoCastEffects {
		if base, err = effects.FromTimeline(tl); err != nil {
			return fmt.Errorf("%s: %w (pass --no-cast-effects to ignore them)", c.Cast, err)
		}
	}
	if tl, err = c.window(tl); err != nil {
		return err
	}
	p, err := c.profile(base...)
	if err != nil {
		return err
	}
	return reel.Export(ctx, tl, p)
}

// StillCmd renders one moment of an asciicast file.
type StillCmd struct {
	look.Look `embed:""`

	Cast   string         `arg:"" placeholder:"CAST" help:"Asciicast file (v2 or v3), or - for standard input."`
	At     *time.Duration `name:"at" xor:"when" placeholder:"DUR" help:"The moment, from the start, such as 3.2s; defaults to the end."`
	Mark   string         `name:"mark" xor:"when" placeholder:"LABEL" help:"The moment of the first mark labeled LABEL."`
	Output string         `short:"o" name:"output" required:"" placeholder:"FILE" help:"File to write: .png for an image, .snap for the screen's text, or .json for the screen with its cells, styles, and cursor."`
}

// Run renders the still. Effects are not drawn on it.
func (c *StillCmd) Run(ctx context.Context) error {
	tl, err := readCast(c.Cast)
	if err != nil {
		return err
	}
	return c.render(ctx, tl)
}

// render renders the still of tl.
func (c *StillCmd) render(ctx context.Context, tl *reel.Timeline) error {
	n, err := c.events(tl)
	if err != nil {
		return err
	}
	err = c.Validate()
	if err != nil {
		return err
	}
	var fontSize float64
	if c.FontSize != nil {
		fontSize = *c.FontSize
	}
	opts, warnings, err := c.Render(fontSize)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		warnf("%s", w)
	}
	p, err := reel.NewPlayer(ctx, tl, opts...)
	if err != nil {
		return err
	}
	defer p.Close()
	err = p.Seek(n)
	if err != nil {
		return err
	}
	return writeStill(p, c.Output)
}

// events returns how many of tl's events lead up to the moment.
func (c *StillCmd) events(tl *reel.Timeline) (int, error) {
	i := 0
	for ev := range tl.Events() {
		switch {
		case c.At != nil && ev.At() > *c.At:
			return i, nil
		case c.Mark != "":
			if m, ok := ev.(reel.Mark); ok && m.Label == c.Mark {
				return i + 1, nil
			}
		}
		i++
	}
	if c.Mark != "" {
		return 0, &reel.NoMarkError{Label: c.Mark}
	}
	return i, nil
}

// writeStill writes the player's screen to path, in the format its
// extension names.
func writeStill(p *reel.Player, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".png":
		img, err := p.Image()
		if err != nil {
			return err
		}
		return sink.PNG(img, path)
	case ".snap":
		return os.WriteFile(path, []byte(p.Frame().Screen.Text()), 0o600)
	case ".json":
		data, err := json.Marshal(p.Screen(), jsontext.Multiline(true))
		if err != nil {
			return err
		}
		return os.WriteFile(path, append(data, '\n'), 0o600)
	default:
		return fmt.Errorf("still %q: want a .png, .snap, or .json file", path)
	}
}

// CutCmd cuts an asciicast file to a window.
type CutCmd struct {
	Cast    string `arg:"" placeholder:"CAST" help:"Asciicast file (v2 or v3), or - for standard input."`
	From    string `name:"from" placeholder:"MARK" help:"Start at the first mark labeled MARK; defaults to the start."`
	To      string `name:"to" placeholder:"MARK" help:"End at the first mark labeled MARK after the start; defaults to the end."`
	Output  string `short:"o" name:"output" required:"" placeholder:"FILE" help:"Asciicast file to write, or - for standard output. It opens on a redraw of the screen at the start, so it plays correctly anywhere."`
	Version int    `name:"cast-version" default:"2" enum:"2,3" help:"Asciicast version to write."`
}

// Run cuts the file.
func (c *CutCmd) Run() error {
	if c.From == "" && c.To == "" {
		return errors.New("give --from, --to, or both")
	}
	tl, err := readCast(c.Cast)
	if err != nil {
		return err
	}
	if tl, err = tl.Between(c.From, c.To); err != nil {
		return err
	}
	return writeCast(tl, c.Output, reel.CastVersion(c.Version))
}

// ConvertCmd converts an asciicast file between versions.
type ConvertCmd struct {
	Cast    string `arg:"" placeholder:"CAST" help:"Asciicast file (v2 or v3), or - for standard input."`
	Output  string `short:"o" name:"output" required:"" placeholder:"FILE" help:"Asciicast file to write, or - for standard output."`
	Version int    `name:"cast-version" default:"3" enum:"2,3" help:"Asciicast version to write."`
}

// Run converts the file.
func (c *ConvertCmd) Run() error {
	tl, err := readCast(c.Cast)
	if err != nil {
		return err
	}
	return writeCast(tl, c.Output, reel.CastVersion(c.Version))
}

// InfoCmd describes an asciicast file.
type InfoCmd struct {
	Cast string `arg:"" placeholder:"CAST" help:"Asciicast file (v2 or v3), or - for standard input."`
	JSON bool   `name:"json" help:"Print JSON."`
}

// castInfo is what InfoCmd prints.
type castInfo struct {
	Title       string         `json:"title,omitempty"`
	Cols        int            `json:"cols"`
	Rows        int            `json:"rows"`
	Duration    string         `json:"duration"`
	Events      map[string]int `json:"events"`
	Resizes     []string       `json:"resizes,omitempty"`
	Exit        *int           `json:"exit,omitempty"`
	Marks       []castMark     `json:"marks,omitempty"`
	Annotations map[string]int `json:"annotations,omitempty"`
	Effects     []string       `json:"effects,omitempty"`
}

type castMark struct {
	Label string `json:"label"`
	At    string `json:"at"`
}

// Run prints the description.
func (c *InfoCmd) Run() error {
	tl, err := readCast(c.Cast)
	if err != nil {
		return err
	}
	h := tl.Header()
	info := castInfo{Title: h.Title, Cols: h.Cols, Rows: h.Rows, Duration: tl.Duration().String(), Events: map[string]int{}, Annotations: map[string]int{}}
	for ev := range tl.Events() {
		kind := strings.ToLower(strings.TrimPrefix(fmt.Sprintf("%T", ev), "reel."))
		info.Events[kind]++
		switch ev := ev.(type) {
		case reel.Resize:
			info.Resizes = append(info.Resizes, fmt.Sprintf("%dx%d@%v", ev.Cols, ev.Rows, ev.At()))
		case reel.Exit:
			info.Exit = &ev.Status
		case reel.Mark:
			info.Marks = append(info.Marks, castMark{Label: ev.Label, At: ev.At().String()})
		case reel.Annotation:
			info.Annotations[ev.Kind]++
			var e struct {
				Effect string `json:"effect"`
			}
			if ev.Kind == reel.EffectKind && ev.Decode(&e) == nil {
				info.Effects = append(info.Effects, e.Effect)
			}
		}
	}
	if c.JSON {
		return json.MarshalWrite(os.Stdout, info, jsontext.Multiline(true))
	}
	w := os.Stdout
	fmt.Fprintf(w, "title        %s\n", cmp.Or(info.Title, "-"))
	fmt.Fprintf(w, "size         %dx%d\n", info.Cols, info.Rows)
	fmt.Fprintf(w, "duration     %v\n", info.Duration)
	fmt.Fprintf(w, "events       %s\n", counts(info.Events))
	if len(info.Resizes) > 0 {
		fmt.Fprintf(w, "resizes      %s\n", strings.Join(info.Resizes, " · "))
	}
	if info.Exit != nil {
		fmt.Fprintf(w, "exit         %d\n", *info.Exit)
	}
	label := "marks"
	for _, m := range info.Marks {
		fmt.Fprintf(w, "%-12s %v\t%s\n", label, m.At, m.Label)
		label = ""
	}
	if len(info.Effects) > 0 {
		fmt.Fprintf(w, "effects      %s\n", strings.Join(info.Effects, " · "))
	}
	if len(info.Annotations) > 0 {
		fmt.Fprintf(w, "annotations  %s\n", counts(info.Annotations))
	}
	return nil
}

// counts formats a count per name, sorted by name.
func counts(m map[string]int) string {
	parts := make([]string, 0, len(m))
	for _, k := range slices.Sorted(maps.Keys(m)) {
		parts = append(parts, fmt.Sprintf("%d %s", m[k], k))
	}
	return strings.Join(parts, " · ")
}

// AttachCmd renders a script(1) typescript.
type AttachCmd struct {
	RenderFlags `embed:""`

	Typescript string `arg:"" type:"existingfile" placeholder:"TYPESCRIPT" help:"Typescript, as script(1) writes it."`
	Timing     string `name:"timing" short:"t" type:"existingfile" placeholder:"FILE" help:"Timing file (script -t or -T), for the output's times; without it, the output is played at once."`
	Size       string `name:"size" default:"80x24" placeholder:"COLSxROWS" help:"Terminal size the typescript was recorded at."`
}

// Run renders the typescript.
func (c *AttachCmd) Run(ctx context.Context) error {
	cols, rows, err := parseSize(c.Size)
	if err != nil {
		return err
	}
	p, err := c.profile()
	if err != nil {
		return err
	}
	ts, err := os.Open(c.Typescript)
	if err != nil {
		return err
	}
	defer ts.Close()
	var opts []reel.AttachOption
	if c.Timing != "" {
		timing, openErr := os.Open(c.Timing)
		if openErr != nil {
			return openErr
		}
		defer timing.Close()
		opts = append(opts, reel.WithTiming(timing))
	}
	s, err := reel.Open(ctx, cols, rows)
	if err != nil {
		return err
	}
	defer s.Close()
	err = reel.Attach(ctx, s, ts, opts...)
	if err != nil {
		return err
	}
	return reel.Export(ctx, s.Timeline(), p)
}

// parseSize parses COLSxROWS.
func parseSize(s string) (cols, rows int, err error) {
	if _, err = fmt.Sscanf(s, "%dx%d", &cols, &rows); err != nil || cols < 1 || rows < 1 {
		return 0, 0, fmt.Errorf("size %q is not COLSxROWS", s)
	}
	return cols, rows, nil
}
