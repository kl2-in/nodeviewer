package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func init() {
	// Honour the NO_COLOR convention (https://no-color.org) at package init so
	// that any early Render() call (e.g. --output table) is also affected.
	if os.Getenv("NO_COLOR") != "" {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
}

// SetNoColor disables all ANSI color output. Called from main when --no-color
// flag is present. Safe to call multiple times.
func SetNoColor() {
	lipgloss.SetColorProfile(termenv.Ascii)
}

var (
	// Status colors
	colorReady    = lipgloss.Color("#00d787")
	colorNotReady = lipgloss.Color("#ff5f5f")
	colorUnknown  = lipgloss.Color("#ffaf00")

	// UI colors
	colorHeader      = lipgloss.Color("#5f87ff")
	colorRowNormal   = lipgloss.Color("#d0d0d0")
	colorRowSelected = lipgloss.Color("#ffffff")
	colorDim         = lipgloss.Color("#626262")
	colorAccent      = lipgloss.Color("#5f87ff")
	colorPanelBg     = lipgloss.Color("#1c1c1c")
	colorSectionHdr  = lipgloss.Color("#af87ff")
	colorCordon      = lipgloss.Color("#ff875f")
	colorBarLow      = lipgloss.Color("#00d787")
	colorBarMid      = lipgloss.Color("#ffaf00")
	colorBarHigh     = lipgloss.Color("#ff5f5f")
	colorBarEmpty    = lipgloss.Color("#3a3a3a")
	colorFilterBg    = lipgloss.Color("#1a2a1a")
	colorFilterFg    = lipgloss.Color("#afffaf")
	colorStatusBar   = lipgloss.Color("#1a1a2e")
	colorClipboard   = lipgloss.Color("#00d787")
	colorDrain       = lipgloss.Color("#ff5f5f")
	colorHelpOverlay = lipgloss.Color("#1e2030")

	HeaderStyle = lipgloss.NewStyle().
			Foreground(colorHeader).
			Bold(true)

	RowStyle = lipgloss.NewStyle().
			Foreground(colorRowNormal).
			Padding(0, 1)

	SelectedRowStyle = lipgloss.NewStyle().
				Foreground(colorRowSelected).
				Background(lipgloss.Color("#2a3a5a")).
				Bold(true).
				Padding(0, 1)

	StatusReadyStyle = lipgloss.NewStyle().
				Foreground(colorReady).
				Bold(true)

	StatusNotReadyStyle = lipgloss.NewStyle().
				Foreground(colorNotReady).
				Bold(true)

	StatusUnknownStyle = lipgloss.NewStyle().
				Foreground(colorUnknown).
				Bold(true)

	CordonStyle = lipgloss.NewStyle().
			Foreground(colorCordon).
			Bold(true)

	DimStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	HelpStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			Italic(true)

	FilterActiveStyle = lipgloss.NewStyle().
				Foreground(colorFilterFg).
				Background(colorFilterBg).
				Bold(true).
				Padding(0, 1)

	FilterPromptStyle = lipgloss.NewStyle().
				Foreground(colorFilterFg).
				Bold(true)

	FilterCursorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#000000")).
				Background(colorFilterFg)

	StatusBarStyle = lipgloss.NewStyle().
			Background(colorStatusBar).
			Foreground(colorDim).
			Padding(0, 1)

	StatusBarReadyStyle = lipgloss.NewStyle().
				Background(colorStatusBar).
				Foreground(colorReady).
				Bold(true)

	StatusBarNotReadyStyle = lipgloss.NewStyle().
				Background(colorStatusBar).
				Foreground(colorNotReady).
				Bold(true)

	StatusBarUnknownStyle = lipgloss.NewStyle().
				Background(colorStatusBar).
				Foreground(colorUnknown).
				Bold(true)

	// Inspect panel styles
	InspectTitleStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	InspectNavStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	InspectSepStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#2a3a5a"))

	SectionHeaderStyle = lipgloss.NewStyle().
				Foreground(colorSectionHdr).
				Bold(true)

	LabelKeyStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	LabelValueStyle = lipgloss.NewStyle().
			Foreground(colorRowNormal)

	TaintStyle = lipgloss.NewStyle().
			Foreground(colorUnknown)

	EventWarningStyle = lipgloss.NewStyle().
				Foreground(colorNotReady).
				Bold(true)

	EventNormalStyle = lipgloss.NewStyle().
				Foreground(colorDim)

	InspectPanelBg = lipgloss.NewStyle().
			Background(colorPanelBg)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff5f5f")).
			Bold(true)

	ClusterNameStyle = lipgloss.NewStyle().
				Background(colorStatusBar).
				Foreground(colorAccent).
				Bold(true)

	// Clipboard feedback
	ClipboardStyle = lipgloss.NewStyle().
			Foreground(colorClipboard).
			Bold(true)

	// Confirm overlay
	ConfirmBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorUnknown).
			Padding(1, 3).
			Background(lipgloss.Color("#1a1a00"))

	ConfirmTitleStyle = lipgloss.NewStyle().
				Foreground(colorUnknown).
				Bold(true)

	ConfirmNodeStyle = lipgloss.NewStyle().
				Foreground(colorRowSelected).
				Bold(true)

	ConfirmCmdStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			Italic(true)

	ConfirmYesStyle = lipgloss.NewStyle().
			Foreground(colorReady).
			Bold(true)

	ConfirmNoStyle = lipgloss.NewStyle().
			Foreground(colorNotReady).
			Bold(true)

	// Cordon confirm overlay
	CordonBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorCordon).
			Padding(1, 3).
			Background(lipgloss.Color("#1a0f00"))

	// Drain confirm overlay
	DrainBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDrain).
			Padding(1, 3).
			Background(lipgloss.Color("#1a0000"))

	DrainWarningStyle = lipgloss.NewStyle().
				Foreground(colorDrain).
				Bold(true)

	// Help overlay
	HelpOverlayBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorHeader).
				Padding(1, 3).
				Background(colorHelpOverlay)

	HelpOverlayTitleStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	HelpOverlaySectionStyle = lipgloss.NewStyle().
				Foreground(colorSectionHdr).
				Bold(true)

	HelpOverlayKeyStyle = lipgloss.NewStyle().
				Foreground(colorUnknown).
				Bold(true)

	HelpOverlayDescStyle = lipgloss.NewStyle().
				Foreground(colorRowNormal)
)

func StatusStyle(status string) lipgloss.Style {
	switch status {
	case "Ready":
		return StatusReadyStyle
	case "NotReady":
		return StatusNotReadyStyle
	case "SchedulingDisabled", "Ready,SchedulingDisabled",
		"NotReady,SchedulingDisabled", "Unknown,SchedulingDisabled":
		return CordonStyle
	default:
		return StatusUnknownStyle
	}
}

// BarColor returns a color based on usage percentage.
func BarColor(pct float64) lipgloss.Color {
	switch {
	case pct >= 85:
		return colorBarHigh
	case pct >= 60:
		return colorBarMid
	default:
		return colorBarLow
	}
}
