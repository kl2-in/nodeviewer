package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// NodeRow is the display-layer representation of a node.
// model/ converts Node → NodeRow to avoid circular imports.
type NodeRow struct {
	Name            string
	Status          string
	Roles           string
	Age             string
	Arch            string
	NodeGroup       string
	CapacityType    string // "spot", "on-demand", ""
	Instance        string
	Zone            string
	KubeletVersion  string
	KarpenterStatus string // "", "Blocked", "Disrupting", "Drifted", "Nominated", "Eligible"
	CPU             string
	Mem             string
	CPUPercent      float64
	MemPercent      float64
	PodCount        int
	PodCapacity     int
	Cordoned        bool
}

// NodeDetail holds the full node info for the inspect panel.
type NodeDetail struct {
	NodeRow
	OSImage                 string
	KernelVersion           string
	ContainerRuntimeVersion string
	KarpenterReason         string
	InternalIP              string
	ExternalIP              string
	PodCIDR                 string
	Taints                  []string
	Labels                  map[string]string
	AllocatableCPU          string
	AllocatableMem          string
	Conditions              []ConditionRow
	Pods                    []PodRow
	Events                  []EventRow
}

type ConditionRow struct {
	Type    string
	Status  string
	Reason  string
	Message string
}

// EventRow is a k8s event for the inspect panel.
type EventRow struct {
	Type    string // Normal / Warning
	Reason  string
	Message string
	Count   int32
	Age     string
	Source  string
}

// PodRow is a lightweight pod summary for the inspect panel.
type PodRow struct {
	Name      string
	Namespace string
	Phase     string
	Ready     bool
	Restarts  int32
	Age       string
	Image     string
}

// colDef defines a table column.
type colDef struct {
	title string
	width int // visual character width (no padding)
}

const selBg = lipgloss.Color("#2a3a5a")

// ─── Dynamic column layout ────────────────────────────────────────────────────

// colDef defines a single column: minimum width and whether it can grow.
type colLayout struct {
	colDef
	flex bool // can absorb extra space
}

// baseColumns defines the full table layout.
// Fixed columns have exact widths; flex columns share leftover space.
func buildColumns(termWidth int) []colDef {
	all := []colLayout{
		{colDef{"NAME", 30}, true},
		{colDef{"STATUS", 28}, false},
		{colDef{"NODEGROUP", 18}, false},
		{colDef{"AGE", 4}, false},
		{colDef{"CAPACITY", 9}, false},
		{colDef{"DISRUPTION", 20}, false},
		{colDef{"INSTANCE", 20}, false},
		{colDef{"ZONE", 22}, false},
		{colDef{"ARCH", 6}, false},
		{colDef{"KUBELET", 16}, false},
		{colDef{"CPU %", 13}, false}, // 8-char bar + " " + " 62%"
		{colDef{"MEM %", 13}, false},
		{colDef{"PODS", 7}, false},
	}

	// Distribute any remaining space to flex columns (NAME), capped at 45 chars.
	const maxNameWidth = 45
	used := calcWidth(all)
	extra := termWidth - used
	if extra > 0 {
		for i := range all {
			if all[i].flex {
				cap := maxNameWidth - all[i].width
				if cap > 0 && extra > 0 {
					grow := extra
					if grow > cap {
						grow = cap
					}
					all[i].width += grow
				}
				break
			}
		}
	}

	out := make([]colDef, len(all))
	for i, c := range all {
		out[i] = c.colDef
	}
	return out
}

// TableWidth returns the actual rendered width of the table for a given terminal width.
// Use this instead of termWidth when sizing elements that must align with the table.
func TableWidth(termWidth int) int {
	return termWidth
}

func calcWidth(cols []colLayout) int {
	w := 0
	for i, c := range cols {
		w += c.width
		if i < len(cols)-1 {
			w++ // separator
		}
	}
	return w
}

// ─── Table ───────────────────────────────────────────────────────────────────

// SortKeyAtX returns the sort key for whichever visible column the cursor x falls in, or "".
func SortKeyAtX(x, termWidth int, xOffset int) string {
	selection := selectVisibleColumns(buildColumns(termWidth), termWidth, xOffset)
	pos := 0
	for _, c := range selection.cols {
		if x >= pos && x < pos+c.width {
			return sortKeyForTitle(c.title)
		}
		pos += c.width + 1
	}
	return ""
}

