// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command genabi writes abi.gen.go from the running libghostty-vt module.
//
// Run it from the vt directory, after vt.generated.go exists:
//
//	go run ./cmd/genabi
//
// The generator imports internal/wasmvt and calls ghostty_type_json.
// abi.gen.go is constants only, so the package does not import this
// command and there is no cycle. vt.generated.go has to be built first.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"go/format"
	"os"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
)

const outPath = "internal/wasmvt/abi.gen.go"

func main() {
	raw := typeJSON(wasmvt.New())
	sum := sha256.Sum256(raw)
	var doc manifest
	if err := json.Unmarshal(raw, &doc); err != nil {
		fatal(err)
	}
	if doc.Schema != 1 {
		fatal(fmt.Errorf("unsupported ghostty_type_json schema %d", doc.Schema))
	}
	if doc.ABI.Endian != "little" || doc.ABI.PointerSize != 4 || doc.ABI.UsizeSize != 4 {
		fatal(fmt.Errorf("abi is %s pointer %d usize %d, generator expects wasm32 little-endian", doc.ABI.Endian, doc.ABI.PointerSize, doc.ABI.UsizeSize))
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, `%s

package wasmvt

// Layout of the libghostty-vt C ABI baked into this module.
//
// Source is ghostty_type_json (include/ghostty/vt/types.h), library
// %s. ABIHash is the sha256 of that document. Regenerate from vt
// with "go run ./cmd/genabi" after rebuilding vt.generated.go. These
// are compile-time constants, so the host does not parse the manifest
// at runtime.
const (
	ABISchema         = %d
	ABILibraryVersion = %q
	ABIHash           = %q
	ABIPointerSize    = %d
	ABIUsizeSize      = %d
	ABIMaxAlignment   = %d
)
`, header, doc.LibraryVersion, doc.Schema, doc.LibraryVersion, hex.EncodeToString(sum[:]), doc.ABI.PointerSize, doc.ABI.UsizeSize, doc.ABI.MaxAlign)

	names := make([]string, 0, len(doc.Types))
	for name := range doc.Types {
		names = append(names, name)
	}
	slices.Sort(names)

	taken := map[string]string{}
	var sizes, fields, enums bytes.Buffer
	var keyCases []string
	for _, name := range names {
		desc := doc.Types[name]
		if desc.Kind == "enum" && desc.Underlying != "" && desc.Underlying != "i32" {
			fatal(fmt.Errorf("%s underlying %s, want i32", name, desc.Underlying))
		}
		sizeName := claim(taken, name+"Size", "Size"+goIdent(name))
		fmt.Fprintf(&sizes, "\t%s = %d\n", sizeName, desc.Size)

		fieldNames := make([]string, 0, len(desc.Fields))
		for field := range desc.Fields {
			fieldNames = append(fieldNames, field)
		}
		slices.Sort(fieldNames)
		for _, field := range fieldNames {
			info := desc.Fields[field]
			offName := claim(taken, name+"."+field+".off", goIdent(name, field)+"Off")
			fmt.Fprintf(&fields, "\t%s = %d\n", offName, info.Offset)
		}

		valueNames := make([]string, 0, len(desc.Values))
		for value := range desc.Values {
			valueNames = append(valueNames, value)
		}
		slices.Sort(valueNames)
		for _, value := range valueNames {
			ident := claim(taken, name+"."+value, goIdent(name, value))
			fmt.Fprintf(&enums, "\t%s int32 = %d\n", ident, desc.Values[value])
			if name == "GhosttyKey" {
				keyCases = append(keyCases, fmt.Sprintf("\tcase %q:\n\t\treturn %s\n", value, ident))
			}
		}
	}

	buf.WriteString("\nconst (\n")
	buf.Write(sizes.Bytes())
	buf.WriteString(")\n\nconst (\n")
	buf.Write(fields.Bytes())
	buf.WriteString(")\n\nconst (\n")
	buf.Write(enums.Bytes())
	buf.WriteString(")\n\n")
	buf.WriteString(`// KeyByName returns the GhosttyKey enum value for a manifest name
// such as "ENTER". An unknown name panics. Key codes arrive as strings
// from the caller, so this is the one enum that stays a lookup.
func KeyByName(name string) int32 {
	switch name {
`)
	for _, line := range keyCases {
		buf.WriteString(line)
	}
	buf.WriteString("\tdefault:\n\t\tpanic(\"vt: missing enum GhosttyKey.\" + name)\n\t}\n}\n")

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		fatal(err)
	}
	if err = os.WriteFile(outPath, formatted, 0o644); err != nil { //nolint:gosec // G306: generated source is not a secret
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d bytes, %d types)\n", outPath, len(formatted), len(names))
}

