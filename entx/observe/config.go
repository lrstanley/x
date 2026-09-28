// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package observe

// Config configures the observe codegen extension.
//
// Nodes is an optional allow-list of schema type names to observe. An empty
// list (or a nil config) means every mutable schema with a single-field ID.
type Config struct {
	Nodes []string
}