// sortKeyForTitle returns the sort key for a column title, or "" if not sortable.
func sortKeyForTitle(title string) string {
	switch title {
	case "NAME":
		return "name"
	case "STATUS":
		return "status"
	case "NODEGROUP":
		return "group"
	case "CPU %":
		return "cpu"
	case "MEM %":
		return "mem"
	case "AGE":
		return "age"
	case "PODS":
		return "pods"
	}
	return ""
}

// sortKeyToTitle maps a sort key name to the column title it corresponds to.
func sortKeyToTitle(sortKey string) string {
	switch sortKey {
	case "name":
		return "NAME"
	case "status":
		return "STATUS"
	case "group":
		return "NODEGROUP"
	case "cpu":
		return "CPU %"
	case "mem":
		return "MEM %"
	case "age":
		return "AGE"
	case "pods":
		return "PODS"
	}
	return ""
}

func RenderTable(rows []NodeRow, cursor int, termWidth int, sortKey string, sortAsc bool, xOffset int) string {
	selection := selectVisibleColumns(buildColumns(termWidth), termWidth, xOffset)
	visibleCols := selection.cols
	var sb strings.Builder

	// Header
	sortColTitle := sortKeyToTitle(sortKey)
	sortDir := "▲"
	if !sortAsc {
		sortDir = "▼"
	}

	hdrs := make([]string, len(visibleCols))
	for i, c := range visibleCols {
		title := c.title
		if sortKey != "" && c.title == sortColTitle {
			// Append indicator to (possibly truncated) title — always visible.
			runes := []rune(title)
			if len(runes) < c.width {
				title = title + sortDir
			} else {
				title = string(runes[:c.width-1]) + sortDir
			}
		}
		hdrs[i] = HeaderStyle.Render(pad(title, c.width))
	}
	sb.WriteString(strings.Join(hdrs, " ") + "\n")

	// Divider
	tw := 0
	for i, c := range visibleCols {
		tw += c.width
		if i < len(visibleCols)-1 {
			tw++
		}
	}
	divider := DimStyle.Render(strings.Repeat("─", tw))
	sb.WriteString(divider + "\n")

	if len(rows) == 0 {
		sb.WriteString("  " + DimStyle.Render("No nodes match filter") + "\n")
		return sb.String()
	}

	for i, row := range rows {
		sb.WriteString(renderRow(row, i == cursor, visibleCols) + "\n")
	}
	return sb.String()
}

type visibleColumnSelection struct {
	cols            []colDef
	pinnedCount     int
	scrollStart     int
	scrollEnd       int
	totalScrollable int
	maxOffset       int
}

// MaxHorizontalOffset returns the right-most column page offset.
func MaxHorizontalOffset(termWidth int) int {
	selection := selectVisibleColumns(buildColumns(termWidth), termWidth, 0)
	return selection.maxOffset
}

func selectVisibleColumns(allCols []colDef, termWidth int, offset int) visibleColumnSelection {
	selection := visibleColumnSelection{}
	if termWidth <= 0 || len(allCols) == 0 {
		return selection
	}

	totalWidth := 0
	for i, c := range allCols {
		totalWidth += c.width
		if i < len(allCols)-1 {
			totalWidth++
		}
	}
	if totalWidth <= termWidth {
		selection.cols = append(selection.cols, allCols...)
		selection.scrollEnd = len(allCols) - 1
		return selection
	}

	used := 0
	for i := 0; i < len(allCols) && i < 2; i++ {
		needed := allCols[i].width
		if len(selection.cols) > 0 {
			needed++
		}
		if used+needed > termWidth {
			break
		}
		selection.cols = append(selection.cols, allCols[i])
		selection.pinnedCount++
		used += needed
	}

	scrollable := allCols[selection.pinnedCount:]
	selection.totalScrollable = len(scrollable)
	if len(scrollable) == 0 {
		selection.scrollEnd = len(selection.cols) - 1
		return selection
	}
	selection.maxOffset = len(scrollable) - 1
	if offset < 0 {
		offset = 0
	}
	if offset > selection.maxOffset {
		offset = selection.maxOffset
	}
	selection.scrollStart = offset
	selection.scrollEnd = offset - 1

	for i := offset; i < len(scrollable); i++ {
		needed := scrollable[i].width
		if len(selection.cols) > 0 {
			needed++
		}
		if used+needed > termWidth {
			break
		}
		selection.cols = append(selection.cols, scrollable[i])
		used += needed
		selection.scrollEnd = i
	}

	if selection.scrollEnd < selection.scrollStart {
		selection.scrollEnd = selection.scrollStart
	}
	return selection
}

