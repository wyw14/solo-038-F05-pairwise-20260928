package sensor

import (
	"fmt"
	"math"
	"time"
)

type ReadingKind string

const (
	ReadingForce         ReadingKind = "force"
	ReadingTension       ReadingKind = "tension"
	ReadingSpeed         ReadingKind = "speed"
	ReadingColorResidual ReadingKind = "color_residual"
)

type Reading struct {
	ID         string      `json:"id"`
	Kind       ReadingKind `json:"kind"`
	Source     string      `json:"source"`
	Origin     string      `json:"origin"`
	Value      float64     `json:"value"`
	Unit       string      `json:"unit"`
	ObservedAt time.Time   `json:"observed_at"`
}

func (reading Reading) Validate() error {
	if reading.ID == "" || reading.Source == "" || reading.Origin == "" {
		return fmt.Errorf("reading identity is incomplete")
	}
	if math.IsNaN(reading.Value) || math.IsInf(reading.Value, 0) {
		return fmt.Errorf("reading value is invalid")
	}
	if reading.ObservedAt.IsZero() {
		return fmt.Errorf("reading timestamp is required")
	}
	return nil
}

func DeriveTension(force Reading, webWidth float64, id string) (Reading, error) {
	if err := force.Validate(); err != nil {
		return Reading{}, err
	}
	if force.Kind != ReadingForce || webWidth <= 0 {
		return Reading{}, fmt.Errorf("force reading and positive web width are required")
	}
	return Reading{ID: id, Kind: ReadingTension, Source: "derived:" + force.Source, Origin: force.Origin, Value: force.Value / webWidth, Unit: "N/mm", ObservedAt: force.ObservedAt}, nil
}
