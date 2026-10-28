// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var harnessHookExcluded map[string]bool

func init() {
	harnessHookExcluded = map[string]bool{}
	for _, name := range []string{
		"View", "Title", "LastBellAt", "Bounds", "IsAltScreen", "ModeSetting",
		"Width", "Height", "Dimensions", "IsFocused", "FgColor", "BgColor",
		"CursorColor", "CursorPosition", "CursorVisible", "CursorBlink", "CursorStyle",
		"ProgramStatuses",
		"Progress", "Notifications", "Clipboard", "WorkingDirectory", "Prompt", "ResizeRequest",
		"Scrollback", "ScrollbackCount", "ScrollbackOffset", "MessageHistory", "Messages", "LiveMessages",
		"FinalModel", "FinalModelAs", "mergedOpts", "keyDelay", "requireProgram", "waitStarted", "snapshotOpts",
		"FilterMessagesType", "FilterMessagesFunc",
		"drawImage", "drawImageInto", "imageViewport", "dropImageScreen",
		"check", "waitFinished",
		"LeftClick", "MiddleClick", "RightClick", "LeftDrag",
		"KeyUp", "KeyDown", "KeyLeft", "KeyRight", "KeyEsc", "KeyDelete",
		"KeyBackspace", "KeyCtrlC", "KeyCtrlD",
	} {
		harnessHookExcluded[name] = true
	}
}

func TestHookInstrumentation(t *testing.T) {
	t.Helper()

	files := []string{
		"harness.go",
		"harness_assertions.go",
		"harness_snapshot.go",
		"harness_terminal.go",
	}

	fset := token.NewFileSet()
	for _, file := range files {
		path := filepath.Join(".", file)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatal(err)
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			recv := fn.Recv.List[0].Type
			star, ok := recv.(*ast.StarExpr)
			if !ok {
				continue
			}
			ident, ok := star.X.(*ast.Ident)
			if !ok || ident.Name != "Harness" {
				continue
			}
			if harnessHookExcluded[fn.Name.Name] {
				continue
			}

			start := fset.Position(fn.Body.Pos()).Offset
			end := fset.Position(fn.Body.End()).Offset
			body := string(src[start:end])
			if !strings.Contains(body, "defer h.hook(") {
				t.Errorf("%s: *Harness.%s missing defer h.hook", file, fn.Name.Name)
			}
		}
	}
}

func TestKeyDownSingleHook(t *testing.T) {
	var count int
	h := NewHarness(t, rootTestModel{}, WithHooks(func(event HookEvent, _ HookStage, _ any) error {
		if event == HookInput {
			count++
		}
		return nil
	}), WithIgnoreEnv(true))
	h.KeyDown()
	if count != 2 {
		t.Fatalf("KeyDown: got %d input hook stages, want 2 (before+after)", count)
	}
}
