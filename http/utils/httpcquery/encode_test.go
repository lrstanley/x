// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package httpcquery

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"slices"
	"testing"
	"time"
)

func testValue(t *testing.T, input any, want url.Values) {
	t.Helper()
	v, err := Values(input)
	if err != nil {
		t.Errorf("Values(%q) returned error: %v", input, err)
		return
	}
	if !reflect.DeepEqual(want, v) {
		t.Errorf("Values(%#v) mismatch\nwant: %#v\n got: %#v", input, want, v)
	}
}

func TestValues_BasicTypes(t *testing.T) {
	tests := []struct {
		input any
		want  url.Values
	}{
		{struct{ V string }{}, url.Values{"V": {""}}},
		{struct{ V int }{}, url.Values{"V": {"0"}}},
		{struct{ V uint }{}, url.Values{"V": {"0"}}},
		{struct{ V float32 }{}, url.Values{"V": {"0"}}},
		{struct{ V bool }{}, url.Values{"V": {"false"}}},

		{struct{ V string }{"v"}, url.Values{"V": {"v"}}},
		{struct{ V int }{1}, url.Values{"V": {"1"}}},
		{struct{ V uint }{1}, url.Values{"V": {"1"}}},
		{struct{ V float32 }{0.1}, url.Values{"V": {"0.1"}}},
		{struct{ V bool }{true}, url.Values{"V": {"true"}}},

		{
			struct {
				V bool `url:",int"`
			}{false},
			url.Values{"V": {"0"}},
		},
		{
			struct {
				V bool `url:",int"`
			}{true},
			url.Values{"V": {"1"}},
		},

		{
			struct {
				V time.Time
			}{time.Date(2000, 1, 1, 12, 34, 56, 0, time.UTC)},
			url.Values{"V": {"2000-01-01T12:34:56Z"}},
		},
		{
			struct {
				V time.Time `url:",unix"`
			}{time.Date(2000, 1, 1, 12, 34, 56, 0, time.UTC)},
			url.Values{"V": {"946730096"}},
		},
		{
			struct {
				V time.Time `url:",unixmilli"`
			}{time.Date(2000, 1, 1, 12, 34, 56, 0, time.UTC)},
			url.Values{"V": {"946730096000"}},
		},
		{
			struct {
				V time.Time `url:",unixnano"`
			}{time.Date(2000, 1, 1, 12, 34, 56, 0, time.UTC)},
			url.Values{"V": {"946730096000000000"}},
		},
		{
			struct {
				V time.Time `layout:"2006-01-02"`
			}{time.Date(2000, 1, 1, 12, 34, 56, 0, time.UTC)},
			url.Values{"V": {"2000-01-01"}},
		},
	}

	for _, tt := range tests {
		testValue(t, tt.input, tt.want)
	}
}

func TestValues_Pointers(t *testing.T) {
	str := "s"
	strPtr := &str

	tests := []struct {
		input any
		want  url.Values
	}{
		{struct{ V *string }{}, url.Values{"V": {""}}},
		{struct{ V *int }{}, url.Values{"V": {""}}},

		{struct{ V *string }{&str}, url.Values{"V": {"s"}}},
		{struct{ V **string }{&strPtr}, url.Values{"V": {"s"}}},

		{struct{ V []*string }{}, url.Values{}},
		{struct{ V []*string }{[]*string{&str, &str}}, url.Values{"V": {"s", "s"}}},

		{struct{ V *[]string }{}, url.Values{"V": {""}}},
		{struct{ V *[]string }{&[]string{"a", "b"}}, url.Values{"V": {"a", "b"}}},

		{(*struct{})(nil), url.Values{}},
		{&struct{}{}, url.Values{}},
		{&struct{ V string }{}, url.Values{"V": {""}}},
		{&struct{ V string }{"v"}, url.Values{"V": {"v"}}},
	}

	for _, tt := range tests {
		testValue(t, tt.input, tt.want)
	}
}