func typeJSON(m *wasmvt.Module) []byte {
	ptr := m.Xghostty_type_json()
	if ptr == 0 {
		fatal(errors.New("ghostty_type_json returned null"))
	}
	mem := m.Memory()
	start := int(uint32(ptr)) //nolint:gosec // G115: wasm byte offset
	end := start
	for end < len(mem) && mem[end] != 0 {
		end++
	}
	return append([]byte(nil), mem[start:end]...)
}

type manifest struct {
	Schema         int    `json:"schema"`
	LibraryVersion string `json:"library_version"`
	ABI            struct {
		Endian      string `json:"endian"`
		PointerSize int    `json:"pointer_size"`
		UsizeSize   int    `json:"usize_size"`
		MaxAlign    int    `json:"max_alignment"`
	} `json:"abi"`
	Types map[string]typeDesc `json:"types"`
}

type typeDesc struct {
	Kind       string               `json:"kind"`
	Size       int                  `json:"size"`
	Underlying string               `json:"underlying"`
	Fields     map[string]fieldDesc `json:"fields"`
	Values     map[string]int32     `json:"values"`
}

type fieldDesc struct {
	Offset int `json:"offset"`
	Size   int `json:"size"`
}

func claim(taken map[string]string, key, ident string) string {
	if !identOK(ident) {
		fatal(fmt.Errorf("invalid go name %q for %s", ident, key))
	}
	if prev, ok := taken[ident]; ok {
		fatal(fmt.Errorf("name %s used by %s and %s", ident, prev, key))
	}
	taken[ident] = key
	return ident
}

func identOK(name string) bool {
	if name == "" || keyword(name) {
		return false
	}
	r, w := utf8.DecodeRuneInString(name)
	if !unicode.IsLetter(r) {
		return false
	}
	for _, r := range name[w:] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func keyword(name string) bool {
	switch name {
	case "break", "default", "func", "interface", "select", "case", "defer", "go", "map", "struct",
		"chan", "else", "goto", "package", "switch", "const", "fallthrough", "if", "range", "type",
		"continue", "for", "import", "return", "var":
		return true
	default:
		return false
	}
}

// goIdent joins a Ghostty type name with a field or enum value.
// The Ghostty prefix is dropped. GhosttyString + len → StringLen.
// NO_VALUE → NoValue. Type sizes use a Size prefix: SizeString.
func goIdent(parts ...string) string {
	var b strings.Builder
	for i, part := range parts {
		if i == 0 {
			part = strings.TrimPrefix(part, "Ghostty")
			b.WriteString(part)
			continue
		}
		for word := range strings.SplitSeq(part, "_") {
			if word == "" {
				continue
			}
			b.WriteString(camel(word))
		}
	}
	return b.String()
}

func camel(word string) string {
	lower := strings.ToLower(word)
	r, size := utf8.DecodeRuneInString(lower)
	return string(unicode.ToUpper(r)) + lower[size:]
}

const header = `// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.
//
// Code generated by cmd/genabi. DO NOT EDIT.`

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "genabi: %s\n", err)
	os.Exit(1)
}
