// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"strings"
	"testing"
)

func TestNextVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tags    []string
		bump    string
		channel string
		want    string
		wantErr string
	}{
		{name: "patch", tags: []string{"v1.2.0"}, bump: "patch", want: "v1.2.1"},
		{name: "minor", tags: []string{"v1.2.3"}, bump: "minor", want: "v1.3.0"},
		{name: "major", tags: []string{"v1.2.3"}, bump: "major", want: "v2.0.0"},
		{name: "major from v0", tags: []string{"v0.6.0"}, bump: "major", want: "v1.0.0"},

		{name: "minor starts a beta", tags: []string{"v1.2.0"}, bump: "minor", channel: "beta", want: "v1.3.0-beta.0"},
		{name: "minor counts up", tags: []string{"v1.3.0-beta.0"}, bump: "minor", channel: "beta", want: "v1.3.0-beta.1"},
		{name: "moves to rc", tags: []string{"v1.3.0-beta.1"}, bump: "minor", channel: "rc", want: "v1.3.0-rc.0"},
		{name: "minor finishes", tags: []string{"v1.3.0-rc.0"}, bump: "minor", want: "v1.3.0"},
		{name: "patch finishes", tags: []string{"v1.3.0-rc.0"}, bump: "patch", want: "v1.3.0"},
		{name: "major finishes", tags: []string{"v2.0.0-rc.0"}, bump: "major", want: "v2.0.0"},
		{name: "patch counts up", tags: []string{"v1.3.1-beta.0"}, bump: "patch", channel: "beta", want: "v1.3.1-beta.1"},
		{name: "chix continues", tags: []string{"v2.0.0-beta.11"}, bump: "major", channel: "beta", want: "v2.0.0-beta.12"},
		{name: "number follows highest tag", tags: []string{"v1.3.0-beta.9", "v1.3.0-beta.10"}, bump: "minor", channel: "beta", want: "v1.3.0-beta.11"},
		{name: "channel restart after rc is refused", tags: []string{"v1.3.0-rc.0", "v1.3.0-beta.4"}, bump: "minor", channel: "beta", wantErr: "not newer"},

		{name: "minor moves past a patch prerelease", tags: []string{"v1.3.1-beta.0"}, bump: "minor", want: "v1.4.0"},
		{name: "major moves past a minor prerelease", tags: []string{"v1.3.0-beta.1"}, bump: "major", channel: "beta", want: "v2.0.0-beta.0"},
		{name: "patch prerelease of a release", tags: []string{"v1.3.0"}, bump: "patch", channel: "alpha", want: "v1.3.1-alpha.0"},

		{name: "alpha after rc is refused", tags: []string{"v1.3.0-rc.0"}, bump: "minor", channel: "alpha", wantErr: "not newer"},
		{name: "keyword needs a tag", bump: "patch", wantErr: "explicit version"},
		{name: "channel needs a tag", bump: "minor", channel: "beta", wantErr: "explicit version"},

		{name: "explicit first release", bump: "v0.1.0", want: "v0.1.0"},
		{name: "explicit prerelease", tags: []string{"v1.0.0"}, bump: "v1.1.0-pre.3", want: "v1.1.0-pre.3"},
		{name: "explicit must be newer", tags: []string{"v1.1.0"}, bump: "v1.0.0", wantErr: "not newer"},
		{name: "explicit equal is refused", tags: []string{"v1.1.0"}, bump: "v1.1.0", wantErr: "not newer"},

		{name: "highest root tag wins", tags: []string{"v1.0.0", "v1.10.0", "v1.9.0"}, bump: "patch", want: "v1.10.1"},
		{name: "sub-module tags are ignored", tags: []string{"xmetrics/v9.0.0", "v1.0.0"}, bump: "patch", want: "v1.0.1"},
		{name: "other tags are ignored", tags: []string{"nightly", "v1", "v1.0.0"}, bump: "patch", want: "v1.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := nextVersion(tt.tags, tt.bump, tt.channel)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got version %q and error %v, want error containing %q", got, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFlagsValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		flags   Flags
		wantErr string
	}{
		{name: "keyword", flags: Flags{Bump: "patch"}},
		{name: "keyword with channel", flags: Flags{Bump: "minor", Beta: true}},
		{name: "explicit", flags: Flags{Bump: "v1.2.3-beta.1"}},
		{name: "explicit with channel", flags: Flags{Bump: "v1.0.0", RC: true}, wantErr: "can't be combined"},
		{name: "missing v", flags: Flags{Bump: "1.2.3"}, wantErr: "canonical"},
		{name: "shortened", flags: Flags{Bump: "v1.2"}, wantErr: "canonical"},
		{name: "build metadata", flags: Flags{Bump: "v1.2.3+meta"}, wantErr: "canonical"},
		{name: "unknown word", flags: Flags{Bump: "next"}, wantErr: "canonical"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.flags.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}
