// Package niri implements a minimal client for niri's IPC event stream
// (see https://yalter.github.io/niri/IPC.html). We only care about enough
// of the protocol to know which workspace is active and where it sits in
// its output's vertical stack (its "idx"), so the wallpaper can pan to
// roughly the right spot.
//
// We deliberately don't depend on the niri-ipc Rust crate (there's no Go
// equivalent) and only decode the handful of fields we need, ignoring
// everything else in each event so we don't break if niri adds fields or
// event variants in future versions.
package niri

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
)

// WorkspaceMoved is delivered whenever the workspace that's active on the
// watched output changes (or its position among that output's workspaces
// changes). Idx is niri's 0-based stacking index for that workspace on its
// output — this is what you multiply by your output's height to get a
// vertical pan target.
type WorkspaceMoved struct {
	Output string
	Idx    int
}

type workspace struct {
	ID       uint64 `json:"id"`
	Idx      int    `json:"idx"`
	Output   string `json:"output"`
	IsActive bool   `json:"is_active"`
}

// Watch connects to the niri IPC socket, subscribes to the event stream, and
// sends a WorkspaceMoved on ch every time the active workspace (or its idx)
// changes on the given output. If output is "", the first output any active
// workspace reports is used and locked in from then on. Watch blocks until
// the connection is closed or ctx-like cancellation isn't needed since this
// is meant to run for the lifetime of the process — on error it retries with
// a fresh connection.
func Watch(output string, ch chan<- WorkspaceMoved) error {
	sockPath := os.Getenv("NIRI_SOCKET")
	if sockPath == "" {
		return fmt.Errorf("niri: NIRI_SOCKET is not set (are you running inside a niri session?)")
	}

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return fmt.Errorf("niri: connecting to %s: %w", sockPath, err)
	}
	defer conn.Close()

	// Request::EventStream serializes to the bare JSON string "EventStream".
	if _, err := conn.Write([]byte("\"EventStream\"\n")); err != nil {
		return fmt.Errorf("niri: sending EventStream request: %w", err)
	}

	reader := bufio.NewReaderSize(conn, 1<<16)

	// First line is the Reply to our request (either "\"Ok\""-ish wrapper or
	// an error). We don't strictly need to parse it, but reading it keeps us
	// in sync with the stream framing.
	if _, err := reader.ReadString('\n'); err != nil {
		return fmt.Errorf("niri: reading initial reply: %w", err)
	}

	var mu sync.Mutex
	workspaces := map[uint64]workspace{}
	watchedOutput := output

	emit := func() {
		mu.Lock()
		defer mu.Unlock()
		for _, ws := range workspaces {
			if !ws.IsActive {
				continue
			}
			if watchedOutput == "" {
				watchedOutput = ws.Output
			}
			if ws.Output != watchedOutput {
				continue
			}
			select {
			case ch <- WorkspaceMoved{Output: ws.Output, Idx: ws.Idx}:
			default:
				// Drop if the renderer hasn't drained the last update yet;
				// the next event will carry the up-to-date state anyway.
			}
		}
	}

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return fmt.Errorf("niri: event stream closed: %w", err)
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			continue // ignore malformed/unknown lines rather than crashing
		}

		if payload, ok := raw["WorkspacesChanged"]; ok {
			var body struct {
				Workspaces []workspace `json:"workspaces"`
			}
			if err := json.Unmarshal(payload, &body); err == nil {
				mu.Lock()
				for _, ws := range body.Workspaces {
					workspaces[ws.ID] = ws
				}
				mu.Unlock()
				emit()
			}
			continue
		}

		if payload, ok := raw["WorkspaceActivated"]; ok {
			var body struct {
				ID uint64 `json:"id"`
			}
			if err := json.Unmarshal(payload, &body); err == nil {
				mu.Lock()
				for id, ws := range workspaces {
					wasActive := ws.IsActive
					ws.IsActive = id == body.ID
					if ws.IsActive != wasActive {
						workspaces[id] = ws
					}
				}
				mu.Unlock()
				emit()
			}
			continue
		}

		// Everything else (window events, keyboard layout, etc.) is
		// irrelevant to the wallpaper and intentionally ignored.
	}
}
