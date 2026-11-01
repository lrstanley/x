// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package spec

import (
	"strings"
	"testing"
)

func TestEffect(t *testing.T) {
	tests := []struct {
		kind, spec, err string
	}{
		{kind: "highlight", spec: "rows=2-4;dim=0.4;outline=#f5c2e7:3;padding=4;between=a,b;ease=200ms"},
		{kind: "highlight", spec: "row=1;outline=#fff"},
		{kind: "zoom", spec: "re=ERROR.*;scale=2;anchor=0.5,0.5"},
		{kind: "zoom", spec: "cells=0,0,10,2"},
		{kind: "redact", spec: "env=TOKEN;style=blur"},
		{kind: "redact", spec: "text=secret;style=replace:*;between=,done"},
		{kind: "redact", spec: "text=x;style=block"},
		{kind: "blur", spec: "row=1", err: `unknown effect "blur"`},
		{kind: "zoom", spec: "row", err: `region "row" is not KIND=VALUE`},
		{kind: "zoom", spec: "rows=1", err: "want 2 numbers"},
		{kind: "zoom", spec: "cells=1,2,3", err: "want 4 numbers"},
		{kind: "zoom", spec: "re=(", err: "missing closing )"},
		{kind: "zoom", spec: "pixel=1", err: `unknown region "pixel"`},
		{kind: "zoom", spec: "row=1;scale", err: `option "scale" is not KEY=VALUE`},
		{kind: "zoom", spec: "row=1;scale=2;scale=3", err: `option "scale" is repeated`},
		{kind: "zoom", spec: "row=1;dim=2", err: `unknown option "dim"`},
		{kind: "zoom", spec: "row=1;between=a", err: `between: "a" is not FROM,TO`},
		{kind: "redact", spec: "row=1;style=fuzzy", err: `style "fuzzy" is not blur`},
		{kind: "redact", spec: "row=1;ease=1s", err: `unknown option "ease"`},
		{kind: "highlight", spec: "row=1;outline=red", err: "is not a #rrggbb color"},
	}
	for _, tt := range tests {
		t.Run(tt.kind+" "+tt.spec, func(t *testing.T) {
			e, err := Effect(tt.kind, tt.spec)
			if tt.err == "" {
				if err != nil || e == nil {
					t.Fatalf("Effect = %v, %v", e, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Fatalf("error = %v, want one containing %q", err, tt.err)
			}
		})
	}
}
