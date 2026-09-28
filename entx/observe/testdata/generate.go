// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build ignore

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"

	"github.com/lrstanley/x/entx/observe"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	entDir := filepath.Join(root, "ent")
	if err := os.RemoveAll(entDir); err != nil {
		fmt.Fprintf(os.Stderr, "remove ent: %v\n", err)
		os.Exit(1)
	}
	if err := entc.Generate(
		filepath.Join(root, "schema"),
		&gen.Config{
			Target:  entDir,
			Package: "github.com/lrstanley/x/entx/observe/testdata/ent",
		},
		entc.Extensions(observe.New(nil)),
	); err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		os.Exit(1)
	}
}
