// Command wavepaper is a small niri-aware wallpaper renderer: it rasterizes
// a (typically tall) SVG wallpaper once at startup, then draws it as a
// wlr-layer-shell background surface, panning vertically to roughly track
// niri workspace switches and animating a horizontal "wave" ripple every
// frame — no GPU/EGL required, just wl_shm.
//
// Usage:
//
//	wavepaper --svg ~/path/to/wallpaper.svg [flags]
//
// See README.md for the full flag list, the niri layer-rule you need
// (place-within-backdrop for this surface's namespace), and known
// limitations (single output, no HiDPI buffer-scale handling yet).
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"runtime/debug"
	"time"

	"github.com/rajveermalviya/go-wayland/wayland/client"
	"golang.org/x/sys/unix"

	"wavepaper/niri"
	"wavepaper/render"
	"wavepaper/wlrlayer"
)

func main() {
	svgPath := flag.String("svg", "", "path to the (tall) SVG wallpaper (required)")
	namespace := flag.String("namespace", "wavepaper", "layer-shell namespace; must match your niri place-within-backdrop layer-rule")
	outputName := flag.String("output", "", "wl_output name to render on (e.g. from `niri msg outputs`); default: first output seen")
	amplitude := flag.Float64("wave-amplitude", 16, "wave horizontal displacement, in pixels")
	wavelength := flag.Float64("wave-wavelength", 250, "wave vertical wavelength, in pixels")
	waveSpeed := flag.Float64("wave-speed", 0.15, "wave cycles per second")
	panSmoothing := flag.Float64("pan-smoothing", 0.25, "pan easing time constant, in seconds")
	idleFPS := flag.Float64("idle-fps", -1, "max fps while only the wave animates; -1 = auto (1px/frame at the wave's fastest point), 0 = every vblank; full rate while the pan eases")
	flag.Parse()

	if *svgPath == "" {
		fmt.Fprintln(os.Stderr, "wavepaper: --svg is required")
		flag.Usage()
		os.Exit(2)
	}

	if err := run(*svgPath, *namespace, *outputName, *idleFPS, render.Params{
		WaveAmplitudePx:     *amplitude,
		WaveWavelengthPx:    *wavelength,
		WaveSpeedHz:         *waveSpeed,
		PanSmoothingSeconds: *panSmoothing,
	}); err != nil {
		log.Fatalf("wavepaper: %v", err)
	}
}

type globalInfo struct {
	name    uint32
	iface   string
	version uint32
}