func TestValues_Slices(t *testing.T) {
	tests := []struct {
		input any
		want  url.Values
	}{
		{
			struct{ V []string }{},
			url.Values{},
		},
		{
			struct{ V []string }{[]string{}},
			url.Values{},
		},
		{
			struct{ V []string }{[]string{""}},
			url.Values{"V": {""}},
		},
		{
			struct{ V []string }{[]string{"a", "b"}},
			url.Values{"V": {"a", "b"}},
		},
		{
			struct {
				V []string `url:",comma"`
			}{[]string{}},
			url.Values{},
		},
		{
			struct {
				V []string `url:",comma"`
			}{[]string{""}},
			url.Values{"V": {""}},
		},
		{
			struct {
				V []string `url:",comma"`
			}{[]string{"a", "b"}},
			url.Values{"V": {"a,b"}},
		},
		{
			struct {
				V []string `url:",space"`
			}{[]string{"a", "b"}},
			url.Values{"V": {"a b"}},
		},
		{
			struct {
				V []string `url:",semicolon"`
			}{[]string{"a", "b"}},
			url.Values{"V": {"a;b"}},
		},
		{
			struct {
				V []string `url:",brackets"`
			}{[]string{"a", "b"}},
			url.Values{"V[]": {"a", "b"}},
		},
		{
			struct {
				V []string `url:",numbered"`
			}{[]string{"a", "b"}},
			url.Values{"V0": {"a"}, "V1": {"b"}},
		},

		{
			struct{ V [2]string }{},
			url.Values{"V": {"", ""}},
		},
		{
			struct{ V [2]string }{[2]string{"a", "b"}},
			url.Values{"V": {"a", "b"}},
		},
		{
			struct {
				V [2]string `url:",comma"`
			}{[2]string{"a", "b"}},
			url.Values{"V": {"a,b"}},
		},
		{
			struct {
				V [2]string `url:",space"`
			}{[2]string{"a", "b"}},
			url.Values{"V": {"a b"}},
		},
		{
			struct {
				V [2]string `url:",semicolon"`
			}{[2]string{"a", "b"}},
			url.Values{"V": {"a;b"}},
		},
		{
			struct {
				V [2]string `url:",brackets"`
			}{[2]string{"a", "b"}},
			url.Values{"V[]": {"a", "b"}},
		},
		{
			struct {
				V [2]string `url:",numbered"`
			}{[2]string{"a", "b"}},
			url.Values{"V0": {"a"}, "V1": {"b"}},
		},

		{
			struct {
				V []string `del:","`
			}{[]string{"a", "b"}},
			url.Values{"V": {"a,b"}},
		},
		{
			struct {
				V []string `del:"|"`
			}{[]string{"a", "b"}},
			url.Values{"V": {"a|b"}},
		},
		{
			struct {
				V []string `del:"🥑"`
			}{[]string{"a", "b"}},
			url.Values{"V": {"a🥑b"}},
		},

		{
			struct {
				V []bool `url:",space,int"`
			}{[]bool{true, false}},
			url.Values{"V": {"1 0"}},
		},
	}

	for _, tt := range tests {
		testValue(t, tt.input, tt.want)
	}
}

func TestValues_NestedTypes(t *testing.T) {
	type SubNested struct {
		Value string `url:"value"`
	}

	type Nested struct {
		A   SubNested  `url:"a"`
		B   *SubNested `url:"b"`
		Ptr *SubNested `url:"ptr,omitempty"`
	}

	tests := []struct {
		input any
		want  url.Values
	}{
		{
			struct {
				Nest Nested `url:"nest"`
			}{
				Nested{
					A: SubNested{
						Value: "v",
					},
				},
			},
			url.Values{
				"nest[a][value]": {"v"},
				"nest[b]":        {""},
			},
		},
		{
			struct {
				Nest Nested `url:"nest"`
			}{
				Nested{
					Ptr: &SubNested{
						Value: "v",
					},
				},
			},
			url.Values{
				"nest[a][value]":   {""},
				"nest[b]":          {""},
				"nest[ptr][value]": {"v"},
			},
		},
		{
			nil,
			url.Values{},
		},
	}

	for _, tt := range tests {
		testValue(t, tt.input, tt.want)
	}
}

func TestValues_OmitEmpty(t *testing.T) {
	str := ""

	tests := []struct {
		input any
		want  url.Values
	}{
		{struct{ v string }{}, url.Values{}},
		{
			struct {
				V string `url:",omitempty"`
			}{},
			url.Values{},
		},
		{
			struct {
				V string `url:"-"`
			}{},
			url.Values{},
		},
		{
			struct {
				V string `url:"omitempty"`
			}{},
			url.Values{"omitempty": {""}},
		},
		{
			struct {
				V *string `url:",omitempty"`
			}{&str},
			url.Values{"V": {""}},
		},
	}

	for _, tt := range tests {
		testValue(t, tt.input, tt.want)
	}
}

