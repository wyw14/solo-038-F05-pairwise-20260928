package service

import (
	"context"
	"testing"
	"time"

	"github.com/wyw14/solo-038/internal/ink"
	"github.com/wyw14/solo-038/internal/sensor"
)

func testRuntime(t *testing.T) *Runtime {
	t.Helper()
	runtime, err := NewRuntime(Config{DataDir: t.TempDir(), FestoonCapacity: 120, RegisterGap: 500 * time.Millisecond, RegisterTolerance: 0.25, InkFlushLimitPPM: 30, InkFlushRequiredConsecutive: 2})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	return runtime
}

func TestSwitchInkFlushesAndConfirms(t *testing.T) {
	runtime := testRuntime(t)
	if _, err := runtime.CreateOperation(context.Background(), "switch_ink"); err != nil {
		t.Fatalf("switch_ink: %v", err)
	}
	if active := runtime.inkControl.Active(); active != "magenta" {
		t.Fatalf("expected magenta active, got %s", active)
	}
	if !ink.ActiveCircuitReady(runtime.ink, "magenta") {
		t.Fatal("magenta circuit must be printable after confirmed flush")
	}
	if err := ink.RequireReady(runtime.ink, "cyan"); err != nil {
		t.Fatalf("previous color must be isolated: %v", err)
	}
	record, ok := runtime.ink.FlushRecord("magenta")
	if !ok || !record.Confirmed || record.Consecutive < 2 || len(record.Samples) != record.Cycles {
		t.Fatalf("flush record must hold the confirmed samples: %+v", record)
	}
	// 出口残色实测必须进入传感器历史。
	residuals := 0
	for _, reading := range runtime.sensors.CurrentReadings(time.Now(), time.Minute) {
		if reading.Kind == sensor.ReadingColorResidual {
			residuals++
		}
	}
	if residuals != record.Cycles {
		t.Fatalf("expected %d residual readings, got %d", record.Cycles, residuals)
	}
	// 再换一次：回 cyan，magenta 被隔离。
	if _, err := runtime.CreateOperation(context.Background(), "switch_ink"); err != nil {
		t.Fatalf("second switch_ink: %v", err)
	}
	if !ink.ActiveCircuitReady(runtime.ink, "cyan") {
		t.Fatal("cyan circuit must be printable after confirmed flush")
	}
	if err := ink.RequireReady(runtime.ink, "magenta"); err != nil {
		t.Fatalf("magenta must be isolated after switch: %v", err)
	}
}

func TestSwitchInkResumesInterruptedFlush(t *testing.T) {
	runtime := testRuntime(t)
	// 冲洗途中测量中断：操作失败，本次清洗记录保留。
	if _, err := runtime.CreateOperation(context.Background(), "ink_flush_fault"); err == nil {
		t.Fatal("expected interrupted flush to fail the operation")
	}
	record, ok := runtime.inkControl.FlushStatus()
	if !ok || record.Confirmed || record.Cycles != 1 {
		t.Fatalf("interrupted flush record must be kept: %+v", record)
	}
	if runtime.ink.Get("magenta") != ink.CircuitFlushing {
		t.Fatalf("circuit must stay flushing, got %s", runtime.ink.Get("magenta"))
	}
	if ink.ActiveCircuitReady(runtime.ink, "magenta") {
		t.Fatal("interrupted circuit must not be printable")
	}
	// 再次换墨：沿用同一清洗记录继续冲洗，而不是重新开路。
	if _, err := runtime.CreateOperation(context.Background(), "switch_ink"); err != nil {
		t.Fatalf("resume switch_ink: %v", err)
	}
	if active := runtime.inkControl.Active(); active != "magenta" {
		t.Fatalf("resume must keep flushing magenta, got %s", active)
	}
	record, ok = runtime.inkControl.FlushStatus()
	if !ok || !record.Confirmed {
		t.Fatalf("resumed flush must confirm: %+v", record)
	}
	if record.Cycles <= 1 {
		t.Fatalf("resume must continue the kept record instead of restarting: %+v", record)
	}
	if !ink.ActiveCircuitReady(runtime.ink, "magenta") {
		t.Fatal("magenta circuit must be printable after resumed confirmation")
	}
}
