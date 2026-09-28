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

// ActiveCircuitReady 判定回路是否可印：阀门打开不算数，
// 必须冲洗确认完成（状态 ready 且本次清洗记录已确认）。
func ActiveCircuitReady(state *State, color string) bool {
	if state.Get(color) != CircuitReady {
		return false
	}
	record, ok := state.FlushRecord(color)
	return ok && record.Confirmed
}