func run(svgPath, namespace, wantOutput string, idleFPS float64, params render.Params) error {
	display, err := client.Connect("")
	if err != nil {
		return fmt.Errorf("connecting to Wayland display: %w", err)
	}
	ctx := display.Context()
	defer ctx.Close()

	display.SetErrorHandler(func(e client.DisplayErrorEvent) {
		log.Printf("wavepaper: FATAL wl_display error: object=%v code=%d message=%q", e.ObjectId, e.Code, e.Message)
	})

	registry, err := display.GetRegistry()
	if err != nil {
		return fmt.Errorf("get_registry: %w", err)
	}

	var globals []globalInfo
	registry.SetGlobalHandler(func(e client.RegistryGlobalEvent) {
		globals = append(globals, globalInfo{e.Name, e.Interface, e.Version})
	})

	if err := roundtrip(display, ctx); err != nil {
		return fmt.Errorf("initial roundtrip: %w", err)
	}

	var (
		compositor *client.Compositor
		shm        *client.Shm
		layerShell *wlrlayer.LayerShell
		outputs    []*client.Output
	)
	outputName := map[*client.Output]string{}

	for _, g := range globals {
		switch g.iface {
		case "wl_compositor":
			log.Printf("wavepaper: binding %s name=%d version=%d (advertised=%d)", g.iface, g.name, min32(g.version, 4), g.version)
			compositor = client.NewCompositor(ctx)
			must(bindGlobal(registry, g.name, g.iface, min32(g.version, 4), compositor))
			if err := roundtrip(display, ctx); err != nil {
				return fmt.Errorf("roundtrip after binding %s: %w", g.iface, err)
			}
		case "wl_shm":
			log.Printf("wavepaper: binding %s name=%d version=1 (advertised=%d)", g.iface, g.name, g.version)
			shm = client.NewShm(ctx)
			must(bindGlobal(registry, g.name, g.iface, 1, shm))
			if err := roundtrip(display, ctx); err != nil {
				return fmt.Errorf("roundtrip after binding %s: %w", g.iface, err)
			}
		case "zwlr_layer_shell_v1":
			log.Printf("wavepaper: binding %s name=%d version=%d (advertised=%d)", g.iface, g.name, min32(g.version, 4), g.version)
			layerShell = wlrlayer.NewLayerShell(ctx)
			must(bindGlobal(registry, g.name, g.iface, min32(g.version, 4), layerShell))
			if err := roundtrip(display, ctx); err != nil {
				return fmt.Errorf("roundtrip after binding %s: %w", g.iface, err)
			}
		case "wl_output":
			log.Printf("wavepaper: binding %s name=%d version=%d (advertised=%d)", g.iface, g.name, min32(g.version, 4), g.version)
			o := client.NewOutput(ctx)
			must(bindGlobal(registry, g.name, g.iface, min32(g.version, 4), o))
			if err := roundtrip(display, ctx); err != nil {
				return fmt.Errorf("roundtrip after binding %s: %w", g.iface, err)
			}
			outputs = append(outputs, o)
			oo := o
			oo.SetNameHandler(func(e client.OutputNameEvent) {
				outputName[oo] = e.Name
			})
		}
	}
	log.Printf("wavepaper: all globals bound successfully")

	if compositor == nil || shm == nil || layerShell == nil {
		return fmt.Errorf("compositor is missing wl_compositor, wl_shm, or zwlr_layer_shell_v1 (are you running this under niri?)")
	}
	if len(outputs) == 0 {
		return fmt.Errorf("no wl_output found")
	}

	// Second roundtrip: flush pending wl_output events (name, geometry,
	// mode, done) so outputName is populated before we pick a target.
	if err := roundtrip(display, ctx); err != nil {
		return fmt.Errorf("output roundtrip: %w", err)
	}

	target := outputs[0]
	if wantOutput != "" {
		found := false
		for _, o := range outputs {
			if outputName[o] == wantOutput {
				target = o
				found = true
				break
			}
		}
		if !found {
			log.Printf("wavepaper: output %q not found, falling back to %q", wantOutput, outputName[outputs[0]])
		}
	}

	surface, err := compositor.CreateSurface()
	if err != nil {
		return fmt.Errorf("create_surface: %w", err)
	}

	layerSurface, err := layerShell.GetLayerSurface(surface, target, wlrlayer.LayerBackground, namespace)
	if err != nil {
		return fmt.Errorf("get_layer_surface: %w", err)
	}
	must(layerSurface.SetAnchor(wlrlayer.AnchorFill))
	must(layerSurface.SetExclusiveZone(-1))
	must(layerSurface.SetKeyboardInteractivity(wlrlayer.KeyboardInteractivityNone))
	must(layerSurface.SetSize(0, 0))

	configured := make(chan struct{}, 1)
	var outW, outH int
	layerSurface.SetConfigureHandler(func(e wlrlayer.LayerSurfaceConfigureEvent) {
		outW, outH = int(e.Width), int(e.Height)
		must(layerSurface.AckConfigure(e.Serial))
		select {
		case configured <- struct{}{}:
		default:
		}
	})
	layerSurface.SetClosedHandler(func(wlrlayer.LayerSurfaceClosedEvent) {
		log.Println("wavepaper: layer surface closed by compositor, exiting")
		os.Exit(0)
	})

	if err := surface.Commit(); err != nil {
		return fmt.Errorf("initial commit: %w", err)
	}
	for outW == 0 || outH == 0 {
		if err := ctx.Dispatch(); err != nil {
			return fmt.Errorf("waiting for configure: %w", err)
		}
	}

	log.Printf("wavepaper: rendering %dx%d on output %q", outW, outH, outputName[target])

	region, err := compositor.CreateRegion()
	if err != nil {
		return fmt.Errorf("create_region: %w", err)
	}
	must(region.Add(0, 0, int32(outW), int32(outH)))
	must(surface.SetOpaqueRegion(region))
	must(region.Destroy())

	rasterWidth := outW + 2*int(math.Ceil(params.WaveAmplitudePx))
	src, err := render.LoadSVG(svgPath, rasterWidth)
	if err != nil {
		return fmt.Errorf("loading %s: %w", svgPath, err)
	}
	if src.H < outH {
		log.Printf("wavepaper: warning: rasterized wallpaper (%dx%d) is not taller than the output (%dx%d); there won't be much to pan through", src.W, src.H, outW, outH)
	}

	_, bufs, err := newDoubleBuffer(shm, outW, outH)
	if err != nil {
		return fmt.Errorf("setting up shm buffers: %w", err)
	}
	debug.FreeOSMemory()

	renderer := render.NewRenderer(src, outW, outH, params)

	// niri workspace watcher: updates the pan target as workspaces change.
	moves := make(chan niri.WorkspaceMoved, 4)
	go func() {
		for {
			if err := niri.Watch(outputName[target], moves); err != nil {
				log.Printf("wavepaper: niri watch: %v (retrying in 2s)", err)
				time.Sleep(2 * time.Second)
			}
		}
	}()
	go func() {
		for m := range moves {
			renderer.SetPanTarget(float64(m.Idx) * float64(outH) * 0.1)
		}
	}()

	start := time.Now()
	lastFrame := start
	cur := 0
	if idleFPS < 0 {
		idleFPS = math.Max(1, math.Ceil(2*math.Pi*params.WaveAmplitudePx*params.WaveSpeedHz))
	}
	var idleInterval time.Duration
	if idleFPS != 0 {
		idleInterval = time.Duration(float64(time.Second) / idleFPS)
	}

	var renderFrame func()
	renderFrame = func() {
		now := time.Now()
		dt := now.Sub(lastFrame).Seconds()
		lastFrame = now

		renderer.Advance(dt)
		renderer.Render(bufs[cur].mem, now.Sub(start).Seconds())

		must(surface.Attach(bufs[cur].wl, 0, 0))
		must(surface.Damage(0, 0, int32(outW), int32(outH)))
		cb, err := surface.Frame()
		if err != nil {
			log.Fatalf("wavepaper: surface.Frame: %v", err)
		}
		cb.SetDoneHandler(func(client.CallbackDoneEvent) {
			if idleInterval > 0 && !renderer.Panning() {
				if d := idleInterval - time.Since(lastFrame); d > 0 {
					time.Sleep(d)
				}
			}
			renderFrame()
		})
		must(surface.Commit())

		cur = 1 - cur
	}
	renderFrame()

	for {
		if err := ctx.Dispatch(); err != nil {
			return fmt.Errorf("dispatch: %w", err)
		}
	}
}