// HorizontalIndicator shows the current visible scrollable column range, or empty when all fit.
func HorizontalIndicator(xOffset, termWidth int) string {
	selection := selectVisibleColumns(buildColumns(termWidth), termWidth, xOffset)
	if selection.maxOffset == 0 || selection.totalScrollable == 0 {
		return ""
	}
	start := selection.scrollStart + 1
	end := selection.scrollEnd + 1
	if end < start {
		end = start
	}
	return DimStyle.Render(fmt.Sprintf("[cols:%d-%d/%d]", start, end, selection.totalScrollable))
}

// colIndex returns the index of the column with the given title, or -1.
func colIndex(cols []colDef, title string) int {
	for i, c := range cols {
		if c.title == title {
			return i
		}
	}
	return -1
}

func renderRow(row NodeRow, selected bool, cols []colDef) string {
	var bg lipgloss.Color
	textFg := colorRowNormal
	bold := false
	if selected {
		bg = selBg
		textFg = colorRowSelected
		bold = true
	}

	text := func(content string, width int) string {
		s := lipgloss.NewStyle().Foreground(textFg).Bold(bold)
		if bg != "" {
			s = s.Background(bg)
		}
		return s.Render(pad(truncate(content, width), width))
	}

	cells := make([]string, len(cols))
	for i, c := range cols {
		switch c.title {
		case "NAME":
			cells[i] = text(row.Name, c.width)
		case "STATUS":
			st := StatusStyle(row.Status).Bold(true)
			if bg != "" {
				st = st.Background(bg)
			}
			cells[i] = st.Render(pad(row.Status, c.width))
		case "NODEGROUP":
			ng := row.NodeGroup
			if ng == "" {
				ng = "-"
			}
			cells[i] = text(ng, c.width)
		case "AGE":
			cells[i] = text(row.Age, c.width)
		case "ARCH":
			cells[i] = text(row.Arch, c.width)
		case "CAPACITY":
			cells[i] = renderCapacity(row.CapacityType, c.width, bg)
		case "DISRUPTION":
			cells[i] = renderKarpenterStatus(row.KarpenterStatus, c.width, bg)
		case "INSTANCE":
			cells[i] = text(row.Instance, c.width)
		case "ZONE":
			cells[i] = text(row.Zone, c.width)
		case "KUBELET":
			cells[i] = text(row.KubeletVersion, c.width)
		case "CPU %":
			cells[i] = barCell(row.CPUPercent, 8, c.width, bg)
		case "MEM %":
			cells[i] = barCell(row.MemPercent, 8, c.width, bg)
		case "PODS":
			cells[i] = text(fmt.Sprintf("%d/%d", row.PodCount, row.PodCapacity), c.width)
		}
	}
	return strings.Join(cells, " ")
}

func renderKarpenterStatusInline(ks string) string {
	return renderKarpenterStatus(ks, len(ks), "")
}

func renderKarpenterStatus(ks string, width int, bg lipgloss.Color) string {
	var s lipgloss.Style
	switch ks {
	case "Blocked":
		s = lipgloss.NewStyle().Foreground(colorNotReady).Bold(true)
	case "Disrupting":
		s = lipgloss.NewStyle().Foreground(colorUnknown).Bold(true)
	case "Drifted":
		s = lipgloss.NewStyle().Foreground(colorUnknown)
	case "Launched", "Registered", "Initialized", "ConsistentStateFound", "Ready":
		s = lipgloss.NewStyle().Foreground(colorHeader)
	case "Nominated":
		s = lipgloss.NewStyle().Foreground(colorHeader)
	case "Eligible":
		s = lipgloss.NewStyle().Foreground(colorReady)
	case "Consolidatable":
		s = lipgloss.NewStyle().Foreground(colorHeader)
	default:
		// Not karpenter-managed — show dim dash
		s = lipgloss.NewStyle().Foreground(colorDim)
		ks = "-"
	}
	if bg != "" {
		s = s.Background(bg)
	}
	return s.Render(pad(ks, width))
}

