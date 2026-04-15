package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"knv/k8s"
	"knv/model"
	"knv/nodeexec"
	"knv/ui"

	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

func main() {
	// Recover from panics so we don't leave the terminal in alt-screen.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "knv panic: %v\n", r)
			os.Exit(2)
		}
	}()

	useMock := flag.Bool("mock", false, "use mock data instead of a live cluster")
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig file (default: $KUBECONFIG or ~/.kube/config)")
	kubeCtx := flag.String("context", "", "kubernetes context name to use (default: current-context)")
	refreshSec := flag.Int("refresh", int(model.DefaultRefreshInterval.Seconds()),
		fmt.Sprintf("refresh interval in seconds (min %ds)", int(model.MinRefreshInterval.Seconds())))
	noColor := flag.Bool("no-color", false, "disable color output (also honoured via NO_COLOR env var)")
	debugImage := flag.String("debug-image", nodeexec.DefaultDebugImage, "container image for kubectl debug node sessions")
	debugNS := flag.String("debug-namespace", "", "namespace for kubectl debug pods (default: server default)")
	output := flag.String("output", "", "non-TUI output mode: json or table (exits after printing)")
	debugLog := flag.String("debug", "", "write debug log to this file path")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "knv %s — Kubernetes Node Viewer\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage: knv [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Keybindings:
  Node List
    ↑/k  ↓/j        navigate nodes
    g    G           jump to first / last
    enter / space    open inspect panel
    P                full pod list for node
    e                exec into node (kubectl debug)
    c                cordon / uncordon node
    d                drain node
    y                copy node name to clipboard
    /                filter  (supports label:key=value syntax)
    esc              clear filter
    s                cycle sort column (name/status/group/cpu/mem/age/pods)
    S                toggle sort direction (▲ asc / ▼ desc)
    r                manual refresh
    ?                toggle help overlay
    q / ctrl+c       quit

  Inspect Panel
    ↑/k  ↓/j    scroll
    n/]  p/[    next / previous node
    P           full pod list for node
    e           exec into node
    c           cordon / uncordon
    d           drain
    y           copy node name
    b / esc     back to list
    ?           toggle help overlay

  Pod List
    ↑/k  ↓/j    scroll
    b / esc     back
`)
	}

	flag.Parse()

	// ── Color profile ─────────────────────────────────────────────────────────
	if *noColor {
		ui.SetNoColor()
	}

	// ── Debug log ─────────────────────────────────────────────────────────────
	var logger *log.Logger
	if *debugLog != "" {
		f, err := os.OpenFile(*debugLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: cannot open debug log %s: %v\n", *debugLog, err)
		} else {
			logger = log.New(f, "[knv] ", log.LstdFlags|log.Lmicroseconds)
			logger.Printf("knv %s starting", version)
		}
	}

	// ── Loader ────────────────────────────────────────────────────────────────
	var loader model.Loader
	if *useMock {
		loader = model.MockLoader{}
	} else {
		client, err := k8s.New(k8s.Options{
			Kubeconfig: *kubeconfig,
			Context:    *kubeCtx,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not connect to cluster (%v) — using mock data\n", err)
			loader = model.MockLoader{}
		} else {
			if err := client.Start(context.Background()); err != nil {
				fmt.Fprintf(os.Stderr, "warning: informer cache sync failed (%v) — data may be incomplete\n", err)
			}
			defer client.Stop()
			loader = client
		}
	}

	// ── Non-TUI output mode ───────────────────────────────────────────────────
	if *output != "" {
		nodes, _, err := loader.FetchNodes()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error fetching nodes: %v\n", err)
			os.Exit(1)
		}
		switch *output {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(nodes); err != nil {
				fmt.Fprintf(os.Stderr, "error encoding JSON: %v\n", err)
				os.Exit(1)
			}
		case "table":
			rows := make([]ui.NodeRow, len(nodes))
			for i, n := range nodes {
				rows[i] = ui.NodeRow{
					Name:            n.Name,
					Status:          string(n.Status),
					NodeGroup:       n.NodeGroup,
					Instance:        n.Instance,
					Zone:            n.Zone,
					CapacityType:    n.CapacityType,
					CPUPercent:      n.CPUPercent,
					MemPercent:      n.MemPercent,
					PodCount:        n.PodCount,
					PodCapacity:     n.PodCapacity,
					KubeletVersion:  n.KubeletVersion,
					KarpenterStatus: n.KarpenterStatus,
				}
			}
			ui.RenderNonTUITable(rows, os.Stdout)
		default:
			fmt.Fprintf(os.Stderr, "unknown output format %q — use json or table\n", *output)
			os.Exit(1)
		}
		return
	}

	// ── TUI ───────────────────────────────────────────────────────────────────
	interval := time.Duration(*refreshSec) * time.Second

	p := tea.NewProgram(
		model.InitialModel(model.Config{
			Loader:         loader,
			Interval:       interval,
			DebugImage:     *debugImage,
			DebugNamespace: *debugNS,
			Logger:         logger,
		}),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