func roundtrip(display *client.Display, ctx *client.Context) error {
	done := make(chan struct{}, 1)
	cb, err := display.Sync()
	if err != nil {
		return err
	}
	cb.SetDoneHandler(func(client.CallbackDoneEvent) {
		select {
		case done <- struct{}{}:
		default:
		}
	})
	for {
		select {
		case <-done:
			return nil
		default:
		}
		if err := ctx.Dispatch(); err != nil {
			return err
		}
	}
}

type wlBuffer struct {
	wl  *client.Buffer
	mem []byte
}

func newDoubleBuffer(shm *client.Shm, w, h int) (*client.ShmPool, [2]wlBuffer, error) {
	var bufs [2]wlBuffer

	stride := w * 4
	frameSize := stride * h
	poolSize := frameSize * 2

	fd, err := unix.MemfdCreate("wavepaper-shm", 0)
	if err != nil {
		return nil, bufs, fmt.Errorf("memfd_create: %w", err)
	}
	defer unix.Close(fd)

	if err := unix.Ftruncate(fd, int64(poolSize)); err != nil {
		return nil, bufs, fmt.Errorf("ftruncate: %w", err)
	}

	mem, err := unix.Mmap(fd, 0, poolSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, bufs, fmt.Errorf("mmap: %w", err)
	}

	pool, err := shm.CreatePool(fd, int32(poolSize))
	if err != nil {
		return nil, bufs, fmt.Errorf("create_pool: %w", err)
	}

	for i := 0; i < 2; i++ {
		off := i * frameSize
		b, err := pool.CreateBuffer(int32(off), int32(w), int32(h), int32(stride), uint32(client.ShmFormatXrgb8888))
		if err != nil {
			return nil, bufs, fmt.Errorf("create_buffer: %w", err)
		}
		bufs[i] = wlBuffer{wl: b, mem: mem[off : off+frameSize]}
	}

	return pool, bufs, nil
}

func must(err error) {
	if err != nil {
		log.Fatalf("wavepaper: wayland request failed: %v", err)
	}
}

func min32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

// bindGlobal is a drop-in replacement for (*client.Registry).Bind. The
// upstream implementation writes the *padded* interface-name length into
// the wire message instead of the true (unpadded) length, which niri's
// strict (smithay-based) request validation rejects for any interface name
// whose length+1 isn't already a multiple of 4 (e.g. "wl_compositor",
// "wl_shm", "wl_output" — but not "zwlr_layer_shell_v1", which is why only
// some binds fail). This reimplements the same request with the correct
// length field.
func bindGlobal(registry *client.Registry, name uint32, iface string, version uint32, id client.Proxy) error {
	const opcode = 0
	trueLen := len(iface) + 1 // string + NUL, per the wire protocol
	padded := trueLen
	if padded&0x3 != 0 {
		padded += 4 - (padded & 0x3)
	}
	reqLen := 8 + 4 + (4 + padded) + 4 + 4
	buf := make([]byte, reqLen)
	l := 0
	client.PutUint32(buf[l:l+4], registry.ID())
	l += 4
	client.PutUint32(buf[l:l+4], uint32(reqLen<<16|opcode&0x0000ffff))
	l += 4
	client.PutUint32(buf[l:l+4], name)
	l += 4
	client.PutUint32(buf[l:l+4], uint32(trueLen)) // <- the actual fix: true length, not padded
	l += 4
	copy(buf[l:l+len(iface)], iface)
	l += padded
	client.PutUint32(buf[l:l+4], version)
	l += 4
	client.PutUint32(buf[l:l+4], id.ID())
	return registry.Context().WriteMsg(buf, nil)
}
