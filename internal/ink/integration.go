package ink

import "fmt"

func RequireReady(state *State, colors ...string) error {
	for _, color := range colors {
		if state.Get(color) != CircuitIsolated {
			return fmt.Errorf("ink color %s is not isolated", color)
		}
	}
	return nil
}

// ActiveCircuitReady reports whether a circuit may be used for printing.
// An open valve alone is not enough: the circuit must also carry the
// printable qualification granted by a confirmed cleaning session.
func ActiveCircuitReady(state *State, color string) bool {
	return state.Get(color) == CircuitOpen && state.Printable(color)
}
