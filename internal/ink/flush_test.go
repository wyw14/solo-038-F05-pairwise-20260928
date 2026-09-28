package ink

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

var testNow = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

func newTestInk(t *testing.T, threshold float64, streak int, sampler ResidualSampler) (*State, *Circuit, *Flusher, *Coordinator) {
	t.Helper()
	state := NewState("cyan", "magenta", "yellow", "black")
	circuit := NewCircuit(state)
	flusher, err := NewFlusher(state, threshold, streak, sampler)
	if err != nil {
		t.Fatalf("new flusher: %v", err)
	}
	return state, circuit, flusher, NewCoordinator(circuit, flusher)
}

func TestSwitchColorConfirmsCleaningBeforePrintable(t *testing.T) {
	state, _, _, coordinator := newTestInk(t, 0.1, 2, nil)
	if err := coordinator.Activate("cyan"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	transition, err := coordinator.SwitchColor("magenta")
	if err != nil {
		t.Fatalf("switch color: %v", err)
	}
	if !transition.Drained || !transition.Flushed || !transition.Confirmed || !transition.Opened {
		t.Fatalf("transition is incomplete: %+v", transition)
	}
	if transition.Cycles != 3 {
		t.Fatalf("expected 3 flush cycles with the default sampler, got %d", transition.Cycles)
	}
	if coordinator.Active() != "magenta" {
		t.Fatalf("active color is %q", coordinator.Active())
	}
	if !ActiveCircuitReady(state, "magenta") {
		t.Fatalf("magenta circuit must be open and printable after confirmation")
	}
	if err := RequireReady(state, "cyan"); err != nil {
		t.Fatalf("old circuit must stay isolated: %v", err)
	}
	session, ok := coordinator.CleaningSession("magenta")
	if !ok || !session.Confirmed {
		t.Fatalf("cleaning session must be confirmed: %+v", session)
	}
	if session.CleanStreak != 2 || len(session.Measurements) != 3 {
		t.Fatalf("unexpected cleaning record: streak=%d measurements=%d", session.CleanStreak, len(session.Measurements))
	}
	for _, measurement := range session.Measurements {
		if measurement.Unit != "%" || measurement.ObservedAt.IsZero() {
			t.Fatalf("measurement is incomplete: %+v", measurement)
		}
	}
}

func TestSwitchColorRequiresConsecutiveClean(t *testing.T) {
	values := []float64{0.4, 0.05, 0.3, 0.06, 0.04}
	calls := 0
	sampler := func(color string, cycle int) (float64, error) {
		value := values[calls%len(values)]
		calls++
		return value, nil
	}
	state, _, _, coordinator := newTestInk(t, 0.1, 2, sampler)
	if err := coordinator.Activate("cyan"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	transition, err := coordinator.SwitchColor("magenta")
	if err != nil {
		t.Fatalf("switch color: %v", err)
	}
	// 0.4 dirty, 0.05 clean (streak 1), 0.3 dirty (streak reset), 0.06 clean,
	// 0.04 clean (streak 2) — a dirty reading breaks the consecutive run.
	if transition.Cycles != 5 {
		t.Fatalf("expected 5 cycles before confirmation, got %d", transition.Cycles)
	}
	session, _ := coordinator.CleaningSession("magenta")
	if session.CleanStreak != 2 || len(session.Measurements) != 5 {
		t.Fatalf("unexpected record: streak=%d measurements=%d", session.CleanStreak, len(session.Measurements))
	}
	if session.Measurements[1].Clean != true || session.Measurements[2].Clean != false {
		t.Fatalf("clean flags do not track the threshold: %+v", session.Measurements)
	}
	if !ActiveCircuitReady(state, "magenta") {
		t.Fatalf("circuit must be printable after consecutive clean readings")
	}
}

func TestMeasurementInterruptionResumesCleaningRecord(t *testing.T) {
	state, _, flusher, coordinator := newTestInk(t, 0.1, 2, nil)
	if err := coordinator.Activate("cyan"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	flusher.FailMeasure("magenta", fmt.Errorf("outlet sensor signal lost"))
	transition, err := coordinator.SwitchColor("magenta")
	if err == nil {
		t.Fatalf("switch must fail while the outlet measurement is interrupted")
	}
	if !transition.Drained || transition.Flushed || transition.Opened {
		t.Fatalf("transition must stop before opening: %+v", transition)
	}
	if ActiveCircuitReady(state, "magenta") || state.Printable("magenta") {
		t.Fatalf("interrupted cleaning must not qualify the circuit")
	}
	if err := RequireReady(state, "cyan"); err != nil {
		t.Fatalf("old circuit must stay drained after the interruption: %v", err)
	}
	interrupted, ok := coordinator.CleaningSession("magenta")
	if !ok || interrupted.Cycles != 1 || len(interrupted.Measurements) != 0 {
		t.Fatalf("interrupted record must keep its cycle without a measurement: %+v", interrupted)
	}
	// The retry continues this record instead of restarting it.
	transition, err = coordinator.SwitchColor("magenta")
	if err != nil {
		t.Fatalf("resumed switch must succeed: %v", err)
	}
	if !transition.Confirmed || transition.Cycles != 3 {
		t.Fatalf("resumed record must continue the cycle count: %+v", transition)
	}
	session, _ := coordinator.CleaningSession("magenta")
	if len(session.Measurements) != 2 || session.Measurements[0].Cycle != 2 {
		t.Fatalf("measurements must resume from the interrupted cycle: %+v", session.Measurements)
	}
	if !session.StartedAt.Equal(interrupted.StartedAt) {
		t.Fatalf("the same cleaning record must be reused: started %v then %v", interrupted.StartedAt, session.StartedAt)
	}
	if !ActiveCircuitReady(state, "magenta") {
		t.Fatalf("circuit must be printable after the resumed cleaning confirms")
	}
}

func TestPausedCleaningResumesSameRecord(t *testing.T) {
	state, _, _, coordinator := newTestInk(t, 0.1, 2, nil)
	if err := coordinator.Activate("cyan"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	coordinator.PauseCleaning("magenta")
	if _, err := coordinator.SwitchColor("magenta"); !errors.Is(err, ErrCleaningPaused) {
		t.Fatalf("paused cleaning must refuse to advance: %v", err)
	}
	if ActiveCircuitReady(state, "magenta") {
		t.Fatalf("paused cleaning must not qualify the circuit")
	}
	if _, err := coordinator.SwitchColor("magenta"); !errors.Is(err, ErrCleaningPaused) {
		t.Fatalf("retrying while paused must not skip the pause: %v", err)
	}
	coordinator.ResumeCleaning("magenta")
	if _, err := coordinator.SwitchColor("magenta"); err != nil {
		t.Fatalf("resumed cleaning must complete: %v", err)
	}
	session, _ := coordinator.CleaningSession("magenta")
	if session.Paused || !session.Confirmed {
		t.Fatalf("session must be unpaused and confirmed: %+v", session)
	}
}

func TestPauseMidRunKeepsPartialRecord(t *testing.T) {
	state := NewState("cyan", "magenta")
	circuit := NewCircuit(state)
	var flusher *Flusher
	sampler := func(color string, cycle int) (float64, error) {
		if cycle == 2 {
			flusher.Pause(color, testNow)
		}
		return defaultResidualSampler(color, cycle)
	}
	built, err := NewFlusher(state, 0.1, 2, sampler)
	if err != nil {
		t.Fatalf("new flusher: %v", err)
	}
	flusher = built
	coordinator := NewCoordinator(circuit, flusher)
	if err := coordinator.Activate("cyan"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if _, err := coordinator.SwitchColor("magenta"); !errors.Is(err, ErrCleaningPaused) {
		t.Fatalf("mid-run pause must stop the cleaning loop: %v", err)
	}
	partial, _ := coordinator.CleaningSession("magenta")
	if partial.Cycles != 2 || partial.CleanStreak != 1 || len(partial.Measurements) != 2 || !partial.Paused {
		t.Fatalf("partial record must keep cycles and streak: %+v", partial)
	}
	if state.Get("magenta") != CircuitIsolated {
		t.Fatalf("circuit must rest isolated while paused, got %s", state.Get("magenta"))
	}
	coordinator.ResumeCleaning("magenta")
	if _, err := coordinator.SwitchColor("magenta"); err != nil {
		t.Fatalf("resumed cleaning must complete: %v", err)
	}
	session, _ := coordinator.CleaningSession("magenta")
	if session.Cycles != 3 || session.CleanStreak != 2 || len(session.Measurements) != 3 {
		t.Fatalf("resumed record must extend the partial one: %+v", session)
	}
	if !ActiveCircuitReady(state, "magenta") {
		t.Fatalf("circuit must be printable after the resumed cleaning confirms")
	}
}

func TestReopeningCircuitDoesNotSkipConfirmation(t *testing.T) {
	state, circuit, flusher, coordinator := newTestInk(t, 0.1, 2, nil)
	if err := coordinator.Activate("cyan"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	flusher.FailMeasure("magenta", fmt.Errorf("outlet sensor signal lost"))
	if _, err := coordinator.SwitchColor("magenta"); err == nil {
		t.Fatalf("switch must fail while the outlet measurement is interrupted")
	}
	// An operator forces the valve open before the cleaning has confirmed.
	if err := circuit.Open("magenta"); err != nil {
		t.Fatalf("open: %v", err)
	}
	if state.Get("magenta") != CircuitOpen {
		t.Fatalf("valve must read open")
	}
	if ActiveCircuitReady(state, "magenta") {
		t.Fatalf("an open valve without confirmation must not be printable")
	}
	// The next switch pulls the prematurely opened circuit back onto the
	// cleaning path and still requires the full confirmation.
	transition, err := coordinator.SwitchColor("magenta")
	if err != nil {
		t.Fatalf("switch must recover the reopened circuit: %v", err)
	}
	if !transition.Confirmed || transition.Cycles != 3 {
		t.Fatalf("confirmation must still run its cycles: %+v", transition)
	}
	if !ActiveCircuitReady(state, "magenta") {
		t.Fatalf("circuit must be printable only after the confirmed cleaning")
	}
}

func TestStaleCleaningRecordDiscardedAfterSwitch(t *testing.T) {
	_, _, flusher, coordinator := newTestInk(t, 0.1, 2, nil)
	if err := coordinator.Activate("cyan"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	flusher.FailMeasure("magenta", fmt.Errorf("outlet sensor signal lost"))
	if _, err := coordinator.SwitchColor("magenta"); err == nil {
		t.Fatalf("switch must fail while the outlet measurement is interrupted")
	}
	if _, ok := coordinator.CleaningSession("magenta"); !ok {
		t.Fatalf("interrupted record must be retained")
	}
	if _, err := coordinator.SwitchColor("yellow"); err != nil {
		t.Fatalf("switch to another color must succeed: %v", err)
	}
	if _, ok := coordinator.CleaningSession("magenta"); ok {
		t.Fatalf("the interrupted record is stale once the loop carries yellow")
	}
	// Switching back to magenta starts a fresh record: the loop now carries
	// yellow residue, so the interrupted cyan-era record must not be reused.
	transition, err := coordinator.SwitchColor("magenta")
	if err != nil {
		t.Fatalf("switch back to magenta: %v", err)
	}
	session, _ := coordinator.CleaningSession("magenta")
	if transition.Cycles != 3 || session.Measurements[0].Cycle != 1 {
		t.Fatalf("magenta cleaning must start fresh after the loop changed color: %+v", session)
	}
}

func TestDrainIsIdempotentForIsolatedCircuit(t *testing.T) {
	state := NewState("cyan", "magenta")
	circuit := NewCircuit(state)
	if err := circuit.Drain("cyan"); err == nil {
		t.Fatalf("draining a closed circuit must fail")
	}
	if err := circuit.Open("cyan"); err != nil {
		t.Fatalf("open: %v", err)
	}
	state.MarkPrintable("cyan")
	if err := circuit.Drain("cyan"); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if state.Printable("cyan") {
		t.Fatalf("draining must revoke the printable qualification")
	}
	if err := circuit.Drain("cyan"); err != nil {
		t.Fatalf("draining an isolated circuit must be a no-op: %v", err)
	}
}

func TestCannotFlushOpenCircuit(t *testing.T) {
	_, _, flusher, coordinator := newTestInk(t, 0.1, 2, nil)
	if err := coordinator.Activate("cyan"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if _, err := flusher.EnsureClean("cyan", testNow); err == nil {
		t.Fatalf("flushing the live circuit must be refused")
	}
}
