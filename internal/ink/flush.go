package ink

import (
	"fmt"
	"math"
	"time"
)

// FlushSample 出口残色的一次实测样本。
type FlushSample struct {
	ResidualPPM float64   `json:"residual_ppm"`
	LimitPPM    float64   `json:"limit_ppm"`
	Clean       bool      `json:"clean"`
	MeasuredAt  time.Time `json:"measured_at"`
}

// FlushRecord 本次清洗记录：冲洗循环次数与出口残色实测的累计证据。
// 中途暂停或测量中断后继续沿用同一条记录，只有连续达标被确认，
// 回路才会被标记为可印；重新开路不会清除未确认的记录。
type FlushRecord struct {
	Color       string        `json:"color"`
	Cycles      int           `json:"cycles"`
	Consecutive int           `json:"consecutive"`
	Samples     []FlushSample `json:"samples"`
	Confirmed   bool          `json:"confirmed"`
	Paused      bool          `json:"paused"`
	StartedAt   time.Time     `json:"started_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

func (record FlushRecord) copy() FlushRecord {
	record.Samples = append([]FlushSample(nil), record.Samples...)
	return record
}

// BeginFlush 让已开阀的回路进入冲洗状态。已有未确认的清洗记录时继续沿用，
// 否则开始新一次清洗。
func (circuit *Circuit) BeginFlush(color string, now time.Time) (FlushRecord, error) {
	circuit.mu.Lock()
	defer circuit.mu.Unlock()
	current := circuit.state.Get(color)
	if current != CircuitOpen && current != CircuitFlushing {
		return FlushRecord{}, fmt.Errorf("ink circuit %s cannot flush from %s", color, current)
	}
	record, ok := circuit.state.FlushRecord(color)
	if !ok {
		record = FlushRecord{Color: color, StartedAt: now}
	}
	record.Paused = false
	record.UpdatedAt = now
	circuit.state.putFlushRecord(record)
	circuit.state.Set(color, CircuitFlushing)
	return record.copy(), nil
}

// FlushCycle 执行一次冲洗循环并记录出口残色实测。实测达标计入连续次数，
// 连续达到 required 后回路标记为可印；实测超标只重置连续次数，记录继续沿用。
// 测量中断时返回错误，本次清洗记录保持不变。
func (circuit *Circuit) FlushCycle(color string, residualPPM, limitPPM float64, required int, now time.Time) (FlushRecord, error) {
	circuit.mu.Lock()
	defer circuit.mu.Unlock()
	if circuit.state.Get(color) != CircuitFlushing {
		return FlushRecord{}, fmt.Errorf("ink circuit %s is not flushing", color)
	}
	if limitPPM <= 0 || required < 1 || residualPPM < 0 || math.IsNaN(residualPPM) || math.IsInf(residualPPM, 0) {
		return FlushRecord{}, fmt.Errorf("ink flush measurement is invalid")
	}
	if err := circuit.measureFail[color]; err != nil {
		delete(circuit.measureFail, color)
		return FlushRecord{}, err
	}
	record, ok := circuit.state.FlushRecord(color)
	if !ok {
		return FlushRecord{}, fmt.Errorf("ink circuit %s has no flush record", color)
	}
	record.Cycles++
	clean := residualPPM <= limitPPM
	record.Samples = append(record.Samples, FlushSample{ResidualPPM: residualPPM, LimitPPM: limitPPM, Clean: clean, MeasuredAt: now})
	if clean {
		record.Consecutive++
	} else {
		record.Consecutive = 0
	}
	record.UpdatedAt = now
	if record.Consecutive >= required {
		record.Confirmed = true
		circuit.state.Set(color, CircuitReady)
	}
	circuit.state.putFlushRecord(record)
	return record.copy(), nil
}

// PauseFlush 暂停冲洗：回路回到已开阀状态，本次清洗记录保留，可继续冲洗。
func (circuit *Circuit) PauseFlush(color string, now time.Time) error {
	circuit.mu.Lock()
	defer circuit.mu.Unlock()
	if circuit.state.Get(color) != CircuitFlushing {
		return fmt.Errorf("ink circuit %s is not flushing", color)
	}
	record, ok := circuit.state.FlushRecord(color)
	if !ok {
		return fmt.Errorf("ink circuit %s has no flush record", color)
	}
	record.Paused = true
	record.UpdatedAt = now
	circuit.state.putFlushRecord(record)
	circuit.state.Set(color, CircuitOpen)
	return nil
}
