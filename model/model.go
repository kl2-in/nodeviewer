package model

import (
	"context"
	"log"
	"sort"
	"strings"
	"time"

	"knv/nodeexec"
	"knv/ui"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	DefaultRefreshInterval = 30 * time.Second
	MinRefreshInterval     = 5 * time.Second
)

type tickMsg struct{}

type viewState int

const (
	viewList          viewState = iota
	viewFilter                  // typing a filter query
	viewInspect                 // expanded node detail
	viewConfirm                 // confirm before exec into node
	viewCordonConfirm           // confirm before cordon/uncordon
	viewDrainConfirm            // confirm before drain
	viewPodList                 // full-screen pod list for a node
	viewHelp                    // floating help overlay
)

// nodesLoadedMsg is the async result from a FetchNodes call.
type nodesLoadedMsg struct {
	nodes       []Node
	clusterName string
	err         error
}

// execDoneMsg is returned after a node debug session exits.
type execDoneMsg struct{ err error }

// mutateResultMsg is returned after a cordon/uncordon/drain operation.
type mutateResultMsg struct {
	action   string
	nodeName string
	err      error
}

// Config holds all options for constructing the model.
type Config struct {
	Loader         Loader
	Interval       time.Duration
	DebugImage     string
	DebugNamespace string
	Logger         *log.Logger
}

// Model is the bubbletea application model.
type Model struct {
	loader         Loader
	mutator        Mutator // non-nil when loader also implements Mutator
	debugImage     string
	debugNamespace string
	logger         *log.Logger

	nodes    []Node
	filtered []Node
	filter   string

	clusterName  string
	errMsg       string
	execErrMsg   string // shown after a failed exec session
	mutateErrMsg string // shown after a failed cordon/drain

	// Error resilience: last successful fetch timestamp and error state
	lastFetchTime time.Time
	lastFetchErr  bool // true if the most recent fetch had an error

	cursor     int
	listOffset int // viewport scroll offset for the node list
	ready      bool
	state      viewState
	prevState  viewState // state to return after overlay is dismissed
	termWidth  int
	termHeight int

	// Sort state
	sortKey string // "name","status","group","cpu","mem","age","pods",""
	sortAsc bool

	// Cordon/drain confirm
	confirmAction string // "cordon", "uncordon", "drain"

	// Clipboard feedback
	clipboardMsg string
	clipboardAt  time.Time

	// Inspect state
	inspectLines  []string
	inspectScroll int

	// Pod list state
	podListScroll int

	interval time.Duration
}

