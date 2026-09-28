package service

import (
	"context"
	"strings"
	"testing"
	"time"
)

func newTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	runtime, err := NewRuntime(Config{DataDir: t.TempDir(), FestoonCapacity: 120, RegisterGap: 500 * time.Millisecond, RegisterTolerance: 0.25, InkResidualThreshold: 0.1, InkCleanStreak: 2})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	return runtime
}

func inkInterlockSatisfied(runtime *Runtime) bool {
	for _, interlock := range runtime.Interlocks() {
		if interlock.Name == "ink_circuit_printable" {
			return interlock.Satisfied
		}
	}
	return false
}

func TestSwitchInkConfirmsCleaningBeforePrintable(t *testing.T) {
	runtime := newTestRuntime(t)
	if err := runtime.ExecuteOperation(context.Background(), "switch_ink"); err != nil {
		t.Fatalf("switch ink: %v", err)
	}
	if runtime.inkControl.Active() != "magenta" {
		t.Fatalf("active color is %q", runtime.inkControl.Active())
	}
	if !inkInterlockSatisfied(runtime) {
		t.Fatalf("ink_circuit_printable interlock must be satisfied after a confirmed cleaning")
	}
	session, ok := runtime.inkControl.CleaningSession("magenta")
	if !ok || !session.Confirmed || session.CleanStreak != 2 {
		t.Fatalf("cleaning session must confirm with the required streak: %+v", session)
	}
}

func TestInkFaultInterruptsMeasurementAndRetryResumesRecord(t *testing.T) {
	runtime := newTestRuntime(t)
	ctx := context.Background()
	if err := runtime.ExecuteOperation(ctx, "switch_ink"); err != nil {
		t.Fatalf("first switch ink: %v", err)
	}
	if err := runtime.ExecuteOperation(ctx, "ink_fault"); err != nil {
		t.Fatalf("ink fault: %v", err)
	}
	err := runtime.ExecuteOperation(ctx, "switch_ink")
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("switch must fail on the interrupted outlet measurement: %v", err)
	}
	if runtime.inkControl.Active() != "magenta" {
		t.Fatalf("active color must stay magenta after the failed switch")
	}
	if runtime.ink.Printable("cyan") {
		t.Fatalf("target circuit must not be printable while its cleaning is incomplete")
	}
	interrupted, ok := runtime.inkControl.CleaningSession("cyan")
	if !ok || interrupted.Cycles != 1 || len(interrupted.Measurements) != 0 {
		t.Fatalf("interrupted cleaning record must be retained: %+v", interrupted)
	}
	// The retry continues the retained record instead of reopening blindly.
	if err := runtime.ExecuteOperation(ctx, "switch_ink"); err != nil {
		t.Fatalf("resumed switch ink: %v", err)
	}
	if runtime.inkControl.Active() != "cyan" {
		t.Fatalf("active color is %q", runtime.inkControl.Active())
	}
	session, _ := runtime.inkControl.CleaningSession("cyan")
	if session.Cycles != 3 || len(session.Measurements) != 2 || session.Measurements[0].Cycle != 2 {
		t.Fatalf("resumed record must continue from the interrupted cycle: %+v", session)
	}
	if !inkInterlockSatisfied(runtime) {
		t.Fatalf("ink_circuit_printable interlock must be satisfied after the resumed cleaning")
	}
	incidents := runtime.Incidents()
	if len(incidents) == 0 {
		t.Fatalf("the interruption must be recorded as an incident")
	}
	found := false
	for _, incident := range incidents {
		if incident.Component == "ink" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an ink incident, got %+v", incidents)
	}
}
