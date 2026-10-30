// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"encoding/hex"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"image"
	"image/color"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
)

// The names of the effects in their annotations.
const (
	effectKeys      = "keys"
	effectIntro     = "intro"
	effectPause     = "pause"
	effectHighlight = "highlight"
	effectZoom      = "zoom"
	effectToasts    = "toasts"
)

// effectDecoders decode the data of an annotation, by effect name.
var effectDecoders = map[string]func(data jsontext.Value) (reel.Effect, error){
	effectKeys:      decodeEffect(keysData.effect),
	effectIntro:     decodeEffect(introData.effect),
	effectPause:     decodeEffect(pauseData.effect),
	effectHighlight: decodeEffect(highlightData.effect),
	effectZoom:      decodeEffect(zoomData.effect),
	effectToasts:    decodeEffect(toastsData.effect),
}

// FromAnnotation returns the effect an annotation of kind [reel.EffectKind]
// describes (see [reel.Describer]). The effect works as the one described
// did, but for what its description leaves out (see the package
// documentation). An annotation of another kind, of an unknown effect or
// region, or with an unknown field, is an error here, though not when
// reading the file (see [reel.AnnotationSchema]).
func FromAnnotation(a reel.Annotation) (reel.Effect, error) {
	if a.Kind != reel.EffectKind {
		return nil, fmt.Errorf("effects: annotation of kind %q, not %q", a.Kind, reel.EffectKind)
	}
	var head struct {
		Effect string `json:"effect"`
	}
	if err := a.Decode(&head); err != nil {
		return nil, err
	}
	decode, ok := effectDecoders[head.Effect]
	if !ok {
		return nil, fmt.Errorf("effects: unknown effect %q", head.Effect)
	}
	e, err := decode(a.Data)
	if err != nil {
		return nil, fmt.Errorf("effects: %s: %w", head.Effect, err)
	}
	return e, nil
}