func renderCapacity(ct string, width int, bg lipgloss.Color) string {
	var s lipgloss.Style
	switch ct {
	case "spot":
		s = lipgloss.NewStyle().Foreground(colorUnknown).Bold(true)
	case "on-demand":
		s = lipgloss.NewStyle().Foreground(colorReady)
	default:
		s = lipgloss.NewStyle().Foreground(colorDim)
		ct = "-"
	}
	if bg != "" {
		s = s.Background(bg)
	}
	return s.Render(pad(ct, width))
}

// ─── Status Bar ──────────────────────────────────────────────────────────────

func RenderStatusBar(clusterName string, total, ready, notReady, unknown, filtered int, termWidth, tableWidth int, lastFetch time.Time, fetchErr bool) string {
	sep := StatusBarStyle.Render("  │  ")

	var parts []string

	if clusterName != "" {
		parts = append(parts,
			ClusterNameStyle.Render("⎈ "+clusterName),
			sep,
		)
	}

	parts = append(parts,
		StatusBarStyle.Render(fmt.Sprintf("%d nodes", total)),
		sep,
		StatusBarReadyStyle.Render(fmt.Sprintf("● %d Ready", ready)),
		sep,
		StatusBarNotReadyStyle.Render(fmt.Sprintf("● %d NotReady", notReady)),
		sep,
		StatusBarUnknownStyle.Render(fmt.Sprintf("● %d Unknown", unknown)),
	)
	if filtered < total {
		parts = append(parts, sep, StatusBarStyle.Render(fmt.Sprintf("filtered: %d/%d", filtered, total)))
	}

	connStr := DimStyle.Background(colorStatusBar).Render("  Connected")
	if fetchErr {
		ts := lastFetch.Format("15:04:05")
		connStr = StatusBarNotReadyStyle.Render(fmt.Sprintf("⚠ stale  last: %s", ts))
	} else if !lastFetch.IsZero() {
		ts := lastFetch.Format("15:04:05")
		connStr = DimStyle.Background(colorStatusBar).Render(fmt.Sprintf("%s  last: %s", "Connected", ts))
	}
	parts = append(parts, sep, connStr)

	content := strings.Join(parts, "")
	return StatusBarStyle.Width(tableWidth).Render(content)
}

// ScrollIndicator returns a compact "[start-end/total]" range string for the list view.
func ScrollIndicator(offset, visible, total int) string {
	if total == 0 {
		return ""
	}
	start := offset + 1
	end := offset + visible
	if end > total {
		end = total
	}
	if start == 1 && end == total {
		return "" // all rows visible — no indicator needed
	}
	return DimStyle.Render(fmt.Sprintf("[%d-%d/%d]", start, end, total))
}

// ─── Filter Bar ──────────────────────────────────────────────────────────────

func RenderFilterBar(query string, active bool) string {
	if active {
		cursor := FilterCursorStyle.Render(" ")
		return FilterActiveStyle.Render("/ "+query) + cursor
	}
	if query != "" {
		return FilterActiveStyle.Render("/ "+query) + DimStyle.Render("  (esc to clear)")
	}
	return ""
}

// ─── Inspect ─────────────────────────────────────────────────────────────────

