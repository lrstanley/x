// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.
//
// Parts of this code are adapted from github.com/google/go-querystring.
// Copyright (c) 2013 Google: https://github.com/google/go-querystring/blob/master/LICENSE

// Package httpcquery encodes structs into [url.Values] for query strings.
//
// Example:
//
//	type Options struct {
//		Query string `url:"q"`
//		All   bool   `url:"all"`
//		Page  int    `url:"page"`
//	}
//	v, err := httpcquery.Values(Options{"foo", true, 2})
//	// v.Encode() => q=foo&all=true&page=2
//
// Encoder values: If field type T or *T implements [Encoder], [Encoder.EncodeValues]
// runs for that field. A nil *T whose method set includes [Encoder.EncodeValues] on *T
// invokes the method with a nil receiver (the method must handle nil). If *T implements
// [Encoder] but the field is nil and T does not implement [Encoder], a zero T is allocated
// and encoded instead (same as a non-nil *T pointing at zero).
//
// Omitempty: For struct fields, "omitempty" applies to the field value's own emptiness.
// An all-zero nested struct is not treated as empty unless its type implements
// [time.Time.IsZero]-style IsZero() or you omit the parent field yourself; nested encoding
// may still emit child keys. This differs from encoding/json.
//
// Tag pitfall: use a leading comma before options (for example `url:",omitempty"`). Without
// it, the first option is parsed as the parameter name, so `url:"omitempty"` emits the key
// "omitempty".
//
// Unsupported kinds for dedicated encoding (maps, channels, functions, and other scalars
// without special cases) use [fmt.Sprint] on the underlying value.
package httpcquery

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var (
	timeType    = reflect.TypeFor[time.Time]()
	encoderType = reflect.TypeFor[Encoder]()
)

// Encoder can encode itself into [url.Values] under a non-standard scheme.
type Encoder interface {
	EncodeValues(key string, v *url.Values) error
}

// fieldOptions holds parsed `url` tag flags for one field (single pass over tag options).
type fieldOptions struct {
	omitEmpty bool
	asInt     bool
	comma     bool
	space     bool
	semicolon bool
	brackets  bool
	number    bool
	unix      bool
	unixMilli bool
	unixNano  bool
}

func parseFieldOptions(opts []string) (o fieldOptions) {
	for _, opt := range opts {
		switch opt {
		case "omitempty":
			o.omitEmpty = true
		case "int":
			o.asInt = true
		case "comma":
			o.comma = true
		case "space":
			o.space = true
		case "semicolon":
			o.semicolon = true
		case "brackets":
			o.brackets = true
		case "numbered":
			o.number = true
		case "unix":
			o.unix = true
		case "unixmilli":
			o.unixMilli = true
		case "unixnano":
			o.unixNano = true
		}
	}
	return o
}

// Values returns the [url.Values] encoding of v. v must be a struct (or
// pointer to struct); nil pointers yield an empty [url.Values] without error.
//
// Encoding rules:
//   - Exported fields encode unless the `url` tag is "-" or the field is empty
//     and the tag includes "omitempty".
//   - Empty means false, 0, nil pointers/interfaces, zero-length
//     slice/map/string, and [time.Time] for which [time.Time.IsZero] is true.
//   - Types with an IsZero() bool method use that for emptiness after kind-specific
//     rules (for example custom struct types).
//   - Parameter name is from the `url` tag before the first comma, or the field
//     name. Options after the comma include omitempty, int (bools as 1/0),
//     comma, space, semicolon, brackets, numbered (slice encoding), and for
//     [time.Time]: unix, unixmilli, unixnano.
//   - Nested structs use bracketed keys: parent[field]=...
//   - Anonymous struct fields are flattened after sibling fields unless the
//     `url` tag sets an explicit name.
//   - [time.Time] uses RFC3339, unless a `layout` struct tag gives [time.Time.Format].
//   - Slices and arrays use multiple keys by default; `del:"x"` sets a single
//     joined value when none of comma, space, or semicolon apply.
//   - Zero-length arrays (for example [0]string) emit no keys, like an empty slice.
func Values(v any) (url.Values, error) {
	out := url.Values{}
	if v == nil {
		return out, nil
	}

	val := reflect.ValueOf(v)
	for val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return out, nil
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("httpcquery: Values expects struct input, got %s", val.Kind())
	}

	if err := walkStruct(out, val, ""); err != nil {
		return nil, err
	}
	return out, nil
}

