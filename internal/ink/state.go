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
	CircuitOpen     CircuitState = "open"
	CircuitFlushing CircuitState = "flushing"
	CircuitReady    CircuitState = "ready"
)

type State struct {
	mu       sync.RWMutex
	circuits map[string]CircuitState
	flushes  map[string]FlushRecord
}

func NewState(colors ...string) *State {
	state := &State{circuits: map[string]CircuitState{}, flushes: map[string]FlushRecord{}}
	for _, color := range colors {
		state.circuits[color] = CircuitClosed
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

func (state *State) Snapshot() map[string]CircuitState {
	state.mu.RLock()
	defer state.mu.RUnlock()
	values := make(map[string]CircuitState, len(state.circuits))
	for color, value := range state.circuits {
		values[color] = value
	}
	return values
}

// FlushRecord 返回指定回路的本次清洗记录。
func (state *State) FlushRecord(color string) (FlushRecord, bool) {
	state.mu.RLock()
	defer state.mu.RUnlock()
	record, ok := state.flushes[color]
	return record.copy(), ok
}

// FlushRecords 返回所有回路的清洗记录。
func (state *State) FlushRecords() map[string]FlushRecord {
	state.mu.RLock()
	defer state.mu.RUnlock()
	values := make(map[string]FlushRecord, len(state.flushes))
	for color, record := range state.flushes {
		values[color] = record.copy()
	}
	return values
}

func (state *State) putFlushRecord(record FlushRecord) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.flushes[record.Color] = record.copy()
}

func (state *State) clearFlushRecord(color string) {
	state.mu.Lock()
	defer state.mu.Unlock()
	delete(state.flushes, color)
}
