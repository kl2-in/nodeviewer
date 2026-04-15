package model

import (
	"testing"
	"time"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func testModel(nodes []Node) Model {
	return Model{
		nodes:      nodes,
		filtered:   nodes,
		sortAsc:    true,
		termWidth:  160,
		termHeight: 40,
	}
}

func makeNodes() []Node {
	now := time.Now()
	return []Node{
		{
			Name:              "node-a",
			Status:            StatusReady,
			NodeGroup:         "workers-prod",
			Zone:              "us-east-1a",
			Instance:          "m5.xlarge",
			Arch:              "amd64",
			CPUPercent:        10.0,
			MemPercent:        20.0,
			PodCount:          5,
			CreationTimestamp: now.Add(-45 * 24 * time.Hour),
			Labels:            map[string]string{"spot": "false", "team": "platform"},
		},
		{
			Name:              "node-b",
			Status:            StatusNotReady,
			NodeGroup:         "workers-spot",
			Zone:              "us-east-1b",
			Instance:          "c5.large",
			Arch:              "amd64",
			CPUPercent:        80.0,
			MemPercent:        90.0,
			PodCount:          20,
			CreationTimestamp: now.Add(-5 * 24 * time.Hour),
			Labels:            map[string]string{"spot": "true", "team": "data"},
		},
		{
			Name:              "node-c",
			Status:            StatusUnknown,
			NodeGroup:         "control-plane",
			Zone:              "us-east-1c",
			Instance:          "m5.large",
			Arch:              "arm64",
			CPUPercent:        50.0,
			MemPercent:        55.0,
			PodCount:          10,
			CreationTimestamp: now.Add(-60 * 24 * time.Hour),
			Labels:            map[string]string{"env": "prod"},
		},
	}
}

// ── applyFilter ───────────────────────────────────────────────────────────────

func TestApplyFilter_empty(t *testing.T) {
	m := testModel(makeNodes())
	m.filter = ""
	m.applyFilter()
	if len(m.filtered) != 3 {
		t.Errorf("empty filter: got %d nodes, want 3", len(m.filtered))
	}
}

func TestApplyFilter_byName(t *testing.T) {
	m := testModel(makeNodes())
	m.filter = "node-a"
	m.applyFilter()
	if len(m.filtered) != 1 || m.filtered[0].Name != "node-a" {
		t.Errorf("name filter: got %v", nodeNames(m.filtered))
	}
}

func TestApplyFilter_byZone(t *testing.T) {
	m := testModel(makeNodes())
	m.filter = "us-east-1b"
	m.applyFilter()
	if len(m.filtered) != 1 || m.filtered[0].Name != "node-b" {
		t.Errorf("zone filter: got %v", nodeNames(m.filtered))
	}
}

func TestApplyFilter_byArch(t *testing.T) {
	m := testModel(makeNodes())
	m.filter = "arm64"
	m.applyFilter()
	if len(m.filtered) != 1 || m.filtered[0].Name != "node-c" {
		t.Errorf("arch filter: got %v", nodeNames(m.filtered))
	}
}

func TestApplyFilter_noMatch(t *testing.T) {
	m := testModel(makeNodes())
	m.filter = "nonexistent"
	m.applyFilter()
	if len(m.filtered) != 0 {
		t.Errorf("no-match filter: got %d nodes, want 0", len(m.filtered))
	}
}

func TestApplyFilter_labelSyntax_keyValue(t *testing.T) {
	m := testModel(makeNodes())
	m.filter = "label:spot=true"
	m.applyFilter()
	if len(m.filtered) != 1 || m.filtered[0].Name != "node-b" {
		t.Errorf("label:key=val filter: got %v", nodeNames(m.filtered))
	}
}

func TestApplyFilter_labelSyntax_keyOnly(t *testing.T) {
	m := testModel(makeNodes())
	m.filter = "label:team"
	m.applyFilter()
	// node-a has team=platform, node-b has team=data
	if len(m.filtered) != 2 {
		t.Errorf("label:key filter: got %d nodes, want 2", len(m.filtered))
	}
}

func TestApplyFilter_labelSyntax_noMatch(t *testing.T) {
	m := testModel(makeNodes())
	m.filter = "label:nonexistent=val"
	m.applyFilter()
	if len(m.filtered) != 0 {
		t.Errorf("label no-match: got %d nodes", len(m.filtered))
	}
}

// ── applySort ─────────────────────────────────────────────────────────────────

func TestApplySort_nameAsc(t *testing.T) {
	m := testModel(makeNodes())
	m.sortKey = "name"
	m.sortAsc = true
	m.applySort()
	if m.filtered[0].Name != "node-a" || m.filtered[2].Name != "node-c" {
		t.Errorf("sort name asc: got %v", nodeNames(m.filtered))
	}
}

func TestApplySort_nameDesc(t *testing.T) {
	m := testModel(makeNodes())
	m.sortKey = "name"
	m.sortAsc = false
	m.applySort()
	if m.filtered[0].Name != "node-c" || m.filtered[2].Name != "node-a" {
		t.Errorf("sort name desc: got %v", nodeNames(m.filtered))
	}
}

func TestApplySort_cpuDesc(t *testing.T) {
	m := testModel(makeNodes())
	m.sortKey = "cpu"
	m.sortAsc = false
	m.applySort()
	// node-b=80%, node-c=50%, node-a=10%
	if m.filtered[0].Name != "node-b" {
		t.Errorf("sort cpu desc: first=%q, want node-b", m.filtered[0].Name)
	}
}

func TestApplySort_memAsc(t *testing.T) {
	m := testModel(makeNodes())
	m.sortKey = "mem"
	m.sortAsc = true
	m.applySort()
	// node-a=20%, node-c=55%, node-b=90%
	if m.filtered[0].Name != "node-a" {
		t.Errorf("sort mem asc: first=%q, want node-a", m.filtered[0].Name)
	}
}

func TestApplySort_ageAsc(t *testing.T) {
	m := testModel(makeNodes())
	m.sortKey = "age"
	m.sortAsc = true
	m.applySort()
	// ascending age = oldest first (smallest timestamp): node-c(-60d) < node-a(-45d) < node-b(-5d)
	if m.filtered[0].Name != "node-c" {
		t.Errorf("sort age asc: first=%q, want node-c", m.filtered[0].Name)
	}
}

func TestApplySort_podsDesc(t *testing.T) {
	m := testModel(makeNodes())
	m.sortKey = "pods"
	m.sortAsc = false
	m.applySort()
	if m.filtered[0].Name != "node-b" {
		t.Errorf("sort pods desc: first=%q, want node-b", m.filtered[0].Name)
	}
}

// ── clampCursor ───────────────────────────────────────────────────────────────

func TestClampCursor_beyondEnd(t *testing.T) {
	m := testModel(makeNodes())
	m.cursor = 100
	m.clampCursor()
	if m.cursor != 2 {
		t.Errorf("clampCursor: got %d, want 2", m.cursor)
	}
}

func TestClampCursor_emptyFiltered(t *testing.T) {
	m := testModel(makeNodes())
	m.filtered = nil
	m.cursor = 5
	m.clampCursor()
	if m.cursor != 0 {
		t.Errorf("clampCursor empty: got %d, want 0", m.cursor)
	}
}

// ── listVisibleRows ───────────────────────────────────────────────────────────

func TestListVisibleRows(t *testing.T) {
	tests := []struct {
		height int
		want   int
	}{
		{40, 34},
		{10, 4},
		{5, 3},
		{1, 3}, // minimum 3
	}
	for _, tt := range tests {
		m := Model{termHeight: tt.height}
		got := m.listVisibleRows()
		if got != tt.want {
			t.Errorf("listVisibleRows(%d) = %d, want %d", tt.height, got, tt.want)
		}
	}
}

// ── ensureVisible ─────────────────────────────────────────────────────────────

func TestEnsureVisible_cursorBelowWindow(t *testing.T) {
	m := testModel(makeNodes())
	m.termHeight = 10 // visible rows = 4
	m.listOffset = 0
	m.cursor = 2 // beyond window
	m.ensureVisible()
	// cursor=2, vis=4 → offset should be 0 (2 < 0+4)
	if m.listOffset != 0 {
		t.Errorf("ensureVisible: listOffset=%d, want 0", m.listOffset)
	}
}

func TestEnsureVisible_cursorAboveWindow(t *testing.T) {
	m := testModel(makeNodes())
	m.termHeight = 10
	m.listOffset = 2
	m.cursor = 0 // above current window
	m.ensureVisible()
	if m.listOffset != 0 {
		t.Errorf("ensureVisible above: listOffset=%d, want 0", m.listOffset)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func nodeNames(nodes []Node) []string {
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name
	}
	return names
}