func TestValues_EmbeddedStructs(t *testing.T) {
	type Inner struct {
		V string
	}
	type Outer struct {
		Inner
	}
	type OuterPtr struct {
		*Inner
	}
	type Mixed struct {
		Inner
		V string
	}
	type unexported struct {
		Inner
		V string
	}
	type Exported struct {
		unexported
	}

	tests := []struct {
		input any
		want  url.Values
	}{
		{
			Outer{Inner{V: "a"}},
			url.Values{"V": {"a"}},
		},
		{
			OuterPtr{&Inner{V: "a"}},
			url.Values{"V": {"a"}},
		},
		{
			Mixed{Inner: Inner{V: "a"}, V: "b"},
			url.Values{"V": {"b", "a"}},
		},
		{
			Exported{
				unexported{
					Inner: Inner{V: "bar"},
					V:     "foo",
				},
			},
			url.Values{"V": {"foo", "bar"}},
		},
	}

	for _, tt := range tests {
		testValue(t, tt.input, tt.want)
	}
}

func TestValues_InvalidInput(t *testing.T) {
	_, err := Values("")
	if err == nil {
		t.Errorf("expected Values() to return an error on invalid input")
	}
}

type customEncodedStrings []string

func (m customEncodedStrings) EncodeValues(key string, v *url.Values) error {
	for i, arg := range m {
		if arg == "err" {
			return errors.New("encoding error")
		}
		v.Set(fmt.Sprintf("%s.%d", key, i), arg)
	}
	return nil
}

func TestValues_CustomEncodingSlice(t *testing.T) {
	tests := []struct {
		input any
		want  url.Values
	}{
		{
			struct {
				V customEncodedStrings `url:"v"`
			}{},
			url.Values{},
		},
		{
			struct {
				V customEncodedStrings `url:"v"`
			}{[]string{"a", "b"}},
			url.Values{"v.0": {"a"}, "v.1": {"b"}},
		},

		{
			struct {
				V *customEncodedStrings `url:"v"`
			}{},
			url.Values{},
		},
		{
			struct {
				V *customEncodedStrings `url:"v"`
			}{(*customEncodedStrings)(&[]string{"a", "b"})},
			url.Values{"v.0": {"a"}, "v.1": {"b"}},
		},
	}

	for _, tt := range tests {
		testValue(t, tt.input, tt.want)
	}
}

func TestValues_CustomEncoding_Error(t *testing.T) {
	type st struct {
		V customEncodedStrings
	}
	tests := []struct {
		input any
	}{
		{
			st{[]string{"err"}},
		},
		{
			struct{ S st }{st{[]string{"err"}}},
		},
		{
			struct{ st }{st{[]string{"err"}}},
		},
	}
	for _, tt := range tests {
		_, err := Values(tt.input)
		if err == nil {
			t.Errorf("Values(%q) did not return expected encoding error", tt.input)
		}
	}
}

type customEncodedInt int

func (m customEncodedInt) EncodeValues(key string, v *url.Values) error {
	v.Set(key, fmt.Sprintf("_%d", m))
	return nil
}

func TestValues_CustomEncodingInt(t *testing.T) {
	var zero customEncodedInt = 0
	var one customEncodedInt = 1
	tests := []struct {
		input any
		want  url.Values
	}{
		{
			struct {
				V customEncodedInt `url:"v"`
			}{},
			url.Values{"v": {"_0"}},
		},
		{
			struct {
				V customEncodedInt `url:"v,omitempty"`
			}{zero},
			url.Values{},
		},
		{
			struct {
				V customEncodedInt `url:"v"`
			}{one},
			url.Values{"v": {"_1"}},
		},

		{
			struct {
				V *customEncodedInt `url:"v"`
			}{},
			url.Values{"v": {"_0"}},
		},
		{
			struct {
				V *customEncodedInt `url:"v,omitempty"`
			}{},
			url.Values{},
		},
		{
			struct {
				V *customEncodedInt `url:"v,omitempty"`
			}{&zero},
			url.Values{"v": {"_0"}},
		},
		{
			struct {
				V *customEncodedInt `url:"v"`
			}{&one},
			url.Values{"v": {"_1"}},
		},
	}

	for _, tt := range tests {
		testValue(t, tt.input, tt.want)
	}
}