// BuildInspectLines builds all content lines for the inspect view (no border).
func BuildInspectLines(d NodeDetail) []string {
	var lines []string

	add := func(s string) { lines = append(lines, s) }
	blank := func() { lines = append(lines, "") }

	section := func(title string) {
		add(SectionHeaderStyle.Render(title))
	}
	info := func(key, value string) {
		add("  " + LabelKeyStyle.Width(24).Render(key+":") + "  " + LabelValueStyle.Render(value))
	}

	// ── Core Info ─────────────────────────────────────────
	section("Core Info")
	info("Status", inspectStatusLabel(d.Status, d.Cordoned))
	if d.KarpenterStatus != "" {
		statusRendered := renderKarpenterStatusInline(d.KarpenterStatus)
		if d.KarpenterReason != "" && d.KarpenterReason != d.KarpenterStatus {
			statusRendered += " " + DimStyle.Render("("+d.KarpenterReason+")")
		}
		info("Disruption", statusRendered)
	}
	info("Roles", d.Roles)
	info("Age", d.Age)
	info("Node Group", d.NodeGroup)
	info("Arch", d.Arch)
	info("Zone / AZ", d.Zone)
	info("Instance Type", d.Instance)
	blank()

	// ── Image & Runtime ───────────────────────────────────
	section("Image & Runtime")
	info("OS Image", d.OSImage)
	info("Kernel Version", d.KernelVersion)
	info("Container Runtime", d.ContainerRuntimeVersion)
	info("Kubelet Version", d.NodeRow.KubeletVersion)
	blank()

	// ── Network ───────────────────────────────────────────
	section("Network")
	info("Internal IP", d.InternalIP)
	extIP := d.ExternalIP
	if extIP == "" {
		extIP = "<none>"
	}
	info("External IP", extIP)
	info("Pod CIDR", d.PodCIDR)
	blank()

	// ── Resources ─────────────────────────────────────────
	section("Resources")
	info("CPU (used/total)", d.CPU)
	info("CPU Usage", renderBarInline(d.CPUPercent, 20))
	info("Mem (used/total)", d.Mem)
	info("Mem Usage", renderBarInline(d.MemPercent, 20))
	info("Allocatable CPU", d.AllocatableCPU)
	info("Allocatable Mem", d.AllocatableMem)
	info("Pods", fmt.Sprintf("%d / %d", d.PodCount, d.PodCapacity))
	blank()

	// ── Taints ────────────────────────────────────────────
	section("Taints")
	if len(d.Taints) == 0 {
		add("  " + DimStyle.Render("<none>"))
	} else {
		for _, t := range d.Taints {
			add("  " + TaintStyle.Render("⚠  "+t))
		}
	}
	blank()

	// ── Conditions ────────────────────────────────────────
	section("Conditions")
	add(DimStyle.Render(fmt.Sprintf("  %-22s %-8s %s", "TYPE", "STATUS", "REASON")))
	for _, c := range d.Conditions {
		var sc string
		switch c.Status {
		case "True":
			sc = StatusReadyStyle.Render(c.Status)
		case "False":
			sc = DimStyle.Render(c.Status)
		default:
			sc = StatusUnknownStyle.Render(c.Status)
		}
		add(fmt.Sprintf("  %-22s %s  %s", c.Type, sc, DimStyle.Render(c.Reason)))
	}
	blank()

	// ── Events ────────────────────────────────────────────
	section(fmt.Sprintf("Events  (%d)", len(d.Events)))
	if len(d.Events) == 0 {
		add("  " + DimStyle.Render("<none>"))
	} else {
		add(DimStyle.Render(fmt.Sprintf("  %-8s %-22s %-6s %-18s %s", "TYPE", "REASON", "COUNT", "SOURCE", "MESSAGE")))
		for _, e := range d.Events {
			var typeStr string
			if e.Type == "Warning" {
				typeStr = EventWarningStyle.Render(pad("Warning", 8))
			} else {
				typeStr = EventNormalStyle.Render(pad("Normal", 8))
			}
			countStr := fmt.Sprintf("%-6d", e.Count)
			if e.Count > 10 {
				countStr = StatusNotReadyStyle.Render(fmt.Sprintf("%-6d", e.Count))
			}
			msg := truncate(e.Message, 60)
			add(fmt.Sprintf("  %s %-22s %s %-18s %s",
				typeStr,
				truncate(e.Reason, 22),
				countStr,
				truncate(e.Source, 18),
				DimStyle.Render(msg),
			))
		}
	}
	blank()

	// ── Pods ──────────────────────────────────────────────
	section(fmt.Sprintf("Pods  (%d)", len(d.Pods)))
	if len(d.Pods) == 0 {
		add("  " + DimStyle.Render("<none>"))
	} else {
		add(DimStyle.Render(fmt.Sprintf("  %-42s %-11s %-8s %-8s %-5s %s",
			"NAME", "NAMESPACE", "PHASE", "READY", "RESTART", "AGE")))
		for _, p := range d.Pods {
			readyStr := DimStyle.Render("false")
			if p.Ready {
				readyStr = StatusReadyStyle.Render("true")
			}
			phaseStr := renderPodPhase(p.Phase)
			restartStr := fmt.Sprintf("%d", p.Restarts)
			if p.Restarts > 5 {
				restartStr = StatusNotReadyStyle.Render(restartStr)
			}
			add(fmt.Sprintf("  %-42s %-11s %s  %s  %-5s %s",
				truncate(p.Name, 42),
				truncate(p.Namespace, 11),
				phaseStr,
				readyStr,
				restartStr,
				p.Age,
			))
		}
	}
	blank()

	// ── Labels ────────────────────────────────────────────
	section("Labels")
	keys := make([]string, 0, len(d.Labels))
	for k := range d.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := d.Labels[k]
		if v == "" {
			v = "<present>"
		}
		add("  " + LabelKeyStyle.Render(k+"=") + LabelValueStyle.Render(v))
	}

	return lines
}

