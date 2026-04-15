package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// ── buildColumns ─────────────────────────────────────────────────────────────

func TestBuildColumns_fullWidth(t *testing.T) {
	cols := buildColumns(250)
	// All 13 columns should be present at 250 chars.
	if len(cols) != 13 {
		t.Errorf("buildColumns(250): got %d cols, want 13", len(cols))
	}
}

func TestBuildColumns_narrow(t *testing.T) {
	cols := buildColumns(60)
	// Very narrow — should drop to minimum 3 columns
	if len(cols) < 3 {
		t.Errorf("buildColumns(60): got %d cols, want >= 3", len(cols))
	}
	// Should still have NAME as first column
	if cols[0].title != "NAME" {
		t.Errorf("buildColumns(60): first col = %q, want NAME", cols[0].title)
	}
}

func TestBuildColumns_nameWidthCapped(t *testing.T) {
	cols := buildColumns(300)
	var nameWidth int
	for _, c := range cols {
		if c.title == "NAME" {
			nameWidth = c.width
			break
		}
	}
	if nameWidth > 45 {
		t.Errorf("NAME column width = %d, want <= 45", nameWidth)
	}
}

func TestBuildColumns_nameGrows(t *testing.T) {
	// At wide widths NAME should absorb extra space beyond its base 30.
	cols := buildColumns(250)
	var nameWidth int
	for _, c := range cols {
		if c.title == "NAME" {
			nameWidth = c.width
			break
		}
	}
	if nameWidth <= 30 {
		t.Errorf("NAME column at 250 width = %d, expected > 30", nameWidth)
	}
}

func TestTableWidth_matchesViewport(t *testing.T) {
	for _, w := range []int{80, 120, 160, 200, 250} {
		if got := TableWidth(w); got != w {
			t.Errorf("TableWidth(%d) = %d, want %d", w, got, w)
		}
	}
}

// ── SortKeyAtX ────────────────────────────────────────────────────────────────

func TestSortKeyAtX_name(t *testing.T) {
	// X=0 should always land on NAME column
	got := SortKeyAtX(0, 160, 0)
	if got != "name" {
		t.Errorf("SortKeyAtX(0, 160, 0) = %q, want name", got)
	}
}

func TestSortKeyAtX_beyondTable(t *testing.T) {
	// X beyond all columns → empty string
	got := SortKeyAtX(10000, 160, 0)
	if got != "" {
		t.Errorf("SortKeyAtX(10000, 160, 0) = %q, want empty", got)
	}
}

func TestSortKeyAtX_nonSortableColumn(t *testing.T) {
	// Find the INSTANCE column start and check it returns ""
	cols := buildColumns(160)
	x := 0
	for i, c := range cols {
		if c.title == "INSTANCE" {
			got := SortKeyAtX(x+1, 160, 0)
			if got != "" {
				t.Errorf("SortKeyAtX on INSTANCE col = %q, want empty", got)
			}
			return
		}
		x += c.width + 1
		_ = i
	}
}

// ── sortKeyForTitle round-trip ────────────────────────────────────────────────

func TestSortKeyForTitle_roundTrip(t *testing.T) {
	sortable := []string{"NAME", "STATUS", "NODEGROUP", "CPU %", "MEM %", "AGE", "PODS"}
	for _, title := range sortable {
		key := sortKeyForTitle(title)
		if key == "" {
			t.Errorf("sortKeyForTitle(%q) = empty, expected a key", title)
			continue
		}
		back := sortKeyToTitle(key)
		if back != title {
			t.Errorf("round-trip %q → %q → %q", title, key, back)
		}
	}
}

// ── ScrollIndicator ───────────────────────────────────────────────────────────

func TestScrollIndicator_partial(t *testing.T) {
	got := ScrollIndicator(11, 30, 87)
	// Should contain 12, 41 (or capped at 87 if exceeds), 87
	if !strings.Contains(got, "12") || !strings.Contains(got, "87") {
		t.Errorf("ScrollIndicator(11,30,87) = %q, expected to contain 12 and 87", got)
	}
}

func TestScrollIndicator_allVisible(t *testing.T) {
	// When all rows fit, no indicator needed
	got := ScrollIndicator(0, 50, 5)
	if got != "" {
		t.Errorf("ScrollIndicator all visible = %q, want empty", got)
	}
}

func TestScrollIndicator_empty(t *testing.T) {
	got := ScrollIndicator(0, 10, 0)
	if got != "" {
		t.Errorf("ScrollIndicator empty = %q, want empty", got)
	}
}

