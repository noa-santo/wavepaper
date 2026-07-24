// Package render turns a (typically tall, SVG-sourced) wallpaper image into
// per-frame XRGB8888 pixel buffers, applying:
//
//   - a vertical pan, eased toward whatever target niri workspace switches
//     set (see the niri package) — this is the "parallax on workspace
//     switch" effect;
//   - a per-row horizontal rotation driven by a sine wave — this is the
//     "waves" animation.
//
// The wave effect is implemented as a whole-row rotation (two slice copies
// per row) rather than a per-pixel shader, which keeps it cheap enough to
// run at 60fps in plain Go with no GPU involved.
package render

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"os/exec"
)

// Source holds the rasterized wallpaper pixels in a simple, fast-to-index
// row-major RGB layout (no alpha — the wallpaper is always opaque).
type Source struct {
	W, H int
	// Pix holds W*H*3 bytes, row-major, RGB order.
	Pix []byte
}

// LoadSVG rasterizes an SVG file to the given pixel width (height follows
// the SVG's intrinsic aspect ratio) using rsvg-convert, which must be on
// PATH (it ships with librsvg).
func LoadSVG(path string, width int) (*Source, error) {
	cmd := exec.Command("rsvg-convert", "-w", fmt.Sprintf("%d", width), "--format", "png", path)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("rsvg-convert failed: %w (stderr: %s)", err, stderr.String())
	}

	img, err := png.Decode(&out)
	if err != nil {
		return nil, fmt.Errorf("decoding rasterized PNG: %w", err)
	}
	return fromImage(img), nil
}

func fromImage(img image.Image) *Source {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	s := &Source{W: w, H: h, Pix: make([]byte, w*h*3)}
	i := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			s.Pix[i+0] = byte(r >> 8)
			s.Pix[i+1] = byte(g >> 8)
			s.Pix[i+2] = byte(bl >> 8)
			i += 3
		}
	}
	return s
}

// Params configures the animation.
type Params struct {
	// WaveAmplitudePx is the max horizontal displacement of a row, in
	// pixels.
	WaveAmplitudePx float64
	// WaveWavelengthPx is the vertical distance (in output pixels) over
	// which the wave completes one full cycle.
	WaveWavelengthPx float64
	// WaveSpeedHz is how many full cycles per second the wave animates
	// through (independent of wavelength).
	WaveSpeedHz float64
	// PanSmoothingSeconds controls how quickly the vertical pan eases
	// toward its target; roughly the time constant of an exponential
	// ease, not a hard duration.
	PanSmoothingSeconds float64
}

// Renderer produces successive frames for one output.
type Renderer struct {
	src    *Source
	params Params

	outW, outH int

	panCurrent float64
	panTarget  float64
}

func NewRenderer(src *Source, outW, outH int, params Params) *Renderer {
	return &Renderer{src: src, outW: outW, outH: outH, params: params}
}

// SetPanTarget sets the vertical pixel offset (into the source image) that
// the renderer should ease toward. Callers are expected to compute this as
// e.g. workspaceIdx * outputHeight; it gets clamped to the valid range here.
func (r *Renderer) SetPanTarget(px float64) {
	max := float64(r.src.H - r.outH)
	if max < 0 {
		max = 0
	}
	if px < 0 {
		px = 0
	}
	if px > max {
		px = max
	}
	r.panTarget = px
}

// Advance steps the pan easing forward by dt seconds. Call once per frame
// before Render.
func (r *Renderer) Advance(dt float64) {
	tau := r.params.PanSmoothingSeconds
	if tau <= 0 {
		tau = 0.001
	}
	factor := 1 - math.Exp(-dt/tau)
	r.panCurrent += (r.panTarget - r.panCurrent) * factor
}

// Render writes one XRGB8888 (little-endian, byte order B,G,R,X) frame into
// dst, which must be at least outW*outH*4 bytes. t is the animation clock in
// seconds (free-running, only used for the wave phase).
func (r *Renderer) Render(dst []byte, t float64) {
	srcW, srcH := r.src.W, r.src.H
	stride := r.outW * 4

	panBase := int(math.Floor(r.panCurrent))
	if maxPan := srcH - r.outH; maxPan >= 0 && panBase > maxPan {
		panBase = maxPan
	}
	if panBase < 0 {
		panBase = 0
	}

	wavelength := r.params.WaveWavelengthPx
	if wavelength <= 0 {
		wavelength = 1
	}

	for y := 0; y < r.outH; y++ {
		srcY := panBase + y
		if srcY >= srcH {
			srcY = srcH - 1
		}
		if srcY < 0 {
			srcY = 0
		}

		phase := float64(y)/wavelength + t*r.params.WaveSpeedHz
		shift := r.params.WaveAmplitudePx * math.Sin(2*math.Pi*phase)
		shiftMod := int(math.Round(shift)) % srcW
		if shiftMod < 0 {
			shiftMod += srcW
		}

		srcRowOff := srcY * srcW * 3
		dstRowOff := y * stride

		// dst[x] = src[(x - shiftMod) mod srcW]; implemented as a row
		// rotation using at most two contiguous copies instead of a
		// per-pixel loop.
		writeRotatedRow(dst[dstRowOff:dstRowOff+stride], r.src.Pix[srcRowOff:srcRowOff+srcW*3], srcW, r.outW, shiftMod)
	}
}

// writeRotatedRow fills dstRow (outW XRGB8888 pixels) by sampling srcRow
// (srcW RGB pixels), rotated right by shiftMod pixels and tiled/clamped to
// outW if the widths differ (they normally won't, since the source is
// rasterized at the output's width).
func writeRotatedRow(dstRow []byte, srcRow []byte, srcW, outW, shiftMod int) {
	convert := func(dstOff, srcOff int) {
		r := srcRow[srcOff+0]
		g := srcRow[srcOff+1]
		b := srcRow[srcOff+2]
		dstRow[dstOff+0] = b
		dstRow[dstOff+1] = g
		dstRow[dstOff+2] = r
		dstRow[dstOff+3] = 0xff
	}

	if outW == srcW {
		// First shiftMod destination pixels come from the tail of the
		// source row, the rest from the head — a rotate-right.
		for x := 0; x < shiftMod; x++ {
			convert(x*4, (srcW-shiftMod+x)*3)
		}
		for x := shiftMod; x < outW; x++ {
			convert(x*4, (x-shiftMod)*3)
		}
		return
	}

	// Widths differ (unusual, e.g. multi-output with different scales
	// sharing one Source): fall back to per-pixel modulo sampling.
	for x := 0; x < outW; x++ {
		sx := ((x-shiftMod)%srcW + srcW) % srcW
		convert(x*4, sx*3)
	}
}
