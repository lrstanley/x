// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package tape parses the tapes of VHS (https://github.com/charmbracelet/vhs)
// into commands, and leaves what they do to the caller, so a tape can drive
// any program that reads one, with commands of its own.
//
// The lexer follows VHS's. Commands and their arguments are separated by
// whitespace, newlines included, and "#" starts a comment to the end of the
// line. A token is one of:
//
//   - a word: a command or setting name, or an unquoted string;
//   - a string, quoted with ", ', or ` and ended on the same line;
//   - a number, with an optional unit right after it, such as 500ms;
//   - a regular expression between slashes, such as /\$ $/;
//   - JSON between braces, up to the first "}";
//   - punctuation: one of @+=%[]-^\.
//
// [Parse] groups the tokens into [Command] values:
//
//	tape    = { command } .
//	command = name { "+" token } [ "@" token ] { argument } .
//	name    = a word that names a command .
//
// An argument is any token but a command name, so a command's arguments
// run to the next command, wherever it is on the line. The command names
// are those of [VHSCommands], or the [Commands] of [WithCommands], which
// may add names, and [Skip] or [Fail] any: [WithReport] tells the caller
// of each. Positions are given as [Pos], and errors as [*Error].
package tape