// RenderInspectPage renders the full-screen inspect view from pre-built lines.
func RenderInspectPage(
	lines []string,
	scroll, termHeight, termWidth int,
	nodeName string,
	nodeIndex, nodeCount int,
) string {
	const headerLines = 3 // title + sep + blank gap
	const footerLines = 2 // sep + help
	available := termHeight - headerLines - footerLines
	if available < 3 {
		available = 3
	}

	maxScroll := len(lines) - available
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}

	end := scroll + available
	if end > len(lines) {
		end = len(lines)
	}

	sep := InspectSepStyle.Render(strings.Repeat("─", termWidth-1))

	scrollInfo := ""
	if len(lines) > available {
		scrollInfo = DimStyle.Render(fmt.Sprintf("  [%d–%d / %d lines]", scroll+1, end, len(lines)))
	}

	titleLine := "  " +
		InspectTitleStyle.Render(truncate(nodeName, termWidth-28)) +
		"  " +
		InspectNavStyle.Render(fmt.Sprintf("[%d/%d]", nodeIndex+1, nodeCount)) +
		scrollInfo

	help := HelpStyle.Render("  esc/b back   n/] next   p/[ prev   ↑↓/j/k scroll   e exec   q quit")

	visible := strings.Join(lines[scroll:end], "\n")
	return titleLine + "\n" + sep + "\n" + visible + "\n" + sep + "\n" + help
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// barCell renders a usage bar in exactly colWidth visual characters.
// Layout: [barWidth chars bar][1 space][4 chars percent] = barWidth+5
func barCell(pct float64, barWidth int, colWidth int, bg lipgloss.Color) string {
	filled := int(math.Round(pct / 100.0 * float64(barWidth)))
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}

	color := BarColor(pct)
	filledS := lipgloss.NewStyle().Foreground(color)
	emptyS := lipgloss.NewStyle().Foreground(lipgloss.Color("#3a3a3a"))
	if bg != "" {
		filledS = filledS.Background(bg)
		emptyS = emptyS.Background(bg)
	}

	bar := filledS.Render(strings.Repeat("█", filled)) +
		emptyS.Render(strings.Repeat("░", barWidth-filled))

	// pct text: always 4 chars " 62%" / "100%" / "  0%"
	pctTxt := fmt.Sprintf("%4s", fmt.Sprintf("%.0f%%", pct))
	rest := " " + pctTxt // 5 chars
	// pad out to colWidth - barWidth
	rest += strings.Repeat(" ", colWidth-barWidth-5)
	if bg != "" {
		rest = lipgloss.NewStyle().Foreground(colorRowSelected).Background(bg).Render(rest)
	} else {
		rest = lipgloss.NewStyle().Foreground(colorRowNormal).Render(rest)
	}

	return bar + rest
}

func renderBarInline(pct float64, barWidth int) string {
	filled := int(math.Round(pct / 100.0 * float64(barWidth)))
	if filled > barWidth {
		filled = barWidth
	}
	color := BarColor(pct)
	bar := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#3a3a3a")).Render(strings.Repeat("░", barWidth-filled))
	return bar + fmt.Sprintf("  %.1f%%", pct)
}

func renderPodPhase(phase string) string {
	switch phase {
	case "Running":
		return StatusReadyStyle.Render(pad(phase, 8))
	case "Pending":
		return StatusUnknownStyle.Render(pad(phase, 8))
	case "Failed":
		return StatusNotReadyStyle.Render(pad(phase, 8))
	default:
		return DimStyle.Render(pad(phase, 8))
	}
}

// inspectStatusLabel renders the full status string for the inspect panel.
func inspectStatusLabel(status string, _ bool) string {
	return StatusStyle(status).Render(status)
}

func shortenRoles(roles string) string {
	r := strings.ReplaceAll(roles, "control-plane,master", "ctrl-plane")
	r = strings.ReplaceAll(r, "control-plane", "ctrl-plane")
	return r
}

// pad pads s to exactly width rune-counted characters.
func pad(s string, width int) string {
	runes := []rune(s)
	n := len(runes)
	if n >= width {
		return string(runes[:width])
	}
	return s + strings.Repeat(" ", width-n)
}

