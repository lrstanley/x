// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"errors"

	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
)

// SearchStatus is how far a [Search] has gotten.
// Values match GhosttySearchStatus.
type SearchStatus int32

// Search progress.
const (
	SearchRunning SearchStatus = iota
	SearchFeedRequired
	SearchComplete
)

// SearchScroll is what selecting a match does to the viewport.
// Values match GhosttySearchScroll.
type SearchScroll int32

// Search scroll policies.
const (
	SearchScrollIfNeeded SearchScroll = iota
	SearchScrollNone
)

// Search finds a byte sequence in the active screen and scrollback.
// Matching is byte-for-byte except ASCII letters, which compare
// case-insensitively. Every method takes the terminal lock: one Wasm
// instance is one memory, so a search cannot run beside a write.
//
// Close the search before [Terminal.Close]. [Terminal.Close] also
// releases searches that are still open.
type Search struct {
	// t is the terminal whose screen and scrollback are searched. The
	// search borrows it and must be closed before the terminal is freed.
	t *Terminal
	// ptr is the GhosttySearch handle. Zero means Close has released it.
	// Calls that need the terminal fail cleanly after the terminal is
	// closed. The handle itself can still be freed.
	ptr int32
}

// NewSearch creates an idle search bound to t.
func NewSearch(t *Terminal) (*Search, error) {
	if t == nil {
		return nil, errors.New("vt: search terminal is nil")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return nil, errClosed
	}
	ptr, err := t.mod.Ctor(func(slot int32) int32 {
		return t.mod.Xghostty_search_new(0, slot, t.term)
	})
	if err != nil {
		return nil, err
	}
	s := &Search{t: t, ptr: ptr}
	t.searches = append(t.searches, s)
	return s, nil
}

// Close releases the search. A second call does nothing.
func (s *Search) Close() {
	if s == nil || s.t == nil {
		return
	}
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	s.closeLocked()
}

func (s *Search) closeLocked() {
	if s.ptr == 0 {
		return
	}
	s.t.mod.Xghostty_search_free(s.ptr)
	s.ptr = 0
	kept := s.t.searches[:0]
	for _, other := range s.t.searches {
		if other != s && other.ptr != 0 {
			kept = append(kept, other)
		}
	}
	s.t.searches = kept
}

func (t *Terminal) freeSearchesLocked() {
	for _, s := range t.searches {
		if s.ptr != 0 {
			t.mod.Xghostty_search_free(s.ptr)
			s.ptr = 0
		}
	}
	t.searches = nil
}

// SetNeedle copies needle and restarts the search. An empty needle
// clears the results. ASCII case does not matter.
func (s *Search) SetNeedle(needle []byte) error {
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	if err := s.ready(); err != nil {
		return err
	}
	if len(needle) == 0 {
		return wasmvt.Result("search needle", s.t.mod.Xghostty_search_set(s.ptr, wasmvt.SearchOptionNeedle, 0))
	}
	buf, err := s.t.mod.Alloc(len(needle))
	if err != nil {
		return err
	}
	defer s.t.mod.Free(buf, len(needle))
	s.t.mod.Put(buf, needle)
	strN := wasmvt.SizeString
	str, err := s.t.mod.Alloc(strN)
	if err != nil {
		return err
	}
	defer s.t.mod.Free(str, strN)
	s.t.mod.StoreString(str, 0, buf, len(needle))
	return wasmvt.Result("search needle", s.t.mod.Xghostty_search_set(s.ptr, wasmvt.SearchOptionNeedle, str))
}

// Run feeds and ticks until the search matches the terminal's current contents.
func (s *Search) Run() error {
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	if err := s.ready(); err != nil {
		return err
	}
	return wasmvt.Result("search run", s.t.mod.Xghostty_search_run(s.ptr))
}

// Feed copies the next portion of terminal state into the search.
func (s *Search) Feed() error {
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	if err := s.ready(); err != nil {
		return err
	}
	return wasmvt.Result("search feed", s.t.mod.Xghostty_search_feed(s.ptr))
}