// FromTimeline returns the effects the annotations of kind
// [reel.EffectKind] in t describe, in order, as [FromAnnotation] reads them.
func FromTimeline(t *reel.Timeline) ([]reel.Effect, error) {
	var out []reel.Effect
	for a := range t.Annotations() {
		if a.Kind != reel.EffectKind {
			continue
		}
		e, err := FromAnnotation(a)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// effectData is the data of an effect's annotation: the effect's name, then
// the fields of T, its options.
type effectData[T any] struct {
	Effect string `json:"effect"`
	Data   T      `json:",embed"`
}

// describe returns the annotation of effect, with the fields of data, a
// struct.
func describe[T any](effect string, data T) (reel.Annotation, error) {
	a, err := reel.NewAnnotation(reel.EffectKind, effectData[T]{Effect: effect, Data: data})
	if err != nil {
		return reel.Annotation{}, fmt.Errorf("effects: %s: %w", effect, err)
	}
	return a, nil
}

// decodeAs unmarshals the JSON object data into a T, and returns what build
// makes of it. An unknown member is an error: what build made without it
// would not be what data describes.
func decodeAs[T, R any](data jsontext.Value, build func(T) (R, error)) (R, error) {
	var d T
	if err := json.Unmarshal(data, &d, json.RejectUnknownMembers(true)); err != nil {
		var zero R
		return zero, err
	}
	return build(d)
}

// decodeEffect returns decodeAs for the data of an effect's annotation.
func decodeEffect[T any](build func(T) (reel.Effect, error)) func(jsontext.Value) (reel.Effect, error) {
	return func(data jsontext.Value) (reel.Effect, error) {
		return decodeAs(data, func(d effectData[T]) (reel.Effect, error) { return build(d.Data) })
	}
}

// millis is d rounded to whole milliseconds.
func millis(d time.Duration) int64 { return d.Round(time.Millisecond).Milliseconds() }

func fromMillis(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }

type keysData struct {
	ShownMS int64 `json:"shown_ms"`
}

// Describe implements [reel.Describer].
func (e keys) Describe() (reel.Annotation, error) {
	return describe(effectKeys, keysData{ShownMS: millis(e.shown)})
}

func (d keysData) effect() (reel.Effect, error) {
	return Keys(fromMillis(d.ShownMS)), nil
}

type introData struct {
	DurationMS int64      `json:"duration_ms"`
	Lines      []lineData `json:"lines,omitzero"`
}

type lineData struct {
	Text  string    `json:"text"`
	Style styleData `json:"style,omitzero"`
}

type styleData struct {
	Fg             colorData `json:"fg,omitzero"`
	Bg             colorData `json:"bg,omitzero"`
	UnderlineColor colorData `json:"underline_color,omitzero"`
	Underline      string    `json:"underline,omitzero"`
	Attrs          []string  `json:"attrs,omitzero"`
}

// underlineNames are the names of the [vt.Underline] styles, by value.
var underlineNames = []string{"", "single", "double", "curly", "dotted", "dashed"}

// attrNames are the names of the [vt.Attr] bits, lowest first.
var attrNames = []string{"bold", "faint", "italic", "blink", "rapid_blink", "reverse", "conceal", "strikethrough"}

// Describe implements [reel.Describer].
func (e intro) Describe() (reel.Annotation, error) {
	d := introData{DurationMS: millis(e.d)}
	for _, line := range e.lines {
		st := line.Style
		if int(st.Underline) >= len(underlineNames) {
			return reel.Annotation{}, fmt.Errorf("effects: intro: unknown underline style %d", st.Underline)
		}
		sd := styleData{
			Fg:             colorData{st.Fg},
			Bg:             colorData{st.Bg},
			UnderlineColor: colorData{st.UnderlineColor},
			Underline:      underlineNames[st.Underline],
		}
		for i, name := range attrNames {
			if st.Attrs&(1<<i) != 0 {
				sd.Attrs = append(sd.Attrs, name)
			}
		}
		d.Lines = append(d.Lines, lineData{Text: line.Text, Style: sd})
	}
	return describe(effectIntro, d)
}

func (d introData) effect() (reel.Effect, error) {
	var lines []Line
	for _, ld := range d.Lines {
		sd := ld.Style
		st := vt.Style{Fg: sd.Fg.c, Bg: sd.Bg.c, UnderlineColor: sd.UnderlineColor.c}
		if sd.Underline != "" {
			u := slices.Index(underlineNames, sd.Underline)
			if u < 0 {
				return nil, fmt.Errorf("unknown underline style %q", sd.Underline)
			}
			st.Underline = vt.Underline(u) //nolint:gosec // An index of underlineNames.
		}
		for _, name := range sd.Attrs {
			i := slices.Index(attrNames, name)
			if i < 0 {
				return nil, fmt.Errorf("unknown attribute %q", name)
			}
			st.Attrs |= 1 << i
		}
		lines = append(lines, Line{Text: ld.Text, Style: st})
	}
	return Intro(fromMillis(d.DurationMS), lines...), nil
}

type toastsData struct {
	ShownMS   int64  `json:"shown_ms,omitzero"`
	Corner    string `json:"corner,omitzero"`
	Clipboard bool   `json:"clipboard,omitzero"`
}

// Describe implements [reel.Describer].
func (e toasts) Describe() (reel.Annotation, error) {
	if err := e.corner.check("toast"); err != nil {
		return reel.Annotation{}, err
	}
	var d toastsData
	if e.shown > 0 {
		d.ShownMS = millis(e.shown)
	}
	if e.corner != TopRight {
		d.Corner = cornerNames[e.corner]
	}
	d.Clipboard = e.clipboard
	return describe(effectToasts, d)
}

func (d toastsData) effect() (reel.Effect, error) {
	opts := []ToastOption{WithToastDuration(fromMillis(d.ShownMS))}
	if d.Corner != "" {
		c, err := parseCorner(d.Corner)
		if err != nil {
			return nil, err
		}
		opts = append(opts, WithToastCorner(c))
	}
	if d.Clipboard {
		opts = append(opts, WithClipboardToasts())
	}
	return Toasts(opts...), nil
}

type pauseData struct {
	Points      []pointData `json:"points,omitzero"`
	Badge       *bool       `json:"badge,omitzero"`
	BadgeCorner string      `json:"badge_corner,omitzero"`
	BadgeSizePX int         `json:"badge_size_px,omitzero"`
	BadgeColor  colorData   `json:"badge_color,omitzero"`
}

type pointData struct {
	At         string   `json:"at"`
	DurationMS int64    `json:"duration_ms"`
	Labels     []string `json:"labels,omitzero"`
	Pattern    *string  `json:"pattern,omitzero"`
}

// Describe implements [reel.Describer].
func (e pause) Describe() (reel.Annotation, error) {
	var d pauseData
	if len(e.points) != 1 || e.points[0].at != pauseAtEnd || e.points[0].d != pauseEnd {
		for _, pt := range e.points {
			if err := pt.check(); err != nil {
				return reel.Annotation{}, err
			}
			pd := pointData{At: pt.at, DurationMS: millis(pt.d), Labels: pt.labels}
			if pt.re != nil {
				pattern := pt.re.String()
				pd.Pattern = &pattern
			}
			d.Points = append(d.Points, pd)
		}
	}
	if e.noBadge {
		d.Badge = new(false)
		return describe(effectPause, d)
	}
	if err := e.corner.check("pause badge"); err != nil {
		return reel.Annotation{}, err
	}
	if e.corner != TopRight {
		d.BadgeCorner = cornerNames[e.corner]
	}
	d.BadgeSizePX = e.size
	if e.color != badgeColor {
		d.BadgeColor = colorData{e.color}
	}
	return describe(effectPause, d)
}

func (d pauseData) effect() (reel.Effect, error) {
	var opts []PauseOption
	for _, pd := range d.Points {
		dur := fromMillis(pd.DurationMS)
		switch pd.At {
		case pauseAtEnd:
			opts = append(opts, PauseAtEnd(dur))
		case pauseAtMarks:
			opts = append(opts, PauseAtMarks(dur, pd.Labels...))
		case pauseAtMatch:
			if pd.Pattern == nil {
				return nil, errors.New("pause at match without a pattern")
			}
			re, err := regexp.Compile(*pd.Pattern)
			if err != nil {
				return nil, err
			}
			opts = append(opts, PauseAtMatch(dur, re))
		default:
			return nil, fmt.Errorf("unknown pause point %q", pd.At)
		}
	}
	if d.Badge != nil && !*d.Badge {
		opts = append(opts, WithoutBadge())
	}
	if d.BadgeCorner != "" {
		c, err := parseCorner(d.BadgeCorner)
		if err != nil {
			return nil, fmt.Errorf("badge: %w", err)
		}
		opts = append(opts, WithBadgeCorner(c))
	}
	opts = append(opts, WithBadgeSize(d.BadgeSizePX))
	if d.BadgeColor.c != nil {
		opts = append(opts, WithBadgeColor(d.BadgeColor.c))
	}
	return Pause(opts...), nil
}

type highlightData struct {
	Region    jsontext.Value `json:"region"`
	Timing    timingData     `json:"timing,omitzero"`
	Dim       *float64       `json:"dim,omitzero"`
	Outline   *outlineData   `json:"outline,omitzero"`
	PaddingPX *int           `json:"padding_px,omitzero"`
}

type outlineData struct {
	Color   colorData `json:"color,omitzero"`
	WidthPX int       `json:"width_px,omitzero"`
}

// Describe implements [reel.Describer].
func (e highlight) Describe() (reel.Annotation, error) {
	region, err := describeRegion(e.region)
	if err != nil {
		return reel.Annotation{}, err
	}
	d := highlightData{Region: region, Timing: e.timing.data()}
	if e.dim != highlightDim {
		d.Dim = &e.dim
	}
	if e.outline != (color.NRGBA{}) || e.outlinePx != 0 {
		d.Outline = &outlineData{WidthPX: e.outlinePx}
		if e.outline != outlineColor {
			d.Outline.Color = colorData{e.outline}
		}
	}
	if e.padSet {
		d.PaddingPX = &e.pad
	}
	return describe(effectHighlight, d)
}

// CheckMarks implements [reel.MarkChecker]: the marks of [Between] must be
// in tl's window.
func (e highlight) CheckMarks(tl *reel.Timeline) error { return e.timing.checkMarks(tl) }

func (d highlightData) effect() (reel.Effect, error) {
	region, err := decodeRegion(d.Region)
	if err != nil {
		return nil, err
	}
	opts := []HighlightOption{d.Timing.option()}
	if d.Dim != nil {
		opts = append(opts, DimOthers(*d.Dim))
	}
	if d.Outline != nil {
		opts = append(opts, Outline(d.Outline.Color.c, d.Outline.WidthPX))
	}
	if d.PaddingPX != nil {
		opts = append(opts, Padding(*d.PaddingPX))
	}
	return Highlight(region, opts...), nil
}

type zoomData struct {
	Region jsontext.Value `json:"region"`
	Timing timingData     `json:"timing,omitzero"`
	Scale  *float64       `json:"scale,omitzero"`
	Anchor *anchorData    `json:"anchor,omitzero"`
}

type anchorData struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Describe implements [reel.Describer].
func (e zoom) Describe() (reel.Annotation, error) {
	region, err := describeRegion(e.region)
	if err != nil {
		return reel.Annotation{}, err
	}
	d := zoomData{Region: region, Timing: e.timing.data()}
	if e.scaleSet {
		d.Scale = &e.scale
	}
	if e.ax != 0.5 || e.ay != 0.5 {
		d.Anchor = &anchorData{X: e.ax, Y: e.ay}
	}
	return describe(effectZoom, d)
}

// CheckMarks implements [reel.MarkChecker]: the marks of [Between] must be
// in tl's window.
func (e zoom) CheckMarks(tl *reel.Timeline) error { return e.timing.checkMarks(tl) }

func (d zoomData) effect() (reel.Effect, error) {
	region, err := decodeRegion(d.Region)
	if err != nil {
		return nil, err
	}
	opts := []ZoomOption{d.Timing.option()}
	if d.Scale != nil {
		opts = append(opts, Scale(*d.Scale))
	}
	if d.Anchor != nil {
		opts = append(opts, Anchor(d.Anchor.X, d.Anchor.Y))
	}
	return Zoom(region, opts...), nil
}

type timingData struct {
	Between   *betweenData `json:"between,omitzero"`
	EaseInMS  int64        `json:"ease_in_ms,omitzero"`
	EaseOutMS int64        `json:"ease_out_ms,omitzero"`
}

type betweenData struct {
	From string `json:"from,omitzero"`
	To   string `json:"to,omitzero"`
}

func (t timing) data() timingData {
	d := timingData{EaseInMS: millis(t.easeIn), EaseOutMS: millis(t.easeOut)}
	if t.from != "" || t.to != "" {
		d.Between = &betweenData{From: t.from, To: t.to}
	}
	return d
}

// option returns the [Timing] option that sets the timing d describes.
func (d timingData) option() Timing {
	t := timing{easeIn: fromMillis(d.EaseInMS), easeOut: fromMillis(d.EaseOutMS)}
	if d.Between != nil {
		t.from, t.to = d.Between.From, d.Between.To
	}
	return func(dst *timing) { *dst = t }
}

// The types of regions in annotations.
const (
	regionTypeCells  = "cells"
	regionTypeRows   = "rows"
	regionTypeText   = "text"
	regionTypeRegexp = "regexp"
	regionTypeEnv    = "env"
	regionTypeAny    = "any"
)

type cellsData struct {
	Type string `json:"type"`
	X0   int    `json:"x0"`
	Y0   int    `json:"y0"`
	X1   int    `json:"x1"`
	Y1   int    `json:"y1"`
}

func (d cellsData) region() (Region, error) { return Cells(image.Rect(d.X0, d.Y0, d.X1, d.Y1)), nil }

type rowsData struct {
	Type string `json:"type"`
	From int    `json:"from"`
	To   int    `json:"to"`
}

func (d rowsData) region() (Region, error) { return Rows(d.From, d.To), nil }

type textData struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (d textData) region() (Region, error) { return Text(d.Text), nil }

type regexpData struct {
	Type    string `json:"type"`
	Pattern string `json:"pattern"`
}

func (d regexpData) region() (Region, error) {
	re, err := regexp.Compile(d.Pattern)
	if err != nil {
		return nil, err
	}
	return Regexp(re), nil
}

type envData struct {
	Type  string   `json:"type"`
	Names []string `json:"names"`
}

func (d envData) region() (Region, error) { return Env(d.Names...), nil }

type anyData struct {
	Type    string           `json:"type"`
	Regions []jsontext.Value `json:"regions"`
}

func (d anyData) region() (Region, error) {
	regions := make([]Region, len(d.Regions))
	for i, sub := range d.Regions {
		var err error
		if regions[i], err = decodeRegion(sub); err != nil {
			return nil, err
		}
	}
	return Any(regions...), nil
}

// describeRegion returns the JSON object that describes r. A region of a
// caller's own, or [Any] of one, is an error wrapping
// [errors.ErrUnsupported].
func describeRegion(r Region) (jsontext.Value, error) {
	var v any
	switch r := r.(type) {
	case nil:
		return nil, errors.New("effects: no region")
	case cellsRegion:
		v = cellsData{regionTypeCells, r.Min.X, r.Min.Y, r.Max.X, r.Max.Y}
	case rowsRegion:
		v = rowsData{regionTypeRows, r.y0, r.y1}
	case textRegion:
		v = textData{regionTypeText, string(r)}
	case regexpRegion:
		if r.re == nil {
			return nil, errors.New("effects: regexp region without a regexp")
		}
		v = regexpData{regionTypeRegexp, r.re.String()}
	case envRegion:
		v = envData{regionTypeEnv, r}
	case anyRegion:
		d := anyData{Type: regionTypeAny, Regions: make([]jsontext.Value, len(r))}
		for i, sub := range r {
			var err error
			if d.Regions[i], err = describeRegion(sub); err != nil {
				return nil, err
			}
		}
		v = d
	default:
		return nil, fmt.Errorf("effects: region %T has no description: %w", r, errors.ErrUnsupported)
	}
	return json.Marshal(v)
}

// decodeRegion returns the region the JSON object v describes.
func decodeRegion(v jsontext.Value) (Region, error) {
	if len(v) == 0 {
		return nil, errors.New("no region")
	}
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(v, &head); err != nil {
		return nil, fmt.Errorf("region: %w", err)
	}
	var r Region
	var err error
	switch head.Type {
	case regionTypeCells:
		r, err = decodeAs(v, cellsData.region)
	case regionTypeRows:
		r, err = decodeAs(v, rowsData.region)
	case regionTypeText:
		r, err = decodeAs(v, textData.region)
	case regionTypeRegexp:
		r, err = decodeAs(v, regexpData.region)
	case regionTypeEnv:
		r, err = decodeAs(v, envData.region)
	case regionTypeAny:
		r, err = decodeAs(v, anyData.region)
	default:
		return nil, fmt.Errorf("unknown region type %q", head.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("%s region: %w", head.Type, err)
	}
	return r, nil
}

// colorData is a color in an annotation: a palette index from 0 to 255 for
// an indexed color, and otherwise "#rrggbb", or "#rrggbbaa" when it is not
// opaque.
type colorData struct{ c color.Color }

func (c colorData) IsZero() bool { return c.c == nil }

func (c colorData) MarshalJSON() ([]byte, error) {
	switch v := c.c.(type) {
	case vt.Indexed:
		return json.Marshal(uint8(v))
	case ansi.IndexedColor:
		return json.Marshal(uint8(v))
	case ansi.BasicColor:
		return json.Marshal(uint8(v))
	}
	n, _ := color.NRGBAModel.Convert(c.c).(color.NRGBA)
	b := []byte{n.R, n.G, n.B, n.A}
	if n.A == 0xff {
		b = b[:3]
	}
	return json.Marshal("#" + hex.EncodeToString(b))
}

func (c *colorData) UnmarshalJSON(data []byte) error {
	var i uint8
	if json.Unmarshal(data, &i) == nil {
		c.c = vt.Indexed(i)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("color %s is neither a palette index nor a string", data)
	}
	b, err := hex.DecodeString(strings.TrimPrefix(s, "#"))
	if err != nil || !strings.HasPrefix(s, "#") || (len(b) != 3 && len(b) != 4) {
		return fmt.Errorf("color %q is not #rrggbb or #rrggbbaa", s)
	}
	n := color.NRGBA{R: b[0], G: b[1], B: b[2], A: 0xff}
	if len(b) == 4 {
		n.A = b[3]
	}
	c.c = n
	return nil
}