func TestHorizontalIndicator_showsRangeWhenClipped(t *testing.T) {
	got := HorizontalIndicator(1, 80)
	if !strings.Contains(got, "cols:") {
		t.Errorf("HorizontalIndicator(1,80) = %q, want range indicator", got)
	}
}

func TestMaxHorizontalOffset_nonNegative(t *testing.T) {
	if got := MaxHorizontalOffset(120); got < 0 {
		t.Errorf("MaxHorizontalOffset(120) = %d, want >= 0", got)
	}
}

func TestRenderTable_keepsPrimaryColumnsVisible(t *testing.T) {
	rows := []NodeRow{{
		Name:      "node-1",
		Status:    "Ready",
		NodeGroup: "workers",
		Age:       "10m",
	}}
	out := RenderTable(rows, 0, 90, "", true, 1)
	if !strings.Contains(out, "NAME") {
		t.Fatalf("expected NAME header to remain visible, got %q", out)
	}
	if !strings.Contains(out, "STATUS") {
		t.Fatalf("expected STATUS header to remain visible, got %q", out)
	}
	if !strings.Contains(out, "node-1") {
		t.Fatalf("expected node name to remain visible, got %q", out)
	}
	if !strings.Contains(out, "Ready") {
		t.Fatalf("expected status to remain visible, got %q", out)
	}
}

// ── RenderNonTUITable ─────────────────────────────────────────────────────────

func TestRenderNonTUITable_smoke(t *testing.T) {
	rows := []NodeRow{
		{Name: "test-node-1", Status: "Ready", NodeGroup: "workers", Instance: "m5.xlarge", Zone: "us-east-1a", CPUPercent: 50, MemPercent: 60},
		{Name: "test-node-2", Status: "NotReady", NodeGroup: "workers", Instance: "c5.large", Zone: "us-east-1b", CPUPercent: 10, MemPercent: 20},
	}
	var buf bytes.Buffer
	RenderNonTUITable(rows, &buf)
	out := buf.String()

	if !strings.Contains(out, "test-node-1") {
		t.Error("output missing test-node-1")
	}
	if !strings.Contains(out, "test-node-2") {
		t.Error("output missing test-node-2")
	}
	if !strings.Contains(out, "NAME") {
		t.Error("output missing header NAME")
	}
}

// ── RenderNonTUIJSON ──────────────────────────────────────────────────────────

func TestRenderNonTUIJSON_smoke(t *testing.T) {
	data := []map[string]string{{"name": "test-node", "status": "Ready"}}
	var buf bytes.Buffer
	if err := RenderNonTUIJSON(data, &buf); err != nil {
		t.Fatalf("RenderNonTUIJSON error: %v", err)
	}
	if !strings.Contains(buf.String(), "test-node") {
		t.Error("JSON output missing test-node")
	}
}

// ── RenderStatusBar ───────────────────────────────────────────────────────────

func TestRenderStatusBar_noStale(t *testing.T) {
	out := RenderStatusBar("my-cluster", 5, 4, 1, 0, 5, 160, TableWidth(160), time.Now(), false)
	if !strings.Contains(out, "my-cluster") {
		t.Error("status bar missing cluster name")
	}
	if !strings.Contains(out, "5 nodes") {
		t.Error("status bar missing node count")
	}
}

func TestRenderStatusBar_staleError(t *testing.T) {
	out := RenderStatusBar("my-cluster", 5, 4, 1, 0, 5, 160, TableWidth(160), time.Now().Add(-30*time.Second), true)
	if !strings.Contains(out, "stale") {
		t.Error("status bar should indicate stale data")
	}
}

// ── pad / truncate helpers ────────────────────────────────────────────────────

func TestPad_short(t *testing.T) {
	got := pad("hi", 10)
	if len([]rune(got)) != 10 {
		t.Errorf("pad: got len %d, want 10", len([]rune(got)))
	}
}

func TestPad_exact(t *testing.T) {
	got := pad("hello", 5)
	if got != "hello" {
		t.Errorf("pad exact: got %q, want hello", got)
	}
}

func TestPad_truncates(t *testing.T) {
	got := pad("hello world", 5)
	if len([]rune(got)) != 5 {
		t.Errorf("pad truncate: got len %d, want 5", len([]rune(got)))
	}
}

func TestTruncate_long(t *testing.T) {
	got := truncate("hello world", 8)
	if len([]rune(got)) != 8 {
		t.Errorf("truncate: got len %d, want 8", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncate: got %q, want ellipsis suffix", got)
	}
}

func TestTruncate_short(t *testing.T) {
	got := truncate("hi", 10)
	if got != "hi" {
		t.Errorf("truncate short: got %q, want hi", got)
	}
}
