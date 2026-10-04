# wavepaper

A small niri-aware wallpaper renderer. It rasterizes a (typically tall) SVG
wallpaper once at startup, then draws it as a `wlr-layer-shell` background
surface on Wayland:

- **Parallax on workspace switch**: pans vertically through the source
  image toward `workspace_idx * output_height`, eased in, driven by niri's
  IPC event stream.
- **Waves**: every frame, each row of the image is rotated horizontally by
  a small sine-based offset that varies with row position and time, giving
  a cheap ripple animation.

No GPU/EGL involved (just `wl_shm`), so it's plain CPU rendering.

## Requirements

- A niri session (uses `$NIRI_SOCKET` and `wlr-layer-shell-unstable-v1`,
  which niri implements).
- `rsvg-convert` on `PATH` (ships with `librsvg`, which you already have in
  your package list).
- Go 1.21+ to build.

## Building

```sh
go build -o wavepaper .
```

## Running

```sh
./wavepaper --svg ~/Pictures/wallpapers/blob.svg
```

Flags:

| Flag                | Default      | Meaning                                                        |
|---------------------|--------------|-----------------------------------------------------------------|
| `--svg`              | *(required)* | Path to the tall SVG wallpaper                                  |
| `--namespace`        | `wavepaper`  | Layer-shell namespace (must match the niri layer-rule below)   |
| `--output`           | *(first)*    | `wl_output` name to render on, e.g. from `niri msg outputs`      |
| `--wave-amplitude`   | `6`          | Horizontal wave displacement, in pixels                         |
| `--wave-wavelength`  | `260`        | Vertical distance for one full wave cycle, in pixels             |
| `--wave-speed`       | `0.15`       | Wave cycles per second                                           |
| `--pan-smoothing`    | `0.25`       | Pan easing time constant, in seconds (higher = lazier/softer)   |
| `--idle-fps`         | `20`         | FPS limit while idle (not panning). To find the correct value for your other settings, slowly try out continuously smaller limits and pick the one where you can't see any stuttering. Doing this is recommended if you don't like wasting system recources :p |

## Required niri config

We do our own vertical panning based on niri's reported workspace index,
so niri itself shouldn't *also* try to scroll the surface so pin it to the
backdrop:

```kdl
layer-rule {
    match namespace="^wavepaper$"
    place-within-backdrop true
}
```

Then add the following spawn-at-startup line with something like this:

```kdl
spawn-at-startup "wavepaper" "--svg" "/home/you/.config/wallpaper/blob.svg"
```

## Packaging as a Nix package

Sketch for your flake (adjust the source path to wherever you vendor this
into your config repo):

```nix
wavepaper = pkgs.buildGoModule {
  pname = "wavepaper";
  version = "0.1.0";
  src = ./path/to/wavepaper;
  vendorHash = null; 
  nativeBuildInputs = [ pkgs.librsvg ]; 
};
```

You'll want to `wrapProgram` it (or add `librsvg` to your home-manager
package list, which you already have) so `rsvg-convert` is on `PATH` at
runtime regardless of build-time deps.

## Known limitations / next steps

- **Single output only.** Picks the first output, or the one named via
  `--output`. Multi-monitor support would mean running one instance per
  output (each with its own `--output` flag) or extending `main.go` to
  manage multiple layer surfaces at once.
- **No HiDPI/`buffer_scale` handling.** On a scaled output this will render
  at logical, not physical, pixel density. Fix: call
  `surface.SetBufferScale()` with the output's reported scale and rasterize
  at physical pixels.
- **Workspace `idx` → pixel offset is a straight multiply.** If your
  workspaces don't map cleanly to "one screen-height of image per
  workspace," or you use niri's dynamic workspace creation heavily, you may
  want to tune this mapping.
- **No frame-perfect sync with niri's internal scroll animation** — niri's
  IPC doesn't expose continuous scroll position (as of niri's current IPC),
  so this eases toward a target locally instead, the same approach tools
  like hyprlax/pandora take.

## Source layout

```
main.go        — Wayland orchestration, shm double-buffering, frame loop
wlrlayer/      — hand-written wlr-layer-shell-unstable-v1 bindings
niri/          — niri IPC event-stream client
render/        — SVG rasterization + pan/wave frame rendering
protocols/     — the wlr-layer-shell-unstable-v1.xml this was written against
```
