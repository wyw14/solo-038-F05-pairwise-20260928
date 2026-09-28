package ink

import (
	"fmt"
	"sync"
)

type Circuit struct {
	mu          sync.Mutex
	state       *State
	drainFail   map[string]error
	measureFail map[string]error
}

func NewCircuit(state *State) *Circuit {
	return &Circuit{state: state, drainFail: map[string]error{}, measureFail: map[string]error{}}
}

func (circuit *Circuit) FailDrain(color string, err error) {
	circuit.mu.Lock()
	defer circuit.mu.Unlock()
	circuit.drainFail[color] = err
}

func (circuit *Circuit) FailMeasure(color string, err error) {
	circuit.mu.Lock()
	defer circuit.mu.Unlock()
	circuit.measureFail[color] = err
}

func (circuit *Circuit) Drain(color string) error {
	circuit.mu.Lock()
	defer circuit.mu.Unlock()
	previous := circuit.state.Get(color)
	switch previous {
	case CircuitOpen, CircuitFlushing, CircuitReady:
	default:
		return fmt.Errorf("ink circuit %s cannot drain from %s", color, previous)
	}
	circuit.state.Set(color, CircuitDraining)
	if err := circuit.drainFail[color]; err != nil {
		delete(circuit.drainFail, color)
		circuit.state.Set(color, previous)
		return err
	}
	circuit.state.Set(color, CircuitIsolated)
	circuit.state.clearFlushRecord(color)
	return nil
}

// Open 只负责开阀；新墨是否可用必须经过冲洗确认，开路本身不会标记可印。
func (circuit *Circuit) Open(color string) error {
	current := circuit.state.Get(color)
	if current != CircuitClosed && current != CircuitIsolated {
		return fmt.Errorf("ink circuit %s cannot open from %s", color, current)
	}
	circuit.state.Set(color, CircuitOpen)
	return nil
}