func walkStruct(dst url.Values, val reflect.Value, scope string) error { //nolint:gocognit
	typ := val.Type()
	var embedded []reflect.Value

	for i := range typ.NumField() {
		sf := typ.Field(i)
		if sf.PkgPath != "" && !sf.Anonymous {
			continue
		}

		fv := val.Field(i)
		tag := sf.Tag.Get("url")
		if tag == "-" {
			continue
		}

		name, tagOpts := parseTag(tag)
		fo := parseFieldOptions(tagOpts)
		if name == "" && sf.Anonymous {
			inner := reflect.Indirect(fv)
			if inner.IsValid() && inner.Kind() == reflect.Struct {
				embedded = append(embedded, inner)
				continue
			}
		}
		if name == "" {
			name = sf.Name
		}

		param := name
		if scope != "" {
			param = scope + "[" + name + "]"
		}

		if fo.omitEmpty && isEmptyValue(fv) {
			continue
		}

		if fv.Type().Implements(encoderType) {
			ev := fv
			if !reflect.Indirect(fv).IsValid() && fv.Type().Elem().Implements(encoderType) {
				ev = reflect.New(fv.Type().Elem())
			}
			enc := ev.Interface().(Encoder) //nolint:errcheck // We already confirmed the type implements the interface.
			if err := enc.EncodeValues(param, &dst); err != nil {
				return err
			}
			continue
		}

		sv := fv
		for sv.Kind() == reflect.Pointer {
			if sv.IsNil() {
				break
			}
			sv = sv.Elem()
		}

		switch {
		case sv.Kind() == reflect.Slice || sv.Kind() == reflect.Array:
			if sv.Len() == 0 {
				continue
			}
			if err := encodeSlice(dst, sf, sv, param, fo); err != nil {
				return err
			}
		case sv.Type() == timeType:
			dst.Add(param, scalarString(sv, sf, fo))
		case sv.Kind() == reflect.Struct:
			if err := walkStruct(dst, sv, param); err != nil {
				return err
			}
		default:
			dst.Add(param, scalarString(sv, sf, fo))
		}
	}

	for _, e := range embedded {
		if err := walkStruct(dst, e, scope); err != nil {
			return err
		}
	}
	return nil
}

func encodeSlice(dst url.Values, sf reflect.StructField, sv reflect.Value, name string, opts fieldOptions) error {
	var del string
	switch {
	case opts.comma:
		del = ","
	case opts.space:
		del = " "
	case opts.semicolon:
		del = ";"
	case opts.brackets:
		name += "[]"
	default:
		del = sf.Tag.Get("del")
	}

	if del != "" {
		var b strings.Builder
		for i := range sv.Len() {
			if i > 0 {
				b.WriteString(del)
			}
			b.WriteString(elemString(sv.Index(i), sf, opts))
		}
		dst.Add(name, b.String())
		return nil
	}

	for i := range sv.Len() {
		k := name
		if opts.number {
			k = name + strconv.Itoa(i)
		}
		dst.Add(k, elemString(sv.Index(i), sf, opts))
	}
	return nil
}

func elemString(v reflect.Value, sf reflect.StructField, opts fieldOptions) string {
	return scalarString(v, sf, opts)
}

func scalarString(v reflect.Value, sf reflect.StructField, opts fieldOptions) string {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}

	if !v.IsValid() {
		return ""
	}

	if v.Kind() == reflect.Bool && opts.asInt {
		if v.Bool() {
			return "1"
		}
		return "0"
	}

	if v.Type() == timeType {
		t := v.Interface().(time.Time) //nolint:errcheck // We already confirmed the type is a [time.Time].
		switch {
		case opts.unix:
			return strconv.FormatInt(t.Unix(), 10)
		case opts.unixMilli:
			return strconv.FormatInt(t.UnixNano()/1e6, 10)
		case opts.unixNano:
			return strconv.FormatInt(t.UnixNano(), 10)
		default:
			if layout := sf.Tag.Get("layout"); layout != "" {
				return t.Format(layout)
			}
			return t.Format(time.RFC3339)
		}
	}

	return fmt.Sprint(v.Interface())
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	case reflect.Invalid:
		return !v.IsValid()
	case reflect.Complex64, reflect.Complex128:
		return v.Complex() == 0
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return v.IsNil()
	case reflect.Struct:
		// [time.Time] and other types may implement IsZero; handled below.
	}

	if z, ok := v.Interface().(interface{ IsZero() bool }); ok {
		return z.IsZero()
	}
	return false
}

// parseTag splits a struct field's url tag into its name and options.
func parseTag(tag string) (name string, opts []string) {
	s := strings.Split(tag, ",")
	return s[0], s[1:]
}
