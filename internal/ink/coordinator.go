package ink

import (
	"fmt"
	"sync"
	"time"
)

type Transition struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Drained   bool   `json:"drained"`
	Flushed   bool   `json:"flushed"`
	Cycles    int    `json:"cycles"`
	Confirmed bool   `json:"confirmed"`
	Opened    bool   `json:"opened"`
}

type Coordinator struct {
	mu      sync.Mutex
	circuit *Circuit
	flusher *Flusher
	active  string
}

func NewCoordinator(circuit *Circuit, flusher *Flusher) *Coordinator {
	return &Coordinator{circuit: circuit, flusher: flusher}
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
	// Initial prime of a fresh loop: no previous color has been drained, so
	// there is no changeover residue to flush out.
	coordinator.circuit.state.MarkPrintable(color)
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
	// Reopening the valve must not skip confirmation: a target circuit that
	// was forced open without a confirmed cleaning is pulled back onto the
	// cleaning path first.
	if coordinator.circuit.state.Get(next) == CircuitOpen && !coordinator.circuit.state.Printable(next) {
		if err := coordinator.circuit.Isolate(next); err != nil {
			return transition, err
		}
	}
	if err := coordinator.circuit.Drain(coordinator.active); err != nil {
		return transition, err
	}
	transition.Drained = true
	// Draining the old color does not prove the loop is clean: flush it and
	// measure the outlet residual until the clean condition holds for the
	// required consecutive readings. An interrupted attempt keeps its
	// cleaning record and resumes here on the next call.
	session, err := coordinator.flusher.EnsureClean(next, time.Now().UTC())
	if err != nil {
		return transition, err
	}
	transition.Flushed = true
	transition.Cycles = session.Cycles
	transition.Confirmed = session.Confirmed
	if err := coordinator.circuit.Open(next); err != nil {
		return transition, err
	}
	transition.Opened = true
	coordinator.active = next
	coordinator.flusher.DiscardExcept(next)
	return transition, nil
}

func (coordinator *Coordinator) Active() string {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	return coordinator.active
}

func (coordinator *Coordinator) CleaningSession(color string) (CleaningSession, bool) {
	return coordinator.flusher.Session(color)
}

func (coordinator *Coordinator) PauseCleaning(color string) {
	coordinator.flusher.Pause(color, time.Now().UTC())
}

func (coordinator *Coordinator) ResumeCleaning(color string) {
	coordinator.flusher.Resume(color, time.Now().UTC())
}

// FailMeasure injects a one-shot outlet measurement fault on the live
// cleaning path, so the next flush cycle of color is interrupted.
func (coordinator *Coordinator) FailMeasure(color string, err error) {
	coordinator.flusher.FailMeasure(color, err)
}