// InitialModel creates the model from Config.
func InitialModel(cfg Config) Model {
	if cfg.DebugImage == "" {
		cfg.DebugImage = nodeexec.DefaultDebugImage
	}
	interval := cfg.Interval
	if interval < MinRefreshInterval {
		interval = DefaultRefreshInterval
	}
	m := Model{
		loader:         cfg.Loader,
		debugImage:     cfg.DebugImage,
		debugNamespace: cfg.DebugNamespace,
		logger:         cfg.Logger,
		ready:          false,
		state:          viewList,
		sortAsc:        true,
		termWidth:      160,
		termHeight:     40,
		interval:       interval,
	}
	if mut, ok := cfg.Loader.(Mutator); ok {
		m.mutator = mut
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return m.fetchCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		if m.state == viewInspect {
			m.inspectLines = m.buildInspectLines()
		}

	case tickMsg:
		return m, m.fetchCmd()

	case nodesLoadedMsg:
		m.ready = true
		m.lastFetchTime = time.Now()
		if msg.err != nil {
			m.lastFetchErr = true
			m.errMsg = msg.err.Error()
			m.logf("fetch error: %v", msg.err)
			// Keep m.nodes (last good data) — do NOT overwrite on error
		} else {
			m.lastFetchErr = false
			m.errMsg = ""
			m.nodes = msg.nodes
			m.clusterName = msg.clusterName
			m.applyFilter()
			if m.state == viewInspect && len(m.filtered) > 0 {
				m.inspectLines = m.buildInspectLines()
			}
		}
		return m, tea.Tick(m.interval, func(time.Time) tea.Msg { return tickMsg{} })

	case execDoneMsg:
		m.state = m.prevState
		if msg.err != nil {
			m.execErrMsg = "session exited: " + msg.err.Error()
		} else {
			m.execErrMsg = ""
		}

	case mutateResultMsg:
		if msg.action != "drain" {
			m.state = m.prevState
		}
		if msg.err != nil {
			if msg.action != "drain" {
				m.mutateErrMsg = msg.action + " failed: " + msg.err.Error()
			} else {
				m.mutateErrMsg = ""
			}
			m.logf("mutate %s on %s failed: %v", msg.action, msg.nodeName, msg.err)
		} else {
			m.mutateErrMsg = ""
			m.logf("mutate %s on %s succeeded", msg.action, msg.nodeName)
		}
		// Refresh immediately to reflect the change
		return m, m.fetchCmd()

	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			switch m.state {
			case viewList:
				switch {
				case msg.Y == 0: // header row — click to sort
					if key := ui.SortKeyAtX(msg.X, m.termWidth); key != "" {
						if key == m.sortKey {
							m.sortAsc = !m.sortAsc
						} else {
							m.sortKey = key
							m.sortAsc = true
						}
						m.applySort()
					}
				case msg.Y >= 2: // data rows (0=header, 1=divider, 2+=rows)
					rowIdx := msg.Y - 2 + m.listOffset
					if rowIdx >= 0 && rowIdx < len(m.filtered) {
						m.cursor = rowIdx
						m.ensureVisible()
					}
				}
			}
		}

	case tea.KeyMsg:
		switch m.state {

		case viewList:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
					m.ensureVisible()
				}
			case "down", "j":
				if m.cursor < len(m.filtered)-1 {
					m.cursor++
					m.ensureVisible()
				}
			case "g":
				m.cursor = 0
				m.listOffset = 0
			case "G":
				if len(m.filtered) > 0 {
					m.cursor = len(m.filtered) - 1
					m.ensureVisible()
				}
			case "enter", " ":
				if len(m.filtered) > 0 {
					m.state = viewInspect
					m.inspectScroll = 0
					m.inspectLines = m.buildInspectLines()
				}
			case "P":
				if len(m.filtered) > 0 {
					m.prevState = viewList
					m.podListScroll = 0
					m.state = viewPodList
				}
			case "/":
				m.state = viewFilter
			case "esc":
				m.filter = ""
				m.applyFilter()
				m.mutateErrMsg = ""
			case "r":
				return m, nil
			case "s":
				sortKeys := []string{"", "name", "status", "group", "cpu", "mem", "age", "pods"}
				next := 0
				for i, k := range sortKeys {
					if k == m.sortKey {
						next = (i + 1) % len(sortKeys)
						break
					}
				}
				m.sortKey = sortKeys[next]
				m.sortAsc = true
				m.applySort()
			case "S":
				if m.sortKey != "" {
					m.sortAsc = !m.sortAsc
					m.applySort()
				}
			case "e":
				if len(m.filtered) > 0 {
					m.execErrMsg = ""
					m.prevState = viewList
					m.state = viewConfirm
				}
			case "c":
				if m.mutator != nil && len(m.filtered) > 0 {
					m.mutateErrMsg = ""
					m.prevState = viewList
					if m.filtered[m.cursor].Cordoned {
						m.confirmAction = "uncordon"
					} else {
						m.confirmAction = "cordon"
					}
					m.state = viewCordonConfirm
				}
			case "d":
				if m.mutator != nil && len(m.filtered) > 0 {
					m.mutateErrMsg = ""
					m.prevState = viewList
					m.confirmAction = "drain"
					m.state = viewDrainConfirm
				}
			case "y":
				if len(m.filtered) > 0 {
					name := m.filtered[m.cursor].Name
					if err := ui.CopyToClipboard(name); err != nil {
						m.clipboardMsg = "copy failed: " + err.Error()
					} else {
						m.clipboardMsg = "✓ copied: " + name
					}
					m.clipboardAt = time.Now()
				}
			case "?":
				m.prevState = viewList
				m.state = viewHelp
			}

		case viewFilter:
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.filter = ""
				m.applyFilter()
				m.state = viewList
			case "enter":
				m.state = viewList
			case "backspace":
				if len(m.filter) > 0 {
					runes := []rune(m.filter)
					m.filter = string(runes[:len(runes)-1])
					m.applyFilter()
				}
			default:
				if len(msg.Runes) > 0 {
					m.filter += string(msg.Runes)
					m.applyFilter()
				}
			}

		case viewInspect:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "esc", "b":
				m.state = viewList
			case "up", "k":
				if m.inspectScroll > 0 {
					m.inspectScroll--
				}
			case "down", "j":
				m.inspectScroll++
			case "n", "]":
				if m.cursor < len(m.filtered)-1 {
					m.cursor++
					m.inspectScroll = 0
					m.inspectLines = m.buildInspectLines()
				}
			case "p", "[":
				if m.cursor > 0 {
					m.cursor--
					m.inspectScroll = 0
					m.inspectLines = m.buildInspectLines()
				}
			case "P":
				if len(m.filtered) > 0 {
					m.prevState = viewInspect
					m.podListScroll = 0
					m.state = viewPodList
				}
			case "e":
				if len(m.filtered) > 0 {
					m.execErrMsg = ""
					m.prevState = viewInspect
					m.state = viewConfirm
				}
			case "c":
				if m.mutator != nil && len(m.filtered) > 0 {
					m.mutateErrMsg = ""
					m.prevState = viewInspect
					if m.filtered[m.cursor].Cordoned {
						m.confirmAction = "uncordon"
					} else {
						m.confirmAction = "cordon"
					}
					m.state = viewCordonConfirm
				}
			case "d":
				if m.mutator != nil && len(m.filtered) > 0 {
					m.mutateErrMsg = ""
					m.prevState = viewInspect
					m.confirmAction = "drain"
					m.state = viewDrainConfirm
				}
			case "y":
				if len(m.filtered) > 0 {
					name := m.filtered[m.cursor].Name
					if err := ui.CopyToClipboard(name); err != nil {
						m.clipboardMsg = "copy failed: " + err.Error()
					} else {
						m.clipboardMsg = "✓ copied: " + name
					}
					m.clipboardAt = time.Now()
				}
			case "?":
				m.prevState = viewInspect
				m.state = viewHelp
			}

		case viewConfirm:
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "y", "Y":
				return m, m.nodeExecCmd()
			case "n", "N", "esc":
				m.state = m.prevState
				m.execErrMsg = ""
			}

		case viewCordonConfirm:
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "y", "Y":
				return m, m.mutateCmd()
			case "n", "N", "esc":
				m.state = m.prevState
				m.mutateErrMsg = ""
			}

		case viewDrainConfirm:
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "y", "Y":
				m.state = viewList
				m.mutateErrMsg = ""
				return m, m.mutateCmd()
			case "n", "N", "esc":
				m.state = m.prevState
				m.mutateErrMsg = ""
			}

		case viewPodList:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "esc", "b":
				m.state = m.prevState
			case "up", "k":
				if m.podListScroll > 0 {
					m.podListScroll--
				}
			case "down", "j":
				m.podListScroll++
			}

		case viewHelp:
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "q", "esc", "?":
				m.state = m.prevState
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	if !m.ready {
		return "\n\n  Loading cluster data...\n"
	}
	switch m.state {
	case viewInspect:
		return m.renderInspect()
	case viewConfirm:
		return m.renderConfirm()
	case viewCordonConfirm:
		return m.renderCordonConfirm()
	case viewDrainConfirm:
		return m.renderDrainConfirm()
	case viewPodList:
		return m.renderPodList()
	case viewHelp:
		return m.renderHelp()
	default:
		return m.renderList()
	}
}

// ── view renderers ────────────────────────────────────────────────────────────

func (m Model) renderList() string {
	vis := m.listVisibleRows()
	start := m.listOffset
	end := start + vis
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	rows := make([]ui.NodeRow, end-start)
	for i, n := range m.filtered[start:end] {
		rows[i] = ui.NodeRow{
			Name:            n.Name,
			Status:          string(n.Status),
			Roles:           n.Roles,
			Age:             n.Age,
			Arch:            n.Arch,
			NodeGroup:       n.NodeGroup,
			CapacityType:    n.CapacityType,
			Instance:        n.Instance,
			Zone:            n.Zone,
			KubeletVersion:  n.KubeletVersion,
			KarpenterStatus: n.KarpenterStatus,
			CPU:             n.CPU,
			Mem:             n.Mem,
			CPUPercent:      n.CPUPercent,
			MemPercent:      n.MemPercent,
			PodCount:        n.PodCount,
			PodCapacity:     n.PodCapacity,
			Cordoned:        n.Cordoned,
		}
	}

	localCursor := m.cursor - m.listOffset
	if localCursor < 0 {
		localCursor = 0
	}

	table := ui.RenderTable(rows, localCursor, m.termWidth, m.sortKey, m.sortAsc)
	filterBar := ui.RenderFilterBar(m.filter, m.state == viewFilter)
	statusBar := m.renderStatusBar()

	scrollInd := ui.ScrollIndicator(m.listOffset, vis, len(m.filtered))
	helpKeys := "  ↑↓/j/k navigate   enter inspect   P pods   e exec   c cordon   d drain   y copy   / filter   s sort   ? help   q quit"
	if scrollInd != "" {
		helpKeys = helpKeys + "  " + scrollInd
	}
	help := ui.HelpStyle.Render(helpKeys)

	var parts []string
	parts = append(parts, table)
	if filterBar != "" {
		parts = append(parts, filterBar)
	}
	if m.errMsg != "" {
		parts = append(parts, ui.ErrorStyle.Render("  ⚠ stale data — "+m.errMsg))
	}
	if m.mutateErrMsg != "" {
		parts = append(parts, ui.ErrorStyle.Render("  "+m.mutateErrMsg))
	}
	if m.execErrMsg != "" {
		parts = append(parts, ui.ErrorStyle.Render("  "+m.execErrMsg))
	}
	if m.clipboardMsg != "" && time.Since(m.clipboardAt) < 3*time.Second {
		parts = append(parts, ui.ClipboardStyle.Render("  "+m.clipboardMsg))
	}
	parts = append(parts, help, statusBar)
	return strings.Join(parts, "\n")
}

func (m Model) renderInspect() string {
	if len(m.filtered) == 0 {
		return "No nodes."
	}
	return ui.RenderInspectPage(
		m.inspectLines,
		m.inspectScroll,
		m.termHeight,
		m.termWidth,
		m.filtered[m.cursor].Name,
		m.cursor,
		len(m.filtered),
	)
}

func (m Model) renderStatusBar() string {
	var ready, notReady, unknown int
	for _, n := range m.nodes {
		switch n.Status {
		case StatusReady, StatusReadySchedulingDisabled:
			ready++
		case StatusNotReady, StatusNotReadySchedulingDisabled:
			notReady++
		default:
			unknown++
		}
	}
	return ui.RenderStatusBar(
		m.clusterName,
		len(m.nodes), ready, notReady, unknown,
		len(m.filtered),
		m.termWidth,
		ui.TableWidth(m.termWidth),
		m.lastFetchTime,
		m.lastFetchErr,
	)
}

func (m Model) renderConfirm() string {
	var bg string
	if m.prevState == viewInspect {
		bg = m.renderInspect()
	} else {
		bg = m.renderList()
	}
	nodeName := ""
	if len(m.filtered) > 0 {
		nodeName = m.filtered[m.cursor].Name
	}
	return ui.RenderConfirm(bg, nodeName, m.debugImage, m.execErrMsg, m.termWidth, m.termHeight)
}

func (m Model) renderCordonConfirm() string {
	var bg string
	if m.prevState == viewInspect {
		bg = m.renderInspect()
	} else {
		bg = m.renderList()
	}
	nodeName := ""
	if len(m.filtered) > 0 {
		nodeName = m.filtered[m.cursor].Name
	}
	return ui.RenderCordonConfirm(bg, nodeName, m.confirmAction, m.mutateErrMsg, m.termWidth, m.termHeight)
}

func (m Model) renderDrainConfirm() string {
	var bg string
	if m.prevState == viewInspect {
		bg = m.renderInspect()
	} else {
		bg = m.renderList()
	}
	nodeName := ""
	if len(m.filtered) > 0 {
		nodeName = m.filtered[m.cursor].Name
	}
	return ui.RenderDrainConfirm(bg, nodeName, m.mutateErrMsg, m.termWidth, m.termHeight)
}

func (m Model) renderPodList() string {
	if len(m.filtered) == 0 {
		return "No nodes."
	}
	n := m.filtered[m.cursor]
	pods := make([]ui.PodRow, len(n.Pods))
	for i, p := range n.Pods {
		pods[i] = ui.PodRow{
			Name:      p.Name,
			Namespace: p.Namespace,
			Phase:     p.Phase,
			Ready:     p.Ready,
			Restarts:  p.Restarts,
			Age:       p.Age,
			Image:     p.Image,
		}
	}
	return ui.RenderPodListPage(pods, m.podListScroll, m.termHeight, m.termWidth, n.Name)
}

func (m Model) renderHelp() string {
	return ui.RenderHelpOverlay("", m.termWidth, m.termHeight)
}

func (m Model) nodeExecCmd() tea.Cmd {
	nodeName := m.filtered[m.cursor].Name
	cmd := nodeexec.NodeDebugCmd(nodeName, m.debugImage, m.debugNamespace)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return execDoneMsg{err: err}
	})
}

func (m Model) mutateCmd() tea.Cmd {
	nodeName := m.filtered[m.cursor].Name
	action := m.confirmAction
	mutator := m.mutator
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var err error
		switch action {
		case "cordon":
			err = mutator.CordonNode(ctx, nodeName)
		case "uncordon":
			err = mutator.UncordonNode(ctx, nodeName)
		case "drain":
			err = mutator.DrainNode(ctx, nodeName)
		}
		return mutateResultMsg{action: action, nodeName: nodeName, err: err}
	}
}

// ── internal helpers ──────────────────────────────────────────────────────────

func (m Model) fetchCmd() tea.Cmd {
	loader := m.loader
	return func() tea.Msg {
		nodes, clusterName, err := loader.FetchNodes()
		return nodesLoadedMsg{nodes: nodes, clusterName: clusterName, err: err}
	}
}

func (m *Model) applyFilter() {
	q := strings.ToLower(m.filter)

	// Label filter syntax: "label:key=value" or "label:key"
	if strings.HasPrefix(q, "label:") {
		kv := strings.TrimPrefix(q, "label:")
		var labelKey, labelVal string
		if idx := strings.Index(kv, "="); idx >= 0 {
			labelKey = kv[:idx]
			labelVal = kv[idx+1:]
		} else {
			labelKey = kv
		}
		var out []Node
		for _, n := range m.nodes {
			if v, ok := n.Labels[labelKey]; ok {
				if labelVal == "" || v == labelVal {
					out = append(out, n)
				}
			}
		}
		m.filtered = out
		m.clampCursor()
		m.applySort()
		m.ensureVisible()
		return
	}

	if q == "" {
		m.filtered = m.nodes
	} else {
		var out []Node
		for _, n := range m.nodes {
			if strings.Contains(strings.ToLower(n.Name), q) ||
				strings.Contains(strings.ToLower(string(n.Status)), q) ||
				strings.Contains(strings.ToLower(n.Roles), q) ||
				strings.Contains(strings.ToLower(n.NodeGroup), q) ||
				strings.Contains(strings.ToLower(n.Zone), q) ||
				strings.Contains(strings.ToLower(n.Instance), q) ||
				strings.Contains(strings.ToLower(n.Arch), q) ||
				strings.Contains(strings.ToLower(n.OSImage), q) {
				out = append(out, n)
			}
		}
		m.filtered = out
	}
	m.clampCursor()
	m.applySort()
	m.ensureVisible()
}