// truncate shortens s to at most max runes, adding "…" if cut.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// RenderCordonConfirm renders a cordon/uncordon confirmation overlay.
func RenderCordonConfirm(background, nodeName, action, errMsg string, termWidth, termHeight int) string {
	var body strings.Builder
	title := "  Cordon Node"
	msg := "This will mark the node as unschedulable. New pods will not be scheduled here."
	if action == "uncordon" {
		title = "  Uncordon Node"
		msg = "This will mark the node as schedulable again."
	}
	body.WriteString(ConfirmTitleStyle.Foreground(colorCordon).Render(title) + "\n\n")
	body.WriteString("  Node:  " + ConfirmNodeStyle.Render(nodeName) + "\n\n")
	body.WriteString("  " + DimStyle.Render(msg) + "\n\n")
	if errMsg != "" {
		body.WriteString("  " + ErrorStyle.Render("Error: "+errMsg) + "\n\n")
	}
	body.WriteString("  " + ConfirmYesStyle.Render("y") + DimStyle.Render(" confirm   ") +
		ConfirmNoStyle.Render("n / esc") + DimStyle.Render(" cancel"))
	box := CordonBoxStyle.Render(body.String())
	return lipgloss.Place(termWidth, termHeight, lipgloss.Center, lipgloss.Center, box)
}

// RenderDrainConfirm renders a drain confirmation overlay.
func RenderDrainConfirm(background, nodeName, errMsg string, termWidth, termHeight int) string {
	var body strings.Builder
	body.WriteString(DrainWarningStyle.Render("  Drain Node") + "\n\n")
	body.WriteString("  Node:  " + ConfirmNodeStyle.Render(nodeName) + "\n\n")
	body.WriteString("  " + DrainWarningStyle.Render("⚠  This will evict all evictable pods from this node.") + "\n")
	body.WriteString("  " + DimStyle.Render("DaemonSet pods are ignored. Bare pods (no controller) are left in place.") + "\n")
	body.WriteString("  " + DimStyle.Render("PodDisruptionBudgets will be respected.") + "\n\n")
	if errMsg != "" {
		body.WriteString("  " + ErrorStyle.Render("Error: "+errMsg) + "\n\n")
	}
	body.WriteString("  " + ConfirmYesStyle.Render("y") + DimStyle.Render(" drain node   ") +
		ConfirmNoStyle.Render("n / esc") + DimStyle.Render(" cancel"))
	box := DrainBoxStyle.Render(body.String())
	return lipgloss.Place(termWidth, termHeight, lipgloss.Center, lipgloss.Center, box)
}

// RenderHelpOverlay renders a floating keybinding reference over a background.
func RenderHelpOverlay(background string, termWidth, termHeight int) string {
	type kv struct{ key, desc string }
	section := func(title string, pairs []kv) string {
		var sb strings.Builder
		sb.WriteString(HelpOverlaySectionStyle.Render(title) + "\n")
		for _, p := range pairs {
			sb.WriteString("  " + HelpOverlayKeyStyle.Width(16).Render(p.key) + HelpOverlayDescStyle.Render(p.desc) + "\n")
		}
		return sb.String()
	}

	var body strings.Builder
	body.WriteString(HelpOverlayTitleStyle.Render("knv — Keybindings") + "\n\n")
	body.WriteString(section("Node List", []kv{
		{"↑/k  ↓/j", "navigate nodes"},
		{"g  G", "jump to first / last"},
		{"enter / space", "open inspect panel"},
		{"P", "full pod list for node"},
		{"e", "exec into node (kubectl debug)"},
		{"c", "cordon / uncordon node"},
		{"d", "drain node"},
		{"y", "copy node name to clipboard"},
		{"/", "filter  (label:key=val supported)"},
		{"esc", "clear filter"},
		{"s", "cycle sort column"},
		{"S", "toggle sort direction"},
		{"r", "manual refresh"},
		{"?", "toggle this help"},
		{"q / ctrl+c", "quit"},
	}))
	body.WriteString("\n")
	body.WriteString(section("Inspect Panel", []kv{
		{"↑/k  ↓/j", "scroll"},
		{"n/]  p/[", "next / previous node"},
		{"P", "full pod list for node"},
		{"e", "exec into node"},
		{"c", "cordon / uncordon node"},
		{"d", "drain node"},
		{"y", "copy node name to clipboard"},
		{"b / esc", "back to list"},
	}))
	body.WriteString("\n")
	body.WriteString(section("Pod List", []kv{
		{"↑/k  ↓/j", "scroll"},
		{"b / esc", "back"},
	}))

	box := HelpOverlayBoxStyle.Render(body.String())
	return lipgloss.Place(termWidth, termHeight, lipgloss.Center, lipgloss.Center, box)
}

