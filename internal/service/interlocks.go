package service

import (
	"sort"
	"time"
)

type Interlock struct {
	Name      string    `json:"name"`
	Satisfied bool      `json:"satisfied"`
	Detail    string    `json:"detail"`
	CheckedAt time.Time `json:"checked_at"`
}

func (runtime *Runtime) Interlocks() []Interlock {
	now := time.Now().UTC()
	_, _, _, threaded, _ := runtime.web.Snapshot()
	status, _ := runtime.dryerControl.Status()
	ready, _, _, fault, _ := runtime.folder.Snapshot()
	activeInk := runtime.inkControl.Active()
	values := []Interlock{
		{Name: "web_threaded", Satisfied: threaded, Detail: "paper web is fenced and threaded", CheckedAt: now},
		{Name: "dryer_ready", Satisfied: status == "ready", Detail: "dryer damper and profile startup are complete", CheckedAt: now},
		{Name: "folder_ready", Satisfied: ready, Detail: fault, CheckedAt: now},
		{Name: "press_stopped_for_splice", Satisfied: runtime.pressControl.CanSplice(), Detail: "current stop epoch has zero-speed proof", CheckedAt: now},
		{Name: "ink_circuit_printable", Satisfied: activeInk != "" && runtime.ink.Printable(activeInk), Detail: "active ink circuit is flushed and the outlet residual is confirmed clean", CheckedAt: now},
	}
	sort.Slice(values, func(left, right int) bool { return values[left].Name < values[right].Name })
	return values
}
