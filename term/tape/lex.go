// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package tape

import (
	"errors"
	"fmt"
	"strings"
)

// Pos is a position in a tape file.
type Pos struct {
	// File is the tape's path, or empty for one read without a name.
	File string
	// Line and Col count from 1; Col counts bytes.
	Line, Col int
}

func (p Pos) String() string {
	if p.File == "" {
		return fmt.Sprintf("%d:%d", p.Line, p.Col)
	}
	return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col)
}

// Error is an error at a position in a tape.
type Error struct {
	Pos Pos
	Err error
}

func (e *Error) Error() string { return "tape: " + e.Pos.String() + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Kind is a [Token]'s kind.
type Kind int

const (
	eof Kind = iota
	// Word is a bare word, such as a command name, or an unquoted string.
	Word
	// String is a string quoted with ", ', or `.
	String
	// Number is a number with an optional unit written right after it, such
	// as 3, .5, 500ms, or 22px.
	Number
	// Regex is a regular expression between slashes.
	Regex
	// JSON is an object between braces.
	JSON
	// Punct is one of "@+=%[]-^\".
	Punct
)

// Token is a token of a tape.
type Token struct {
	Kind Kind
	// Text is the token as written, without the quotes, slashes, or braces
	// around a String, Regex, or JSON.
	Text string
	Pos  Pos
}

// lex splits src into tokens as VHS's lexer does: newlines are whitespace
// like any other, "#" starts a comment to the end of the line, and strings
// end at their quote or the end of the line. The last token is eof.
func lex(file, src string) ([]Token, []error) {
	var (
		toks []Token
		errs []error
	)
	line, lineStart := 1, 0
	for i := 0; i < len(src); {
		c := src[i]
		pos := Pos{File: file, Line: line, Col: i - lineStart + 1}
		// end is the index of the end of the line.
		end := strings.IndexAny(src[i:], "\r\n")
		if end < 0 {
			end = len(src)
		} else {
			end += i
		}
		switch {
		case c == '\n':
			line, lineStart = line+1, i+1
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '#':
			i = end
		case strings.IndexByte(`"'`+"`", c) >= 0:
			j := strings.IndexByte(src[i+1:end], c)
			if j < 0 {
				errs = append(errs, &Error{Pos: pos, Err: errors.New("unterminated string")})
				i = end
				continue
			}
			toks = append(toks, Token{Kind: String, Text: src[i+1 : i+1+j], Pos: pos})
			i += j + 2
		case c == '/':
			j := regexEnd(src[i+1 : end])
			if j < 0 {
				errs = append(errs, &Error{Pos: pos, Err: errors.New("unterminated regular expression")})
				i = end
				continue
			}
			toks = append(toks, Token{Kind: Regex, Text: src[i+1 : i+1+j], Pos: pos})
			i += j + 2
		case c == '{':
			// As VHS reads it: to the first "}", across lines.
			j := strings.IndexByte(src[i:], '}')
			if j < 0 {
				errs = append(errs, &Error{Pos: pos, Err: errors.New("unterminated JSON")})
				i = len(src)
				continue
			}
			text := src[i : i+j+1]
			toks = append(toks, Token{Kind: JSON, Text: text, Pos: pos})
			if n := strings.Count(text, "\n"); n > 0 {
				line, lineStart = line+n, i+strings.LastIndexByte(text, '\n')+1
			}
			i += j + 1
		case isDigit(c) || c == '.' && i+1 < len(src) && isDigit(src[i+1]):
			j := i
			for j < len(src) && (isDigit(src[j]) || src[j] == '.') {
				j++
			}
			for j < len(src) && (isLetter(src[j]) || src[j] == '%') {
				j++
			}
			toks = append(toks, Token{Kind: Number, Text: src[i:j], Pos: pos})
			i = j
		case isLetter(c) || c == '.':
			j := i
			for j < len(src) && (isLetter(src[j]) || isDigit(src[j]) || strings.IndexByte("._-/%", src[j]) >= 0) {
				j++
			}
			toks = append(toks, Token{Kind: Word, Text: src[i:j], Pos: pos})
			i = j
		case strings.IndexByte(`@+=%[]-^\`, c) >= 0:
			toks = append(toks, Token{Kind: Punct, Text: src[i : i+1], Pos: pos})
			i++
		default:
			errs = append(errs, &Error{Pos: pos, Err: fmt.Errorf("unexpected character %q", c)})
			i++
		}
	}
	toks = append(toks, Token{Kind: eof, Pos: Pos{File: file, Line: line, Col: len(src) - lineStart + 1}})
	return toks, errs
}

// regexEnd returns the index of the "/" that ends the regular expression s
// starts, skipping those escaped with an odd number of backslashes, or -1.
func regexEnd(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '/':
			return i
		}
	}
	return -1
}

func isDigit(c byte) bool  { return '0' <= c && c <= '9' }
func isLetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }
