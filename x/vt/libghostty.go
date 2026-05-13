// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package vt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"github.com/go-webgpu/goffi/ffi"
	"github.com/go-webgpu/goffi/types"
)

type ghostLib struct {
	mu sync.Mutex

	handle unsafe.Pointer

	symTerminalNew         unsafe.Pointer
	symTerminalFree        unsafe.Pointer
	symTerminalVtWrite     unsafe.Pointer
	symTerminalResize      unsafe.Pointer
	symTerminalGet         unsafe.Pointer
	symTerminalGridRef     unsafe.Pointer
	symGridRefCell         unsafe.Pointer
	symGridRefGraphemes    unsafe.Pointer
	symGridRefHyperlinkURI unsafe.Pointer
	symGridRefStyle        unsafe.Pointer
	symCellGet             unsafe.Pointer
	symStyleDefault        unsafe.Pointer
	symStyleIsDefault      unsafe.Pointer

	cifTerminalNew         types.CallInterface
	cifTerminalFree        types.CallInterface
	cifTerminalVtWrite     types.CallInterface
	cifTerminalResize      types.CallInterface
	cifTerminalGet         types.CallInterface
	cifTerminalGridRef     types.CallInterface
	cifGridRefCell         types.CallInterface
	cifGridRefGraphemes    types.CallInterface
	cifGridRefHyperlinkURI types.CallInterface
	cifGridRefStyle        types.CallInterface
	cifCellGet             types.CallInterface
	cifStyleDefault        types.CallInterface
	cifStyleIsDefault      types.CallInterface
}

var (
	libOnce sync.Once
	libInst *ghostLib
	libErr  error
)

func ghostLibSingleton() (*ghostLib, error) {
	libOnce.Do(func() {
		libInst, libErr = loadGhostLib()
	})
	return libInst, libErr
}