// Tick does a bounded amount of work on data the search already copied.
func (s *Search) Tick() (SearchStatus, error) {
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	if err := s.ready(); err != nil {
		return 0, err
	}
	out, err := s.t.mod.Alloc(4)
	if err != nil {
		return 0, err
	}
	defer s.t.mod.Free(out, 4)
	if err = wasmvt.Result("search tick", s.t.mod.Xghostty_search_tick(s.ptr, out)); err != nil {
		return 0, err
	}
	return SearchStatus(s.t.mod.I32(out, 0)), nil
}

// Status reports whether the search needs another [Search.Feed].
func (s *Search) Status() (SearchStatus, error) {
	v, err := s.getI32(wasmvt.SearchDataStatus)
	return SearchStatus(v), err
}

// Needle returns a copy of the current needle. An idle search returns [ErrNoValue].
func (s *Search) Needle() ([]byte, error) {
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	if err := s.ready(); err != nil {
		return nil, err
	}
	n := wasmvt.SizeString
	ptr, err := s.t.mod.Alloc(n)
	if err != nil {
		return nil, err
	}
	defer s.t.mod.Free(ptr, n)
	if err = wasmvt.Result("search needle", s.t.mod.Xghostty_search_get(s.ptr, wasmvt.SearchDataNeedle, ptr)); err != nil {
		return nil, err
	}
	return append([]byte(nil), []byte(s.t.mod.String(ptr, 0))...), nil
}

// MatchCount is the number of matches found so far on the active screen.
func (s *Search) MatchCount() (int, error) {
	v, err := s.getU32(wasmvt.SearchDataTotalMatches)
	return int(v), err
}

// SelectedIndex is the selected match in [Search.Matches]. The newest
// match is index 0. No selection returns [ErrNoValue].
func (s *Search) SelectedIndex() (int, error) {
	v, err := s.getU32(wasmvt.SearchDataSelectedIndex)
	return int(v), err
}

// SelectNext selects the next older match, wrapping at the oldest.
// No matches returns [ErrNoValue].
func (s *Search) SelectNext() error {
	return s.setOption(wasmvt.SearchOptionSelectNext, 0)
}

// SelectPrevious selects the next newer match, wrapping at the newest.
func (s *Search) SelectPrevious() error {
	return s.setOption(wasmvt.SearchOptionSelectPrev, 0)
}

// SetScrollPolicy controls whether selecting a match moves the viewport.
func (s *Search) SetScrollPolicy(policy SearchScroll) error {
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	if err := s.ready(); err != nil {
		return err
	}
	ptr, err := s.t.mod.Alloc(4)
	if err != nil {
		return err
	}
	defer s.t.mod.Free(ptr, 4)
	s.t.mod.SetI32(ptr, 0, int32(policy))
	return wasmvt.Result("search scroll", s.t.mod.Xghostty_search_set(s.ptr, wasmvt.SearchOptionSelectScroll, ptr))
}

// ScrollPolicy is the policy used when a match is selected.
func (s *Search) ScrollPolicy() (SearchScroll, error) {
	v, err := s.getI32(wasmvt.SearchDataSelectScroll)
	return SearchScroll(v), err
}

// SelectedMatch is the current match. No selection returns [ErrNoValue].
func (s *Search) SelectedMatch() (Selection, error) {
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	if err := s.ready(); err != nil {
		return Selection{}, err
	}
	n := wasmvt.SizeSelection
	raw, err := s.t.mod.Alloc(n)
	if err != nil {
		return Selection{}, err
	}
	defer s.t.mod.Free(raw, n)
	s.t.mod.Zero(raw, n)
	s.t.mod.SetU32(raw, 0, uint32(n))
	if err = wasmvt.Result("search match", s.t.mod.Xghostty_search_get(s.ptr, wasmvt.SearchDataSelectedMatch, raw)); err != nil {
		return Selection{}, err
	}
	return s.t.selectionFrom(raw)
}

