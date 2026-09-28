package ink

import (
	"sort"
	"sync"
)

type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitDraining CircuitState = "draining"
	CircuitIsolated CircuitState = "isolated"
	CircuitFlushing CircuitState = "flushing"
	CircuitOpen     CircuitState = "open"
)

type State struct {
	mu        sync.RWMutex
	circuits  map[string]CircuitState
	printable map[string]bool
}

func NewState(colors ...string) *State {
	state := &State{circuits: map[string]CircuitState{}, printable: map[string]bool{}}
	for _, color := range colors {
		state.circuits[color] = CircuitClosed
		state.printable[color] = false
	}
	return state
}

func (state *State) Set(color string, value CircuitState) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.circuits[color] = value
}

func (state *State) Get(color string) CircuitState {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.circuits[color]
}

func (state *State) Colors() []string {
	state.mu.RLock()
	defer state.mu.RUnlock()
	values := make([]string, 0, len(state.circuits))
	for color := range state.circuits {
		values = append(values, color)
	}
	sort.Strings(values)
	return values
}

// MarkPrintable qualifies a circuit for printing. It is set only after a
// cleaning session has confirmed the outlet is clean (or on the initial
// prime of a fresh loop); opening the valve alone never sets it.
func (state *State) MarkPrintable(color string) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.printable[color] = true
}

func (state *State) ClearPrintable(color string) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.printable[color] = false
}

func (state *State) Printable(color string) bool {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.printable[color]
}
