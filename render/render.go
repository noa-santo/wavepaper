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

// Source holds the rasterized wallpaper pixels pre-converted to XRGB8888 —
// the same byte layout the Wayland buffer needs — so per-frame rendering is
// just a copy() per row instead of a per-pixel format conversion.
type Source struct {
	W, H int
	// Pix holds W*H*4 bytes, row-major, byte order B,G,R,X (matches
	// wl_shm's little-endian XRGB8888).
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
	s := &Source{W: w, H: h, Pix: make([]byte, w*h*4)}
	i := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			s.Pix[i+0] = byte(bl >> 8)
			s.Pix[i+1] = byte(g >> 8)
			s.Pix[i+2] = byte(r >> 8)
			s.Pix[i+3] = 0xff
			i += 4
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
	marginX    int // extra source pixels on each side, for the wave to sample into

	panCurrent float64
	panTarget  float64
}

func NewRenderer(src *Source, outW, outH int, params Params) *Renderer {
	margin := (src.W - outW) / 2
	if margin < 0 {
		margin = 0
	}
	return &Renderer{src: src, outW: outW, outH: outH, marginX: margin, params: params}
}

// SetPanTarget sets the vertical pixel offset (into the source image) that
// the renderer should ease toward. Callers are expected to compute this as
// e.g. workspaceIdx * outputHeight; it gets clamped to the valid range here.
func (r *Renderer) SetPanTarget(px float64) {
	maxValue := float64(r.src.H - r.outH)
	if maxValue < 0 {
		maxValue = 0
	}
	if px < 0 {
		px = 0
	}
	if px > maxValue {
		px = maxValue
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
		srcX := r.marginX + int(math.Round(shift))
		if srcX < 0 {
			srcX = 0
		}
		if maxX := srcW - r.outW; srcX > maxX {
			srcX = maxX
		}

		srcRowOff := (srcY*srcW + srcX) * 4
		dstRowOff := y * stride

		copy(dst[dstRowOff:dstRowOff+stride], r.src.Pix[srcRowOff:srcRowOff+stride])
	}
}