type customEncodedIntPtr int

func (m *customEncodedIntPtr) EncodeValues(key string, v *url.Values) error {
	if m == nil {
		v.Set(key, "undefined")
	} else {
		v.Set(key, fmt.Sprintf("_%d", *m))
	}
	return nil
}

func TestValues_CustomEncodingPointer(t *testing.T) {
	var zero customEncodedIntPtr = 0
	var one customEncodedIntPtr = 1
	tests := []struct {
		input any
		want  url.Values
	}{
		{
			struct {
				V customEncodedIntPtr `url:"v"`
			}{},
			url.Values{"v": {"0"}},
		},
		{
			struct {
				V customEncodedIntPtr `url:"v,omitempty"`
			}{},
			url.Values{},
		},
		{
			struct {
				V customEncodedIntPtr `url:"v"`
			}{one},
			url.Values{"v": {"1"}},
		},

		{
			struct {
				V *customEncodedIntPtr `url:"v"`
			}{},
			url.Values{"v": {"undefined"}},
		},
		{
			struct {
				V *customEncodedIntPtr `url:"v,omitempty"`
			}{},
			url.Values{},
		},
		{
			struct {
				V *customEncodedIntPtr `url:"v"`
			}{&zero},
			url.Values{"v": {"_0"}},
		},
		{
			struct {
				V *customEncodedIntPtr `url:"v,omitempty"`
			}{&zero},
			url.Values{"v": {"_0"}},
		},
		{
			struct {
				V *customEncodedIntPtr `url:"v"`
			}{&one},
			url.Values{"v": {"_1"}},
		},
	}

	for _, tt := range tests {
		testValue(t, tt.input, tt.want)
	}
}

func TestValues_EmbeddedNilPointer(t *testing.T) {
	type Inner struct {
		V string
	}
	type OuterPtr struct {
		*Inner
	}
	testValue(t, OuterPtr{nil}, url.Values{"Inner": {""}})
}

type innerStructWithIsZero struct {
	X string `url:"x"`
}

func (innerStructWithIsZero) IsZero() bool { return true }

func TestValues_NestedOmitEmptyPlainStruct(t *testing.T) {
	type innerPlain struct {
		X string `url:"x"`
	}
	type outerPlain struct {
		N innerPlain `url:"n,omitempty"`
	}
	testValue(t, outerPlain{N: innerPlain{}}, url.Values{"n[x]": {""}})
}

func TestValues_NestedOmitEmptyIsZeroStruct(t *testing.T) {
	type outer struct {
		N innerStructWithIsZero `url:"n,omitempty"`
	}
	testValue(t, outer{N: innerStructWithIsZero{}}, url.Values{})
}

func TestValues_ZeroLengthArray(t *testing.T) {
	testValue(t, struct {
		V [0]string `url:"v"`
	}{}, url.Values{})
}

func TestValues_FloatNegativeZero(t *testing.T) {
	testValue(t, struct {
		V float64 `url:"v"`
	}{math.Copysign(0, -1)}, url.Values{"v": {"-0"}})
}

func TestValues_MapChanComplex(t *testing.T) {
	testValue(t, struct {
		M map[string]string `url:"m"`
	}{
		M: map[string]string{"a": "1", "b": "2"},
	}, url.Values{"m": {"map[a:1 b:2]"}})

	var ch chan int
	testValue(t, struct {
		C chan int `url:"c"`
	}{C: ch}, url.Values{"c": {"<nil>"}})

	testValue(t, struct {
		Z complex128 `url:"z"`
	}{1 + 2i}, url.Values{"z": {"(1+2i)"}})
}

func TestValues_SliceDelimiterPrecedence(t *testing.T) {
	// When multiple delimiters appear in the tag, comma wins (historical order).
	testValue(t, struct {
		V []string `url:",comma,space"`
	}{[]string{"a", "b"}}, url.Values{"V": {"a,b"}})
}

type isEmptyAlways struct{}

func (isEmptyAlways) IsZero() bool { return true }

type isEmptyNever struct{}

func (isEmptyNever) IsZero() bool { return false }

type neverEmptyForOmit struct {
	X string `url:"x"`
}

