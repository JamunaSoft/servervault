package job

import (
	"reflect"
	"strings"
	"testing"
)

func TestCanTransition(t *testing.T) {
	tests := []struct {
		name string
		from State
		to   State
		want bool
	}{
		{"pending to preparing", StatePending, StatePreparing, true},
		{"pending to cancelled", StatePending, StateCancelled, true},
		{"pending to interrupted", StatePending, StateInterrupted, true},
		{"pending to completed illegal", StatePending, StateCompleted, false},
		{"preparing to dumping", StatePreparing, StateDumping, true},
		{"preparing to backing_up (restore skips dumping)", StatePreparing, StateBackingUp, true},
		{"preparing to verifying (restore-only flow)", StatePreparing, StateVerifying, true},
		{"dumping to backing_up", StateDumping, StateBackingUp, true},
		{"dumping to pending illegal", StateDumping, StatePending, false},
		{"backing_up to verifying", StateBackingUp, StateVerifying, true},
		{"backing_up to completed", StateBackingUp, StateCompleted, true},
		{"verifying to completed", StateVerifying, StateCompleted, true},
		{"verifying to backing_up (backup verifies the dump before restic)", StateVerifying, StateBackingUp, true},
		{"verifying to failed", StateVerifying, StateFailed, true},
		{"completed is terminal", StateCompleted, StatePreparing, false},
		{"failed is terminal", StateFailed, StateCompleted, false},
		{"cancelled is terminal", StateCancelled, StatePreparing, false},
		{"interrupted is terminal", StateInterrupted, StatePreparing, false},
		{"any non-terminal to cancelled", StateDumping, StateCancelled, true},
		{"any non-terminal to interrupted", StateVerifying, StateInterrupted, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanTransition(tt.from, tt.to); got != tt.want {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestState_Terminal(t *testing.T) {
	terminal := []State{StateCompleted, StateFailed, StateCancelled, StateInterrupted}
	for _, s := range terminal {
		if !s.Terminal() {
			t.Errorf("%s.Terminal() = false, want true", s)
		}
	}
	nonTerminal := []State{StatePending, StatePreparing, StateDumping, StateBackingUp, StateVerifying}
	for _, s := range nonTerminal {
		if s.Terminal() {
			t.Errorf("%s.Terminal() = true, want false", s)
		}
	}
}

func TestState_Valid(t *testing.T) {
	if !StatePending.Valid() {
		t.Error("StatePending.Valid() = false, want true")
	}
	if State("bogus").Valid() {
		t.Error(`State("bogus").Valid() = true, want false`)
	}
}

func TestTransitionError_Error(t *testing.T) {
	err := &TransitionError{From: StateCompleted, To: StatePreparing}
	if !strings.Contains(err.Error(), "terminal") {
		t.Errorf("terminal TransitionError message = %q, want it to mention terminal", err.Error())
	}

	err2 := &TransitionError{From: StatePending, To: StateCompleted}
	if strings.Contains(err2.Error(), "terminal") {
		t.Errorf("non-terminal TransitionError message = %q, should not mention terminal", err2.Error())
	}
}

// TestMetadata_NoSecretShapedFields is a regression guard, not a
// functional test: it fails if anyone ever adds a field to Metadata whose
// name suggests it could hold a credential, since Metadata's entire safety
// argument (see job.go's doc comment) rests on it being a closed set of
// known-safe fields with no generic escape hatch.
func TestMetadata_NoSecretShapedFields(t *testing.T) {
	denylist := []string{"password", "secret", "token", "key", "credential", "env"}

	typ := reflect.TypeOf(Metadata{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.ToLower(typ.Field(i).Name)
		for _, bad := range denylist {
			if strings.Contains(name, bad) {
				t.Errorf("Metadata field %q looks secret-shaped (matches %q) -- job history must never persist this", typ.Field(i).Name, bad)
			}
		}
	}

	// Job itself must not gain a generic map-based metadata field either.
	jobType := reflect.TypeOf(Job{})
	for i := 0; i < jobType.NumField(); i++ {
		f := jobType.Field(i)
		if f.Type.Kind() == reflect.Map {
			t.Errorf("Job field %q is a map -- job.Metadata must stay a closed, typed struct, not gain a free-form map escape hatch", f.Name)
		}
	}
}