// Matches returns matches on the active screen, newest first.
func (s *Search) Matches() ([]Selection, error) {
	return s.selections(wasmvt.SearchDataMatches)
}

// ViewportMatches returns matches on the pages covering the viewport.
func (s *Search) ViewportMatches() ([]Selection, error) {
	return s.selections(wasmvt.SearchDataViewportMatches)
}

// selections reads a GhosttySelectionBuffer. data is a SearchData
// constant (matches or viewport matches). The first get uses a null
// pointer so OUT_OF_SPACE reports the count. The buffer is then
// pointed at storage for that many GhosttySelection values and read
// again. Each selection is decoded by selectionFrom.
func (s *Search) selections(data int32) ([]Selection, error) {
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	if err := s.ready(); err != nil {
		return nil, err
	}
	bufN := wasmvt.SizeSelectionBuffer
	buf, err := s.t.mod.Alloc(bufN)
	if err != nil {
		return nil, err
	}
	defer s.t.mod.Free(buf, bufN)
	s.t.mod.Zero(buf, bufN)
	rc := s.t.mod.Xghostty_search_get(s.ptr, data, buf)
	outOfSpace := rc == wasmvt.ResultOutOfSpace
	if rc != 0 && !outOfSpace {
		return nil, wasmvt.Result("search matches", rc)
	}
	count := int(s.t.mod.U32(buf, wasmvt.SelectionBufferLenOff))
	if count == 0 {
		return nil, nil
	}
	selN := wasmvt.SizeSelection
	raw, err := s.t.mod.Alloc(count * selN)
	if err != nil {
		return nil, err
	}
	defer s.t.mod.Free(raw, count*selN)
	s.t.mod.SetU32(buf, wasmvt.SelectionBufferPtrOff, uint32(raw))
	s.t.mod.SetU32(buf, wasmvt.SelectionBufferCapOff, uint32(count))
	s.t.mod.SetU32(buf, wasmvt.SelectionBufferLenOff, 0)
	if err = wasmvt.Result("search matches", s.t.mod.Xghostty_search_get(s.ptr, data, buf)); err != nil {
		return nil, err
	}
	n := int(s.t.mod.U32(buf, wasmvt.SelectionBufferLenOff))
	out := make([]Selection, 0, n)
	for i := range n {
		sel, err := s.t.selectionFrom(raw + int32(i*selN))
		if err != nil {
			return nil, err
		}
		out = append(out, sel)
	}
	return out, nil
}

// setOption calls ghostty_search_set. opt is a generated SearchOption
// value. value is the raw pointer argument. 0 clears pointer options
// such as the needle.
func (s *Search) setOption(opt, value int32) error {
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	if err := s.ready(); err != nil {
		return err
	}
	return wasmvt.Result("search set", s.t.mod.Xghostty_search_set(s.ptr, opt, value))
}

func (s *Search) getU32(data int32) (uint32, error) {
	return s.getWord(data)
}

func (s *Search) getI32(data int32) (int32, error) {
	v, err := s.getWord(data)
	return int32(v), err
}

// getWord reads one u32 GhosttySearchData value. getU32 and getI32
// share it. NO_VALUE surfaces as [ErrNoValue] for an idle search or
// a missing selection.
func (s *Search) getWord(data int32) (uint32, error) {
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	if err := s.ready(); err != nil {
		return 0, err
	}
	ptr, err := s.t.mod.Alloc(4)
	if err != nil {
		return 0, err
	}
	defer s.t.mod.Free(ptr, 4)
	if err = wasmvt.Result("search get", s.t.mod.Xghostty_search_get(s.ptr, data, ptr)); err != nil {
		return 0, err
	}
	return s.t.mod.U32(ptr, 0), nil
}

func (s *Search) ready() error {
	if s.ptr == 0 || s.t.idle {
		return errClosed
	}
	return nil
}
