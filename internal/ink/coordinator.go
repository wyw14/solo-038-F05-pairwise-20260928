package ink

import (
	"fmt"
	"sync"
	"time"
)

type Transition struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Drained bool   `json:"drained"`
	Opened  bool   `json:"opened"`
}

type Coordinator struct {
	mu      sync.Mutex
	circuit *Circuit
	active  string
}

func NewCoordinator(circuit *Circuit) *Coordinator {
	return &Coordinator{circuit: circuit}
}

func (coordinator *Coordinator) Activate(color string) error {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.active != "" {
		return fmt.Errorf("an ink color is already active")
	}
	if err := coordinator.circuit.Open(color); err != nil {
		return err
	}
	coordinator.active = color
	return nil
}

func (coordinator *Coordinator) SwitchColor(next string) (Transition, error) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	transition := Transition{From: coordinator.active, To: next}
	if coordinator.active == "" || next == "" || next == coordinator.active {
		return transition, fmt.Errorf("ink color transition is invalid")
	}
	if err := coordinator.circuit.Drain(coordinator.active); err != nil {
		return transition, err
	}
	transition.Drained = true
	if err := coordinator.circuit.Open(next); err != nil {
		return transition, err
	}
	transition.Opened = true
	coordinator.active = next
	return transition, nil
}

func (coordinator *Coordinator) Active() string {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	return coordinator.active
}

// BeginFlush 开始或继续当前回路的冲洗。
func (coordinator *Coordinator) BeginFlush(now time.Time) (FlushRecord, error) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.active == "" {
		return FlushRecord{}, fmt.Errorf("no ink color is active")
	}
	return coordinator.circuit.BeginFlush(coordinator.active, now)
}

// FlushCycle 对当前回路执行一次冲洗循环并记录出口残色实测。
func (coordinator *Coordinator) FlushCycle(residualPPM, limitPPM float64, required int, now time.Time) (FlushRecord, error) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.active == "" {
		return FlushRecord{}, fmt.Errorf("no ink color is active")
	}
	return coordinator.circuit.FlushCycle(coordinator.active, residualPPM, limitPPM, required, now)
}

// PauseFlush 暂停当前回路的冲洗，保留本次清洗记录。
func (coordinator *Coordinator) PauseFlush(now time.Time) error {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.active == "" {
		return fmt.Errorf("no ink color is active")
	}
	return coordinator.circuit.PauseFlush(coordinator.active, now)
}

// FailMeasure 让当前回路的下一次出口残色实测失败一次（模拟测量中断）。
func (coordinator *Coordinator) FailMeasure(err error) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.active == "" {
		return
	}
	coordinator.circuit.FailMeasure(coordinator.active, err)
}

// FlushStatus 返回当前回路的本次清洗记录。
func (coordinator *Coordinator) FlushStatus() (FlushRecord, bool) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.active == "" {
		return FlushRecord{}, false
	}
	return coordinator.circuit.state.FlushRecord(coordinator.active)
}
