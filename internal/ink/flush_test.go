package ink

import (
	"errors"
	"testing"
	"time"
)

func switchedCoordinator(t *testing.T) (*Coordinator, *State) {
	t.Helper()
	state := NewState("cyan", "magenta")
	coordinator := NewCoordinator(NewCircuit(state))
	if err := coordinator.Activate("cyan"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if _, err := coordinator.SwitchColor("magenta"); err != nil {
		t.Fatalf("switch: %v", err)
	}
	return coordinator, state
}

func TestOpenAloneIsNotPrintable(t *testing.T) {
	coordinator, state := switchedCoordinator(t)
	if ActiveCircuitReady(state, "magenta") {
		t.Fatal("valve open alone must not mark the circuit printable")
	}
	if state.Get("magenta") != CircuitOpen {
		t.Fatalf("expected open circuit, got %s", state.Get("magenta"))
	}
	if _, err := coordinator.BeginFlush(time.Now()); err != nil {
		t.Fatalf("begin flush: %v", err)
	}
	if ActiveCircuitReady(state, "magenta") {
		t.Fatal("flushing without confirmed measurements must not be printable")
	}
}

func TestFlushConfirmsAfterConsecutiveCleanSamples(t *testing.T) {
	coordinator, state := switchedCoordinator(t)
	now := time.Now()
	if _, err := coordinator.BeginFlush(now); err != nil {
		t.Fatalf("begin flush: %v", err)
	}
	// 第一次实测超标，连续计数清零。
	if _, err := coordinator.FlushCycle(40, 30, 2, now); err != nil {
		t.Fatalf("dirty cycle: %v", err)
	}
	if _, err := coordinator.FlushCycle(20, 30, 2, now); err != nil {
		t.Fatalf("clean cycle 1: %v", err)
	}
	if ActiveCircuitReady(state, "magenta") {
		t.Fatal("single clean sample must not confirm the circuit")
	}
	record, err := coordinator.FlushCycle(10, 30, 2, now)
	if err != nil {
		t.Fatalf("clean cycle 2: %v", err)
	}
	if !record.Confirmed || record.Consecutive != 2 || record.Cycles != 3 {
		t.Fatalf("unexpected record after confirmation: %+v", record)
	}
	if !ActiveCircuitReady(state, "magenta") {
		t.Fatal("circuit must be printable after consecutive clean samples")
	}
	if state.Get("magenta") != CircuitReady {
		t.Fatalf("expected ready circuit, got %s", state.Get("magenta"))
	}
}

func TestDirtySampleResetsConsecutiveCount(t *testing.T) {
	coordinator, _ := switchedCoordinator(t)
	now := time.Now()
	if _, err := coordinator.BeginFlush(now); err != nil {
		t.Fatalf("begin flush: %v", err)
	}
	if _, err := coordinator.FlushCycle(10, 30, 2, now); err != nil {
		t.Fatalf("clean cycle: %v", err)
	}
	record, err := coordinator.FlushCycle(90, 30, 2, now)
	if err != nil {
		t.Fatalf("dirty cycle: %v", err)
	}
	if record.Consecutive != 0 || record.Confirmed {
		t.Fatalf("dirty sample must reset consecutive count: %+v", record)
	}
	if len(record.Samples) != 2 || record.Cycles != 2 {
		t.Fatalf("samples and cycles must accumulate across dirty readings: %+v", record)
	}
}

func TestMeasurementInterruptKeepsRecord(t *testing.T) {
	state := NewState("cyan", "magenta")
	circuit := NewCircuit(state)
	coordinator := NewCoordinator(circuit)
	if err := coordinator.Activate("cyan"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if _, err := coordinator.SwitchColor("magenta"); err != nil {
		t.Fatalf("switch: %v", err)
	}
	now := time.Now()
	if _, err := coordinator.BeginFlush(now); err != nil {
		t.Fatalf("begin flush: %v", err)
	}
	if _, err := coordinator.FlushCycle(10, 30, 2, now); err != nil {
		t.Fatalf("clean cycle: %v", err)
	}
	// 测量中断：本次循环不计入，记录原样保留。
	circuit.FailMeasure("magenta", errors.New("sensor offline"))
	if _, err := coordinator.FlushCycle(10, 30, 2, now); err == nil {
		t.Fatal("expected measurement failure")
	}
	record, ok := coordinator.FlushStatus()
	if !ok || record.Cycles != 1 || record.Consecutive != 1 || record.Confirmed {
		t.Fatalf("interrupted record must be kept: %+v", record)
	}
	if state.Get("magenta") != CircuitFlushing {
		t.Fatalf("circuit must stay flushing after interruption, got %s", state.Get("magenta"))
	}
	// 恢复测量后继续沿用同一记录，不从头开始。
	record, err := coordinator.FlushCycle(10, 30, 2, now)
	if err != nil {
		t.Fatalf("resumed cycle: %v", err)
	}
	if !record.Confirmed || record.Cycles != 2 || record.Consecutive != 2 {
		t.Fatalf("resume must continue the kept record: %+v", record)
	}
	if !ActiveCircuitReady(state, "magenta") {
		t.Fatal("circuit must be printable after resumed confirmation")
	}
}

func TestPauseKeepsRecordForResume(t *testing.T) {
	coordinator, state := switchedCoordinator(t)
	now := time.Now()
	if _, err := coordinator.BeginFlush(now); err != nil {
		t.Fatalf("begin flush: %v", err)
	}
	if _, err := coordinator.FlushCycle(10, 30, 2, now); err != nil {
		t.Fatalf("clean cycle: %v", err)
	}
	if err := coordinator.PauseFlush(now); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if state.Get("magenta") != CircuitOpen {
		t.Fatalf("paused circuit must return to open, got %s", state.Get("magenta"))
	}
	record, ok := coordinator.FlushStatus()
	if !ok || !record.Paused || record.Cycles != 1 || record.Consecutive != 1 {
		t.Fatalf("paused record must be kept: %+v", record)
	}
	// 暂停后不能直接当可印。
	if ActiveCircuitReady(state, "magenta") {
		t.Fatal("paused circuit must not be printable")
	}
	// 继续冲洗沿用同一记录。
	if _, err := coordinator.BeginFlush(now); err != nil {
		t.Fatalf("resume flush: %v", err)
	}
	record, err := coordinator.FlushCycle(10, 30, 2, now)
	if err != nil {
		t.Fatalf("resumed cycle: %v", err)
	}
	if !record.Confirmed || record.Cycles != 2 || record.Paused {
		t.Fatalf("resume must continue the kept record: %+v", record)
	}
}

func TestReopenCannotSkipConfirmation(t *testing.T) {
	coordinator, state := switchedCoordinator(t)
	now := time.Now()
	if _, err := coordinator.BeginFlush(now); err != nil {
		t.Fatalf("begin flush: %v", err)
	}
	if _, err := coordinator.FlushCycle(10, 30, 1, now); err != nil {
		t.Fatalf("clean cycle: %v", err)
	}
	if !ActiveCircuitReady(state, "magenta") {
		t.Fatal("expected confirmed circuit")
	}
	// 换走再换回：开路后必须重新冲洗确认。
	if _, err := coordinator.SwitchColor("cyan"); err != nil {
		t.Fatalf("switch away: %v", err)
	}
	if _, err := coordinator.SwitchColor("magenta"); err != nil {
		t.Fatalf("switch back: %v", err)
	}
	if ActiveCircuitReady(state, "magenta") {
		t.Fatal("reopened circuit must not skip flush confirmation")
	}
	if _, ok := coordinator.FlushStatus(); ok {
		t.Fatal("drain must clear the previous flush record")
	}
}

func TestDrainFailureKeepsReadyState(t *testing.T) {
	state := NewState("cyan", "magenta")
	circuit := NewCircuit(state)
	coordinator := NewCoordinator(circuit)
	if err := coordinator.Activate("cyan"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if _, err := coordinator.SwitchColor("magenta"); err != nil {
		t.Fatalf("switch: %v", err)
	}
	now := time.Now()
	if _, err := coordinator.BeginFlush(now); err != nil {
		t.Fatalf("begin flush: %v", err)
	}
	if _, err := coordinator.FlushCycle(10, 30, 1, now); err != nil {
		t.Fatalf("clean cycle: %v", err)
	}
	circuit.FailDrain("magenta", errors.New("isolation signal failed"))
	if err := circuit.Drain("magenta"); err == nil {
		t.Fatal("expected drain failure")
	}
	if state.Get("magenta") != CircuitReady {
		t.Fatalf("failed drain must restore ready state, got %s", state.Get("magenta"))
	}
	if !ActiveCircuitReady(state, "magenta") {
		t.Fatal("confirmed record must survive a failed drain")
	}
}

func TestFlushRejectsInvalidStates(t *testing.T) {
	state := NewState("cyan", "magenta")
	circuit := NewCircuit(state)
	if _, err := circuit.BeginFlush("cyan", time.Now()); err == nil {
		t.Fatal("closed circuit must not start flushing")
	}
	if err := circuit.Open("cyan"); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := circuit.FlushCycle("cyan", 10, 30, 1, time.Now()); err == nil {
		t.Fatal("flush cycle requires begin flush first")
	}
	if err := circuit.PauseFlush("cyan", time.Now()); err == nil {
		t.Fatal("pause requires an active flush")
	}
	if _, err := circuit.FlushCycle("cyan", -1, 30, 1, time.Now()); err == nil {
		t.Fatal("negative residual must be rejected")
	}
}
