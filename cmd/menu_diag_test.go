package cmd

import (
	"strings"
	"testing"
)

func TestDiagPage_OnEnter_RunsOnce(t *testing.T) {
	withCheckStubs(t, okInterface, okRoute, okPing, okResolve, okTCP)

	m := newDiagPageModel()
	updated, cmd := m.onEnter()
	if !updated.running || !updated.loaded {
		t.Fatal("expected running=true, loaded=true after first onEnter")
	}
	if cmd == nil {
		t.Fatal("expected a diagnostics command")
	}

	updated2, cmd2 := updated.onEnter()
	if cmd2 != nil {
		t.Error("expected onEnter to be a no-op after the page has already run once")
	}
	_ = updated2
}

func TestDiagPage_RanMsg_RendersReport(t *testing.T) {
	withCheckStubs(t, okInterface, okRoute, okPing, okResolve, okTCP)

	m := newDiagPageModel()
	m.running = true

	report := runDiagnostics()
	updated, _ := m.Update(diagRanMsg{report: report})
	if updated.running {
		t.Error("expected running=false after diagRanMsg")
	}
	if !strings.Contains(updated.lastOut, "HEALTHY") {
		t.Errorf("expected the rendered report to be stored, got: %q", updated.lastOut)
	}
}

func TestDiagPage_RescanKey(t *testing.T) {
	m := newDiagPageModel()
	m.loaded = true

	updated, cmd := m.Update(keyRune('r'))
	if !updated.running {
		t.Error("expected 'r' to trigger running=true")
	}
	if cmd == nil {
		t.Fatal("expected a diagnostics command")
	}
}

func TestDiagPage_RescanNoopWhileRunning(t *testing.T) {
	m := newDiagPageModel()
	m.running = true

	_, cmd := m.Update(keyRune('r'))
	if cmd != nil {
		t.Error("expected no command while a diagnostic run is already in flight")
	}
}

func TestDiagPage_View_RunningWithNoPriorOutput(t *testing.T) {
	m := newDiagPageModel()
	m.running = true
	view := m.View("<spinner>")
	if !strings.Contains(view, "Executando") {
		t.Errorf("expected a running indicator, got: %q", view)
	}
}
