// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestKittyDirectImage(t *testing.T) {
	t.Parallel()
	term := openTest(t, 10, 4)
	rgba := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	payload := base64.StdEncoding.EncodeToString(rgba)
	seq := "\x1b_Ga=T,i=1,f=32,t=d,s=2,v=2,c=2,r=1,z=-1;" + payload + "\x1b\\"
	if _, err := term.Write([]byte(seq)); err != nil {
		t.Fatal(err)
	}
	if term.KittyImages() != nil {
		t.Fatal("image stored while the storage limit is zero")
	}
	if err := term.SetKittyImageStorageLimit(64); err != nil {
		t.Fatal(err)
	}
	if _, err := term.Write([]byte(seq)); err != nil {
		t.Fatal(err)
	}
	img, ok := term.KittyImage(1)
	if !ok {
		t.Fatal("missing image 1")
	}
	if img.Width != 2 || img.Height != 2 || img.Format != KittyImageRGBA || !bytes.Equal(img.Data, rgba) {
		t.Fatalf("image = %+v data %v", img, img.Data)
	}
	places := term.KittyPlacements()
	if len(places) != 1 || places[0].ImageID != 1 || places[0].Columns != 2 || places[0].Rows != 1 || places[0].Z != -1 {
		t.Fatalf("placements = %+v", places)
	}
	if _, err := term.Write([]byte("\x1b_Ga=d,d=I,i=1\x1b\\")); err != nil {
		t.Fatal(err)
	}
	if _, stillThere := term.KittyImage(1); stillThere {
		t.Fatal("image survived delete")
	}
	if term.KittyPlacements() != nil {
		t.Fatal("placement survived delete")
	}
}