func loadGhostLib() (*ghostLib, error) {
	candidates := libraryCandidates()
	var handle unsafe.Pointer
	var loaded string
	var lastErr error
	for _, path := range candidates {
		if path == "" {
			continue
		}
		h, err := ffi.LoadLibrary(path)
		if err != nil {
			lastErr = err
			continue
		}
		handle = h
		loaded = path
		break
	}
	if handle == nil {
		if lastErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrUnavailable, lastErr)
		}
		return nil, ErrUnavailable
	}

	g := &ghostLib{handle: handle}
	resolve := func(name string, out *unsafe.Pointer) error {
		sym, err := ffi.GetSymbol(handle, name)
		if err != nil {
			return fmt.Errorf("vt: missing symbol %q in %s: %w", name, loaded, err)
		}
		*out = sym
		return nil
	}

	if err := resolve("ghostty_terminal_new", &g.symTerminalNew); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := resolve("ghostty_terminal_free", &g.symTerminalFree); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := resolve("ghostty_terminal_vt_write", &g.symTerminalVtWrite); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := resolve("ghostty_terminal_resize", &g.symTerminalResize); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := resolve("ghostty_terminal_get", &g.symTerminalGet); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := resolve("ghostty_terminal_grid_ref", &g.symTerminalGridRef); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := resolve("ghostty_grid_ref_cell", &g.symGridRefCell); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := resolve("ghostty_grid_ref_graphemes", &g.symGridRefGraphemes); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := resolve("ghostty_grid_ref_hyperlink_uri", &g.symGridRefHyperlinkURI); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := resolve("ghostty_grid_ref_style", &g.symGridRefStyle); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := resolve("ghostty_cell_get", &g.symCellGet); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := resolve("ghostty_style_default", &g.symStyleDefault); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := resolve("ghostty_style_is_default", &g.symStyleIsDefault); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}

	termOptDesc := &types.TypeDescriptor{
		Kind: types.StructType,
		Members: []*types.TypeDescriptor{
			types.UInt16TypeDescriptor,
			types.UInt16TypeDescriptor,
			types.UInt64TypeDescriptor,
		},
	}
	pointArgDesc := &types.TypeDescriptor{
		Kind: types.StructType,
		Members: []*types.TypeDescriptor{
			types.SInt32TypeDescriptor,
			types.UInt32TypeDescriptor,
			{
				Kind: types.StructType,
				Members: []*types.TypeDescriptor{
					types.UInt64TypeDescriptor,
					types.UInt64TypeDescriptor,
				},
			},
		},
	}

	if err := ffi.PrepareCallInterface(&g.cifTerminalNew, types.DefaultCall, types.SInt32TypeDescriptor, []*types.TypeDescriptor{
		types.PointerTypeDescriptor,
		types.PointerTypeDescriptor,
		termOptDesc,
	}); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := ffi.PrepareCallInterface(&g.cifTerminalFree, types.DefaultCall, types.VoidTypeDescriptor, []*types.TypeDescriptor{
		types.PointerTypeDescriptor,
	}); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := ffi.PrepareCallInterface(&g.cifTerminalVtWrite, types.DefaultCall, types.VoidTypeDescriptor, []*types.TypeDescriptor{
		types.PointerTypeDescriptor,
		types.PointerTypeDescriptor,
		types.UInt64TypeDescriptor,
	}); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := ffi.PrepareCallInterface(&g.cifTerminalResize, types.DefaultCall, types.SInt32TypeDescriptor, []*types.TypeDescriptor{
		types.PointerTypeDescriptor,
		types.UInt16TypeDescriptor,
		types.UInt16TypeDescriptor,
		types.UInt32TypeDescriptor,
		types.UInt32TypeDescriptor,
	}); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := ffi.PrepareCallInterface(&g.cifTerminalGet, types.DefaultCall, types.SInt32TypeDescriptor, []*types.TypeDescriptor{
		types.PointerTypeDescriptor,
		types.SInt32TypeDescriptor,
		types.PointerTypeDescriptor,
	}); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := ffi.PrepareCallInterface(&g.cifTerminalGridRef, types.DefaultCall, types.SInt32TypeDescriptor, []*types.TypeDescriptor{
		types.PointerTypeDescriptor,
		pointArgDesc,
		types.PointerTypeDescriptor,
	}); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := ffi.PrepareCallInterface(&g.cifGridRefCell, types.DefaultCall, types.SInt32TypeDescriptor, []*types.TypeDescriptor{
		types.PointerTypeDescriptor,
		types.PointerTypeDescriptor,
	}); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := ffi.PrepareCallInterface(&g.cifGridRefGraphemes, types.DefaultCall, types.SInt32TypeDescriptor, []*types.TypeDescriptor{
		types.PointerTypeDescriptor,
		types.PointerTypeDescriptor,
		types.UInt64TypeDescriptor,
		types.PointerTypeDescriptor,
	}); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := ffi.PrepareCallInterface(&g.cifGridRefHyperlinkURI, types.DefaultCall, types.SInt32TypeDescriptor, []*types.TypeDescriptor{
		types.PointerTypeDescriptor,
		types.PointerTypeDescriptor,
		types.UInt64TypeDescriptor,
		types.PointerTypeDescriptor,
	}); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := ffi.PrepareCallInterface(&g.cifGridRefStyle, types.DefaultCall, types.SInt32TypeDescriptor, []*types.TypeDescriptor{
		types.PointerTypeDescriptor,
		types.PointerTypeDescriptor,
	}); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := ffi.PrepareCallInterface(&g.cifCellGet, types.DefaultCall, types.SInt32TypeDescriptor, []*types.TypeDescriptor{
		types.UInt64TypeDescriptor,
		types.SInt32TypeDescriptor,
		types.PointerTypeDescriptor,
	}); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := ffi.PrepareCallInterface(&g.cifStyleDefault, types.DefaultCall, types.VoidTypeDescriptor, []*types.TypeDescriptor{
		types.PointerTypeDescriptor,
	}); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}
	if err := ffi.PrepareCallInterface(&g.cifStyleIsDefault, types.DefaultCall, types.UInt8TypeDescriptor, []*types.TypeDescriptor{
		types.PointerTypeDescriptor,
	}); err != nil {
		_ = ffi.FreeLibrary(handle)
		return nil, err
	}

	_ = loaded
	return g, nil
}

func libraryCandidates() []string {
	if p := os.Getenv("LIBGHOSTTY_VT"); p != "" {
		return []string{p}
	}
	switch runtime.GOOS {
	case "linux":
		return []string{
			"libghostty-vt.so.0",
			"libghostty-vt.so",
		}
	case "darwin":
		home, _ := os.UserHomeDir()
		return []string{
			filepath.Join(home, ".local/lib/libghostty-vt.dylib"),
			"/usr/local/lib/libghostty-vt.dylib",
			"/opt/homebrew/lib/libghostty-vt.dylib",
			"libghostty-vt.dylib",
		}
	case "windows":
		return []string{"ghostty-vt.dll"}
	default:
		return []string{"libghostty-vt.so"}
	}
}

func (g *ghostLib) terminalNew(opts ghosttyTerminalOptions) (uintptr, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	var out uintptr
	var res int32
	err := ffi.CallFunctionContext(context.Background(), &g.cifTerminalNew, g.symTerminalNew, unsafe.Pointer(&res), []unsafe.Pointer{
		nil,
		unsafe.Pointer(&out),
		unsafe.Pointer(&opts),
	})
	if err != nil {
		return 0, err
	}
	if res != ghosttySuccess {
		return 0, ghosttyErr(res)
	}
	if out == 0 {
		return 0, fmt.Errorf("%w: ghostty_terminal_new returned null handle", ErrUnavailable)
	}
	return out, nil
}

func (g *ghostLib) terminalFree(t uintptr) {
	if t == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	tp := t
	_ = ffi.CallFunctionContext(context.Background(), &g.cifTerminalFree, g.symTerminalFree, nil, []unsafe.Pointer{
		unsafe.Pointer(&tp),
	})
}

func (g *ghostLib) terminalVtWrite(t uintptr, p []byte) error {
	if len(p) == 0 {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	tp := t
	n := uint64(len(p))
	err := ffi.CallFunctionContext(context.Background(), &g.cifTerminalVtWrite, g.symTerminalVtWrite, nil, []unsafe.Pointer{
		unsafe.Pointer(&tp),
		unsafe.Pointer(unsafe.SliceData(p)),
		unsafe.Pointer(&n),
	})
	runtime.KeepAlive(p)
	return err
}

func (g *ghostLib) terminalResize(t uintptr, cols, rows uint16, cellW, cellH uint32) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	tp := t
	var res int32
	err := ffi.CallFunctionContext(context.Background(), &g.cifTerminalResize, g.symTerminalResize, unsafe.Pointer(&res), []unsafe.Pointer{
		unsafe.Pointer(&tp),
		unsafe.Pointer(&cols),
		unsafe.Pointer(&rows),
		unsafe.Pointer(&cellW),
		unsafe.Pointer(&cellH),
	})
	if err != nil {
		return err
	}
	return ghosttyErr(res)
}

func (g *ghostLib) terminalGet(t uintptr, key int32, dst unsafe.Pointer) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	tp := t
	k := key
	var res int32
	err := ffi.CallFunctionContext(context.Background(), &g.cifTerminalGet, g.symTerminalGet, unsafe.Pointer(&res), []unsafe.Pointer{
		unsafe.Pointer(&tp),
		unsafe.Pointer(&k),
		dst,
	})
	if err != nil {
		return err
	}
	return ghosttyErr(res)
}

func (g *ghostLib) terminalGridRef(t uintptr, pt ghosttyPoint, out *ghosttyGridRef) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	tp := t
	var res int32
	err := ffi.CallFunctionContext(context.Background(), &g.cifTerminalGridRef, g.symTerminalGridRef, unsafe.Pointer(&res), []unsafe.Pointer{
		unsafe.Pointer(&tp),
		unsafe.Pointer(&pt),
		unsafe.Pointer(out),
	})
	if err != nil {
		return err
	}
	return ghosttyErr(res)
}

func (g *ghostLib) gridRefCell(ref *ghosttyGridRef, out *uint64) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	var res int32
	err := ffi.CallFunctionContext(context.Background(), &g.cifGridRefCell, g.symGridRefCell, unsafe.Pointer(&res), []unsafe.Pointer{
		unsafe.Pointer(ref),
		unsafe.Pointer(out),
	})
	if err != nil {
		return err
	}
	return ghosttyErr(res)
}

func (g *ghostLib) gridRefGraphemes(ref *ghosttyGridRef, buf []uint32) (needed int, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var outLen uint64
	var res int32
	var bufPtr unsafe.Pointer
	bufLen := uint64(len(buf))
	if len(buf) > 0 {
		bufPtr = unsafe.Pointer(unsafe.SliceData(buf))
	}
	callErr := ffi.CallFunctionContext(context.Background(), &g.cifGridRefGraphemes, g.symGridRefGraphemes, unsafe.Pointer(&res), []unsafe.Pointer{
		unsafe.Pointer(ref),
		bufPtr,
		unsafe.Pointer(&bufLen),
		unsafe.Pointer(&outLen),
	})
	runtime.KeepAlive(buf)
	if callErr != nil {
		return 0, callErr
	}
	if res == ghosttyOutOfSpace {
		return int(outLen), ghosttyErr(res)
	}
	if res != ghosttySuccess {
		return 0, ghosttyErr(res)
	}
	return int(outLen), nil
}

func (g *ghostLib) gridRefHyperlinkURI(ref *ghosttyGridRef, buf []byte) (needed int, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var outLen uint64
	var res int32
	var bufPtr unsafe.Pointer
	bufLen := uint64(len(buf))
	if len(buf) > 0 {
		bufPtr = unsafe.Pointer(unsafe.SliceData(buf))
	}
	callErr := ffi.CallFunctionContext(context.Background(), &g.cifGridRefHyperlinkURI, g.symGridRefHyperlinkURI, unsafe.Pointer(&res), []unsafe.Pointer{
		unsafe.Pointer(ref),
		bufPtr,
		unsafe.Pointer(&bufLen),
		unsafe.Pointer(&outLen),
	})
	runtime.KeepAlive(buf)
	if callErr != nil {
		return 0, callErr
	}
	if res == ghosttyOutOfSpace {
		return int(outLen), ghosttyErr(res)
	}
	if res != ghosttySuccess {
		return 0, ghosttyErr(res)
	}
	return int(outLen), nil
}

func (g *ghostLib) gridRefStyle(ref *ghosttyGridRef, out *ghosttyStyle) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	var res int32
	err := ffi.CallFunctionContext(context.Background(), &g.cifGridRefStyle, g.symGridRefStyle, unsafe.Pointer(&res), []unsafe.Pointer{
		unsafe.Pointer(ref),
		unsafe.Pointer(out),
	})
	if err != nil {
		return err
	}
	return ghosttyErr(res)
}

func (g *ghostLib) cellGet(cell uint64, key int32, dst unsafe.Pointer) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	c := cell
	k := key
	var res int32
	err := ffi.CallFunctionContext(context.Background(), &g.cifCellGet, g.symCellGet, unsafe.Pointer(&res), []unsafe.Pointer{
		unsafe.Pointer(&c),
		unsafe.Pointer(&k),
		dst,
	})
	if err != nil {
		return err
	}
	return ghosttyErr(res)
}

func (g *ghostLib) styleDefault(out *ghosttyStyle) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return ffi.CallFunctionContext(context.Background(), &g.cifStyleDefault, g.symStyleDefault, nil, []unsafe.Pointer{
		unsafe.Pointer(out),
	})
}

func (g *ghostLib) styleIsDefault(st *ghosttyStyle) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out uint8
	err := ffi.CallFunctionContext(context.Background(), &g.cifStyleIsDefault, g.symStyleIsDefault, unsafe.Pointer(&out), []unsafe.Pointer{
		unsafe.Pointer(st),
	})
	if err != nil {
		return false, err
	}
	return out != 0, nil
}

// Available reports whether libghostty-vt can be loaded in this environment.
func Available() error {
	_, err := ghostLibSingleton()
	return err
}
