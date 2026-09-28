package ink

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrCleaningPaused is returned when a cleaning session cannot advance
// because an operator pause is in effect. The session record is retained
// and a later EnsureClean call resumes it after Resume.
var ErrCleaningPaused = errors.New("ink cleaning is paused")

// ResidualMeasurement is one measured outlet residual-color sample taken
// after a flush cycle.
type ResidualMeasurement struct {
	Color      string    `json:"color"`
	Cycle      int       `json:"cycle"`
	Value      float64   `json:"value"`
	Unit       string    `json:"unit"`
	Clean      bool      `json:"clean"`
	ObservedAt time.Time `json:"observed_at"`
}

// CleaningSession is the cleaning record for one target color circuit. It
// survives pauses and measurement interruptions: resuming a color change
// always continues this record instead of starting over.
type CleaningSession struct {
	Color          string                `json:"color"`
	Cycles         int                   `json:"cycles"`
	CleanStreak    int                   `json:"clean_streak"`
	RequiredStreak int                   `json:"required_streak"`
	Measurements   []ResidualMeasurement `json:"measurements"`
	Paused         bool                  `json:"paused"`
	Confirmed      bool                  `json:"confirmed"`
	StartedAt      time.Time             `json:"started_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
}

// ResidualSampler measures the outlet residual color after the given flush
// cycle. It is called without the flusher lock held.
type ResidualSampler func(color string, cycle int) (float64, error)

const (
	residualUnit            = "%"
	defaultMaxCyclesPerCall = 12
)

// defaultResidualSampler simulates an outlet sensor: residual pigment decays
// quadratically with each flush cycle.
func defaultResidualSampler(color string, cycle int) (float64, error) {
	if cycle < 1 {
		return 0, fmt.Errorf("flush cycle must be positive")
	}
	return 0.32 / float64(cycle*cycle), nil
}

type Flusher struct {
	mu             sync.Mutex
	state          *State
	threshold      float64
	requiredStreak int
	maxCycles      int
	sample         ResidualSampler
	sessions       map[string]*CleaningSession
	measureFail    map[string]error
}

func NewFlusher(state *State, threshold float64, requiredStreak int, sampler ResidualSampler) (*Flusher, error) {
	if state == nil || threshold <= 0 || requiredStreak < 1 {
		return nil, fmt.Errorf("ink flusher configuration is incomplete")
	}
	if sampler == nil {
		sampler = defaultResidualSampler
	}
	return &Flusher{state: state, threshold: threshold, requiredStreak: requiredStreak, maxCycles: defaultMaxCyclesPerCall, sample: sampler, sessions: map[string]*CleaningSession{}, measureFail: map[string]error{}}, nil
}

// FailMeasure injects a one-shot outlet measurement fault for the next flush
// cycle of color, simulating an interrupted sensor reading.
func (flusher *Flusher) FailMeasure(color string, err error) {
	flusher.mu.Lock()
	defer flusher.mu.Unlock()
	flusher.measureFail[color] = err
}

// Pause holds the cleaning session for color. The record is kept as-is and
// EnsureClean refuses to advance it until Resume.
func (flusher *Flusher) Pause(color string, now time.Time) {
	flusher.mu.Lock()
	defer flusher.mu.Unlock()
	flusher.session(color, now).Paused = true
}

// Resume releases a paused session; the accumulated cycles and clean streak
// carry over.
func (flusher *Flusher) Resume(color string, now time.Time) {
	flusher.mu.Lock()
	defer flusher.mu.Unlock()
	if session, ok := flusher.sessions[color]; ok {
		session.Paused = false
		session.UpdatedAt = now
	}
}

func (flusher *Flusher) Confirmed(color string) bool {
	flusher.mu.Lock()
	defer flusher.mu.Unlock()
	session, ok := flusher.sessions[color]
	return ok && session.Confirmed
}

func (flusher *Flusher) Session(color string) (CleaningSession, bool) {
	flusher.mu.Lock()
	defer flusher.mu.Unlock()
	session, ok := flusher.sessions[color]
	if !ok {
		return CleaningSession{}, false
	}
	return copySession(session), true
}

// DiscardExcept drops every cleaning record except the keeper's. Once a
// color change completes, the loop carries the new ink and any other color's
// record describes a residue that no longer applies.
func (flusher *Flusher) DiscardExcept(keep string) {
	flusher.mu.Lock()
	defer flusher.mu.Unlock()
	for color := range flusher.sessions {
		if color != keep {
			delete(flusher.sessions, color)
		}
	}
}

func (flusher *Flusher) session(color string, now time.Time) *CleaningSession {
	session, ok := flusher.sessions[color]
	if !ok {
		session = &CleaningSession{Color: color, RequiredStreak: flusher.requiredStreak, StartedAt: now, UpdatedAt: now}
		flusher.sessions[color] = session
	}
	return session
}

func copySession(session *CleaningSession) CleaningSession {
	value := *session
	value.Measurements = append([]ResidualMeasurement(nil), session.Measurements...)
	return value
}

// EnsureClean advances the cleaning session for color until the outlet
// residual stays at or below the clean threshold for the required number of
// consecutive measurements, then marks the circuit printable. An existing
// session is always continued, never restarted: paused sessions refuse to
// advance until resumed, and an interrupted measurement keeps the recorded
// cycles and clean streak for the next attempt.
func (flusher *Flusher) EnsureClean(color string, now time.Time) (CleaningSession, error) {
	flusher.mu.Lock()
	session := flusher.session(color, now)
	if session.Confirmed {
		confirmed := copySession(session)
		flusher.mu.Unlock()
		return confirmed, nil
	}
	switch current := flusher.state.Get(color); current {
	case CircuitClosed, CircuitIsolated, CircuitFlushing:
	default:
		flusher.mu.Unlock()
		return copySession(session), fmt.Errorf("ink circuit %s cannot be flushed from %s", color, current)
	}
	// Entering cleaning revokes any stale printable qualification; only a
	// confirmed session may set it again.
	flusher.state.ClearPrintable(color)
	flusher.mu.Unlock()

	for {
		flusher.mu.Lock()
		if session.Paused {
			session.UpdatedAt = now
			paused := copySession(session)
			flusher.mu.Unlock()
			return paused, ErrCleaningPaused
		}
		if session.Cycles >= flusher.maxCycles {
			limit := copySession(session)
			flusher.mu.Unlock()
			return limit, fmt.Errorf("ink circuit %s did not reach the clean condition within %d flush cycles", color, flusher.maxCycles)
		}
		flusher.state.Set(color, CircuitFlushing)
		session.Cycles++
		cycle := session.Cycles
		if err := flusher.measureFail[color]; err != nil {
			delete(flusher.measureFail, color)
			flusher.state.Set(color, CircuitIsolated)
			session.UpdatedAt = now
			interrupted := copySession(session)
			flusher.mu.Unlock()
			return interrupted, fmt.Errorf("outlet residual measurement for %s was interrupted: %w", color, err)
		}
		flusher.mu.Unlock()

		value, err := flusher.sample(color, cycle)

		flusher.mu.Lock()
		if err != nil {
			flusher.state.Set(color, CircuitIsolated)
			session.UpdatedAt = now
			interrupted := copySession(session)
			flusher.mu.Unlock()
			return interrupted, fmt.Errorf("outlet residual measurement for %s was interrupted: %w", color, err)
		}
		clean := value <= flusher.threshold
		session.Measurements = append(session.Measurements, ResidualMeasurement{Color: color, Cycle: cycle, Value: value, Unit: residualUnit, Clean: clean, ObservedAt: now})
		if clean {
			session.CleanStreak++
		} else {
			session.CleanStreak = 0
		}
		session.UpdatedAt = now
		flusher.state.Set(color, CircuitIsolated)
		if session.CleanStreak >= flusher.requiredStreak {
			session.Confirmed = true
			flusher.state.MarkPrintable(color)
			confirmed := copySession(session)
			flusher.mu.Unlock()
			return confirmed, nil
		}
		flusher.mu.Unlock()
	}
}
