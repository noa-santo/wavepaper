// Package wlrlayer implements just enough of the wlr-layer-shell-unstable-v1
// Wayland protocol (zwlr_layer_shell_v1 / zwlr_layer_surface_v1) to create a
// background-layer surface, in the same style as the code
// github.com/rajveermalviya/go-wayland/wayland's own generator produces for
// other protocols. It's hand-written rather than generated because
// wlr-layer-shell isn't part of upstream wayland-protocols, so it isn't
// bundled with that module.
//
// Protocol source: wlr-layer-shell-unstable-v1.xml
// Copyright © 2017 Drew DeVault (MIT-style permissive license)
package wlrlayer

import (
	"github.com/rajveermalviya/go-wayland/wayland/client"
)

// ---- zwlr_layer_shell_v1 -------------------------------------------------

type Layer uint32

const (
	LayerBackground Layer = 0
	LayerBottom     Layer = 1
	LayerTop        Layer = 2
	LayerOverlay    Layer = 3
)

type LayerShell struct {
	client.BaseProxy
}

func NewLayerShell(ctx *client.Context) *LayerShell {
	ls := &LayerShell{}
	ctx.Register(ls)
	return ls
}

// GetLayerSurface : opcode 0
//
//	get_layer_surface(new_id id, object surface, object output (nullable), uint layer, string namespace)
func (i *LayerShell) GetLayerSurface(surface *client.Surface, output *client.Output, layer Layer, namespace string) (*LayerSurface, error) {
	id := NewLayerSurface(i.Context())

	const opcode = 0

	nsLen := client.PaddedLen(len(namespace) + 1)

	var outputID uint32
	if output != nil {
		outputID = output.ID()
	}

	_reqBufLen := 8 + 4 + 4 + 4 + 4 + (4 + nsLen)
	_reqBuf := make([]byte, _reqBufLen)
	l := 0
	client.PutUint32(_reqBuf[l:l+4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	client.PutUint32(_reqBuf[l:l+4], id.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], surface.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], outputID)
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(layer))
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(len(namespace)+1))
	copy(_reqBuf[l+4:l+4+len(namespace)], namespace)
	l += 4 + nsLen

	err := i.Context().WriteMsg(_reqBuf, nil)
	return id, err
}

// Destroy : opcode 1
func (i *LayerShell) Destroy() error {
	defer i.Context().Unregister(i)
	const opcode = 1
	const _reqBufLen = 8
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:l+4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// ---- zwlr_layer_surface_v1 -----------------------------------------------

type Anchor uint32

const (
	AnchorTop    Anchor = 1
	AnchorBottom Anchor = 2
	AnchorLeft   Anchor = 4
	AnchorRight  Anchor = 8
)

const AnchorFill = AnchorTop | AnchorBottom | AnchorLeft | AnchorRight

type KeyboardInteractivity uint32

const (
	KeyboardInteractivityNone      KeyboardInteractivity = 0
	KeyboardInteractivityExclusive KeyboardInteractivity = 1
	KeyboardInteractivityOnDemand  KeyboardInteractivity = 2
)

type LayerSurface struct {
	client.BaseProxy
	configureHandler LayerSurfaceConfigureHandlerFunc
	closedHandler    LayerSurfaceClosedHandlerFunc
}

func NewLayerSurface(ctx *client.Context) *LayerSurface {
	ls := &LayerSurface{}
	ctx.Register(ls)
	return ls
}

// SetSize : opcode 0
func (i *LayerSurface) SetSize(width, height uint32) error {
	const opcode = 0
	const _reqBufLen = 8 + 4 + 4
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:l+4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	client.PutUint32(_reqBuf[l:l+4], width)
	l += 4
	client.PutUint32(_reqBuf[l:l+4], height)
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// SetAnchor : opcode 1
func (i *LayerSurface) SetAnchor(anchor Anchor) error {
	const opcode = 1
	const _reqBufLen = 8 + 4
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:l+4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(anchor))
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// SetExclusiveZone : opcode 2
func (i *LayerSurface) SetExclusiveZone(zone int32) error {
	const opcode = 2
	const _reqBufLen = 8 + 4
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:l+4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(zone))
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// SetMargin : opcode 3
func (i *LayerSurface) SetMargin(top, right, bottom, left int32) error {
	const opcode = 3
	const _reqBufLen = 8 + 4 + 4 + 4 + 4
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:l+4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(top))
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(right))
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(bottom))
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(left))
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// SetKeyboardInteractivity : opcode 4
func (i *LayerSurface) SetKeyboardInteractivity(v KeyboardInteractivity) error {
	const opcode = 4
	const _reqBufLen = 8 + 4
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:l+4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(v))
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// AckConfigure : opcode 6
func (i *LayerSurface) AckConfigure(serial uint32) error {
	const opcode = 6
	const _reqBufLen = 8 + 4
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:l+4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	client.PutUint32(_reqBuf[l:l+4], serial)
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// Destroy : opcode 7
func (i *LayerSurface) Destroy() error {
	defer i.Context().Unregister(i)
	const opcode = 7
	const _reqBufLen = 8
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:l+4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// SetLayer : opcode 8 (since version 2)
func (i *LayerSurface) SetLayer(layer Layer) error {
	const opcode = 8
	const _reqBufLen = 8 + 4
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:l+4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(layer))
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// ---- events ----------------------------------------------------------

type LayerSurfaceConfigureEvent struct {
	Serial uint32
	Width  uint32
	Height uint32
}
type LayerSurfaceConfigureHandlerFunc func(LayerSurfaceConfigureEvent)

func (i *LayerSurface) SetConfigureHandler(f LayerSurfaceConfigureHandlerFunc) {
	i.configureHandler = f
}

type LayerSurfaceClosedEvent struct{}
type LayerSurfaceClosedHandlerFunc func(LayerSurfaceClosedEvent)

func (i *LayerSurface) SetClosedHandler(f LayerSurfaceClosedHandlerFunc) {
	i.closedHandler = f
}

// Dispatch implements client.Dispatcher.
func (i *LayerSurface) Dispatch(opcode uint32, fd int, data []byte) {
	switch opcode {
	case 0: // configure
		if i.configureHandler == nil {
			return
		}
		var e LayerSurfaceConfigureEvent
		l := 0
		e.Serial = client.Uint32(data[l : l+4])
		l += 4
		e.Width = client.Uint32(data[l : l+4])
		l += 4
		e.Height = client.Uint32(data[l : l+4])
		i.configureHandler(e)
	case 1: // closed
		if i.closedHandler == nil {
			return
		}
		i.closedHandler(LayerSurfaceClosedEvent{})
	}
}