func (neverEmptyForOmit) IsZero() bool { return false }

func TestValues_OmitEmptyCustomIsZero(t *testing.T) {
	testValue(t, struct {
		A innerStructWithIsZero `url:"a,omitempty"`
	}{}, url.Values{})
	testValue(t, struct {
		N neverEmptyForOmit `url:"n,omitempty"`
	}{}, url.Values{"n[x]": {""}})
}

func TestIsEmptyValue(t *testing.T) {
	str := "string"
	tests := []struct {
		value any
		empty bool
	}{
		{[]int{}, true},
		{[]int{0}, false},
		{[0]int{}, true},
		{[3]int{}, false},
		{[3]int{1}, false},
		{map[string]string{}, true},
		{map[string]string{"a": "b"}, false},

		{"", true},
		{" ", false},
		{"a", false},

		{true, false},
		{false, true},

		{int(0), true},
		{int(1), false},
		{int(-1), false},
		{int8(0), true},
		{int8(1), false},
		{int8(-1), false},
		{int16(0), true},
		{int16(1), false},
		{int16(-1), false},
		{int32(0), true},
		{int32(1), false},
		{int32(-1), false},
		{int64(0), true},
		{int64(1), false},
		{int64(-1), false},
		{uint(0), true},
		{uint(1), false},
		{uint8(0), true},
		{uint8(1), false},
		{uint16(0), true},
		{uint16(1), false},
		{uint32(0), true},
		{uint32(1), false},
		{uint64(0), true},
		{uint64(1), false},

		{float32(0), true},
		{float32(0.0), true},
		{float32(0.1), false},
		{float64(0), true},
		{float64(0.0), true},
		{float64(0.1), false},

		{(*int)(nil), true},
		{new([]int), false},
		{&str, false},

		{time.Time{}, true},
		{time.Now(), false},

		{(*struct{ int })(nil), true},
		{struct{ int }{}, false},
		{struct{ int }{0}, false},
		{struct{ int }{1}, false},

		{isEmptyAlways{}, true},
		{isEmptyNever{}, false},
	}

	for _, tt := range tests {
		got := isEmptyValue(reflect.ValueOf(tt.value))
		want := tt.empty
		if got != want {
			t.Errorf("isEmptyValue(%v) returned %t; want %t", tt.value, got, want)
		}
	}
}

func TestParseTag(t *testing.T) {
	name, opts := parseTag("field,foobar,foo")
	if name != "field" {
		t.Fatalf("name = %q, want field", name)
	}
	for _, tt := range []struct {
		opt  string
		want bool
	}{
		{"foobar", true},
		{"foo", true},
		{"bar", false},
		{"field", false},
	} {
		if slices.Contains(opts, tt.opt) != tt.want {
			t.Errorf("Contains(%q) = %v", tt.opt, !tt.want)
		}
	}
}

func TestParseFieldOptions(t *testing.T) {
	fo := parseFieldOptions([]string{"omitempty", "int", "comma", "numbered", "unix"})
	if !fo.omitEmpty || !fo.asInt || !fo.comma || !fo.number || !fo.unix {
		t.Fatalf("parseFieldOptions: got %+v", fo)
	}
	// Comma wins over space when both appear (encode path checks comma first).
	fo2 := parseFieldOptions([]string{"space", "comma"})
	if !fo2.space || !fo2.comma {
		t.Fatalf("parseFieldOptions: want both flags set, got %+v", fo2)
	}
}

func FuzzValues(f *testing.F) {
	f.Add("", 0, false)
	f.Add("x", -1, true)

	f.Fuzz(func(t *testing.T, s string, n int, b bool) {
		type flat struct {
			S  string   `url:"s"`
			SO string   `url:"so,omitempty"`
			N  int      `url:"n"`
			B  bool     `url:"b"`
			Sl []string `url:"sl,comma"`
		}
		v, err := Values(flat{
			S:  s,
			SO: s,
			N:  n,
			B:  b,
			Sl: []string{s, s},
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = v.Encode()

		type nested struct {
			I struct {
				X string `url:"x"`
			} `url:"i"`
		}
		v2, err := Values(nested{I: struct {
			X string `url:"x"`
		}{X: s}})
		if err != nil {
			t.Fatal(err)
		}
		_ = v2.Encode()
	})
}
