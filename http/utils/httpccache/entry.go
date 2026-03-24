// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package httpccache

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"
)

// CacheEntry is HTTP cache metadata for a stored response. Body bytes live in
// [Storage] beside JSON produced by [CacheEntry.marshalWire].
//
// BodySize is set by the transport after [Storage.Get] (not part of persisted
// metadata) for Content-Length on synthesized responses.
//
// ResponseHeader holds the full upstream response headers (including ETag,
// Last-Modified, Vary). VaryValues records the request header values that were
// sent when the response was cached; they are not derivable from
// ResponseHeader and are required for Vary matching.
type CacheEntry struct {
	Key    string `json:"key"`
	Method string `json:"method"`
	URL    string `json:"url"`

	StoredAt  time.Time `json:"stored_at"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`

	ResponseStatus int         `json:"response_status"`
	ResponseHeader http.Header `json:"response_header"`
	BodySize       int64       `json:"-"` // Set by the storage implementation during Get.

	VaryValues map[string]string `json:"vary_values"`
}

// toResponse converts a cache entry into an [http.Response]. body is the
// cached response body stream; caller may pass nil for an empty body.
func (e *CacheEntry) toResponse(req *http.Request, body io.ReadCloser) *http.Response {
	if body == nil {
		body = io.NopCloser(bytes.NewReader(nil))
	}
	return &http.Response{
		StatusCode:    e.ResponseStatus,
		Status:        strconv.Itoa(e.ResponseStatus) + " " + http.StatusText(e.ResponseStatus),
		Header:        e.ResponseHeader.Clone(),
		Body:          body,
		ContentLength: e.BodySize,
		Request:       req,
	}
}

func unmarshalCacheEntryJSON(data []byte) (*CacheEntry, error) {
	var e CacheEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	if e.ResponseHeader == nil {
		e.ResponseHeader = make(http.Header)
	}
	if e.VaryValues == nil {
		e.VaryValues = make(map[string]string)
	}
	return &e, nil
}