// RenderPodListPage renders a full-screen scrollable pod list for a node.
func RenderPodListPage(pods []PodRow, scroll, termHeight, termWidth int, nodeName string) string {
	const headerLines = 3
	const footerLines = 2
	available := termHeight - headerLines - footerLines
	if available < 3 {
		available = 3
	}

	// Build rows
	var lines []string
	lines = append(lines, DimStyle.Render(fmt.Sprintf(
		"  %-42s %-13s %-8s %-5s %-7s %-5s %s",
		"NAME", "NAMESPACE", "PHASE", "READY", "RESTART", "AGE", "IMAGE",
	)))
	for _, p := range pods {
		readyStr := DimStyle.Render("false")
		if p.Ready {
			readyStr = StatusReadyStyle.Render("true ")
		}
		phaseStr := renderPodPhase(p.Phase)
		restartStr := fmt.Sprintf("%d", p.Restarts)
		if p.Restarts > 5 {
			restartStr = StatusNotReadyStyle.Render(restartStr)
		}
		lines = append(lines, fmt.Sprintf("  %-42s %-13s %s  %s  %-7s %-5s %s",
			truncate(p.Name, 42),
			truncate(p.Namespace, 13),
			phaseStr,
			readyStr,
			restartStr,
			p.Age,
			DimStyle.Render(truncate(p.Image, termWidth-100)),
		))
	}

	maxScroll := len(lines) - available
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	end := scroll + available
	if end > len(lines) {
		end = len(lines)
	}

	sep := InspectSepStyle.Render(strings.Repeat("─", termWidth-1))
	titleLine := "  " + InspectTitleStyle.Render(fmt.Sprintf("Pods on %s", truncate(nodeName, termWidth-20))) +
		"  " + DimStyle.Render(fmt.Sprintf("(%d pods)", len(pods)))
	help := HelpStyle.Render("  b/esc back   ↑↓/j/k scroll   q quit")
	visible := strings.Join(lines[scroll:end], "\n")
	return titleLine + "\n" + sep + "\n" + visible + "\n" + sep + "\n" + help
}

// RenderNonTUITable prints a plain tab-separated node table to w (no ANSI codes).
func RenderNonTUITable(rows []NodeRow, w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSTATUS\tNODEGROUP\tINSTANCE\tZONE\tCAPACITY\tCPU%\tMEM%\tPODS\tKUBELET")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%.0f%%\t%.0f%%\t%d/%d\t%s\n",
			r.Name, r.Status, r.NodeGroup, r.Instance, r.Zone,
			r.CapacityType, r.CPUPercent, r.MemPercent,
			r.PodCount, r.PodCapacity, r.KubeletVersion,
		)
	}
	tw.Flush()
}

// RenderNonTUIJSON marshals nodes as JSON to w.
func RenderNonTUIJSON(v interface{}, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// RenderConfirm renders a confirmation overlay for a node exec action.
// background is the already-rendered list/inspect screen shown behind it.
func RenderConfirm(background, nodeName, image, errMsg string, termWidth, termHeight int) string {
	var body strings.Builder

	body.WriteString(ConfirmTitleStyle.Render("  Debug Node") + "\n\n")
	body.WriteString("  Node:   " + ConfirmNodeStyle.Render(nodeName) + "\n")
	body.WriteString("  Image:  " + ConfirmCmdStyle.Render(image) + "\n\n")
	body.WriteString("  " + ConfirmCmdStyle.Render(
		fmt.Sprintf("kubectl debug node/%s -it --image=%s --profile=sysadmin -- bash", nodeName, image),
	) + "\n\n")

	if errMsg != "" {
		body.WriteString("  " + ErrorStyle.Render("Error: "+errMsg) + "\n\n")
	}

	body.WriteString("  " + ConfirmYesStyle.Render("y") + DimStyle.Render(" launch session   ") +
		ConfirmNoStyle.Render("n / esc") + DimStyle.Render(" cancel"))

	box := ConfirmBoxStyle.Render(body.String())

	// Center the box over the background
	return lipgloss.Place(termWidth, termHeight, lipgloss.Center, lipgloss.Center, box)
}