func (m *Model) applySort() {
	if m.sortKey == "" || len(m.filtered) == 0 {
		return
	}
	sort.SliceStable(m.filtered, func(i, j int) bool {
		a, b := m.filtered[i], m.filtered[j]
		var less bool
		switch m.sortKey {
		case "name":
			less = a.Name < b.Name
		case "status":
			less = string(a.Status) < string(b.Status)
		case "group":
			less = a.NodeGroup < b.NodeGroup
		case "cpu":
			less = a.CPUPercent < b.CPUPercent
		case "mem":
			less = a.MemPercent < b.MemPercent
		case "age":
			less = a.CreationTimestamp.Before(b.CreationTimestamp)
		case "pods":
			less = a.PodCount < b.PodCount
		default:
			return false
		}
		if m.sortAsc {
			return less
		}
		return !less
	})
}

func (m *Model) clampCursor() {
	if len(m.filtered) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
}

func (m Model) listVisibleRows() int {
	// header(1) + divider(1) + filter(1) + errs(~1) + help(1) + statusbar(1) = 6
	rows := m.termHeight - 6
	if rows < 3 {
		rows = 3
	}
	return rows
}

func (m *Model) ensureVisible() {
	vis := m.listVisibleRows()
	if m.cursor < m.listOffset {
		m.listOffset = m.cursor
	}
	if m.cursor >= m.listOffset+vis {
		m.listOffset = m.cursor - vis + 1
	}
	maxOff := len(m.filtered) - vis
	if maxOff < 0 {
		maxOff = 0
	}
	if m.listOffset > maxOff {
		m.listOffset = maxOff
	}
	if m.listOffset < 0 {
		m.listOffset = 0
	}
}

func (m Model) buildInspectLines() []string {
	if len(m.filtered) == 0 {
		return nil
	}
	n := m.filtered[m.cursor]

	conditions := make([]ui.ConditionRow, len(n.Conditions))
	for i, c := range n.Conditions {
		conditions[i] = ui.ConditionRow{
			Type:    c.Type,
			Status:  c.Status,
			Reason:  c.Reason,
			Message: c.Message,
		}
	}

	pods := make([]ui.PodRow, len(n.Pods))
	for i, p := range n.Pods {
		pods[i] = ui.PodRow{
			Name:      p.Name,
			Namespace: p.Namespace,
			Phase:     p.Phase,
			Ready:     p.Ready,
			Restarts:  p.Restarts,
			Age:       p.Age,
			Image:     p.Image,
		}
	}

	events := make([]ui.EventRow, len(n.Events))
	for i, e := range n.Events {
		events[i] = ui.EventRow{
			Type:    e.Type,
			Reason:  e.Reason,
			Message: e.Message,
			Count:   e.Count,
			Age:     e.Age,
			Source:  e.Source,
		}
	}

	return ui.BuildInspectLines(ui.NodeDetail{
		NodeRow: ui.NodeRow{
			Name:            n.Name,
			Status:          string(n.Status),
			Roles:           n.Roles,
			Age:             n.Age,
			Arch:            n.Arch,
			NodeGroup:       n.NodeGroup,
			CapacityType:    n.CapacityType,
			Instance:        n.Instance,
			Zone:            n.Zone,
			KubeletVersion:  n.KubeletVersion,
			KarpenterStatus: n.KarpenterStatus,
			CPU:             n.CPU,
			Mem:             n.Mem,
			CPUPercent:      n.CPUPercent,
			MemPercent:      n.MemPercent,
			PodCount:        n.PodCount,
			PodCapacity:     n.PodCapacity,
			Cordoned:        n.Cordoned,
		},
		OSImage:                 n.OSImage,
		KernelVersion:           n.KernelVersion,
		ContainerRuntimeVersion: n.ContainerRuntimeVersion,
		KarpenterReason:         n.KarpenterReason,
		InternalIP:              n.InternalIP,
		ExternalIP:              n.ExternalIP,
		PodCIDR:                 n.PodCIDR,
		Taints:                  n.Taints,
		Labels:                  n.Labels,
		AllocatableCPU:          n.AllocatableCPU,
		AllocatableMem:          n.AllocatableMem,
		Conditions:              conditions,
		Pods:                    pods,
		Events:                  events,
	})
}

func (m Model) logf(format string, args ...interface{}) {
	if m.logger != nil {
		m.logger.Printf(format, args...)
	}
}
