// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package formatter

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"strings"
)

// ToJSON will convert the provided data value into JSON. If mask is true, all
// concrete values will be masked with asterisks. A positive indent emits
// multiline JSON indented with that many spaces.
func ToJSON(data any, mask bool, indent int) string {
	// MaskValue turns structs into maps. Deterministic sorts those keys so the
	// formatted output is stable.
	opts := []json.Options{json.Deterministic(true)}
	if indent > 0 {
		opts = append(opts, jsontext.WithIndent(strings.Repeat(" ", indent)))
	}
	if !mask {
		b, err := json.Marshal(data, opts...)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return string(b)
	}

	if data == nil {
		return "null"
	}

	masked := MaskValue(data)
	b, err := json.Marshal(masked, opts...)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return string(b)
}
