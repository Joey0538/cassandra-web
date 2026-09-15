package main

import (
	"errors"
	"testing"
)

// Cassandra cannot set a counter: "counters can only be incremented/
// decremented, not set", and "Conditions on counters are not supported" rules
// out a compare-and-set. Reaching a requested value therefore means reading,
// applying the difference, and checking where it landed.
func TestConvergeCounters_AppliesTheDifference(t *testing.T) {
	stored := []int64{5}

	err := convergeCounters(
		[]int64{12},
		func() ([]int64, error) { return append([]int64(nil), stored...), nil },
		func(deltas []int64) error {
			for i, d := range deltas {
				stored[i] += d
			}
			return nil
		},
		3,
	)
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if stored[0] != 12 {
		t.Fatalf("stored = %d, want 12", stored[0])
	}
}

func TestConvergeCounters_NoWriteWhenAlreadyCorrect(t *testing.T) {
	applied := 0

	err := convergeCounters(
		[]int64{7},
		func() ([]int64, error) { return []int64{7}, nil },
		func([]int64) error { applied++; return nil },
		3,
	)
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if applied != 0 {
		t.Fatalf("applied %d writes, want 0", applied)
	}
}

// Another writer incrementing between the read and the update used to leave the
// counter silently wrong. The result is now re-read and the remaining
// difference applied.
func TestConvergeCounters_RecoversFromAConcurrentWriter(t *testing.T) {
	stored := []int64{5}
	interfered := false

	err := convergeCounters(
		[]int64{12},
		func() ([]int64, error) { return append([]int64(nil), stored...), nil },
		func(deltas []int64) error {
			for i, d := range deltas {
				stored[i] += d
			}
			if !interfered {
				// a concurrent +3 landing in the same window
				interfered = true
				stored[0] += 3
			}
			return nil
		},
		3,
	)
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if stored[0] != 12 {
		t.Fatalf("stored = %d, want 12", stored[0])
	}
}

// A counter under constant contention cannot be pinned, and saying so beats
// reporting a success that did not happen.
func TestConvergeCounters_ReportsPersistentContention(t *testing.T) {
	stored := []int64{0}

	err := convergeCounters(
		[]int64{10},
		func() ([]int64, error) { return append([]int64(nil), stored...), nil },
		func(deltas []int64) error {
			for i, d := range deltas {
				stored[i] += d
			}
			stored[0] += 1 // always moves again
			return nil
		},
		3,
	)
	if err == nil {
		t.Fatal("expected an error when the counter never settles")
	}
}

func TestConvergeCounters_PropagatesFailures(t *testing.T) {
	readErr := errors.New("read boom")
	if err := convergeCounters([]int64{1},
		func() ([]int64, error) { return nil, readErr },
		func([]int64) error { return nil }, 3); !errors.Is(err, readErr) {
		t.Fatalf("read error not propagated: %v", err)
	}

	applyErr := errors.New("apply boom")
	if err := convergeCounters([]int64{1},
		func() ([]int64, error) { return []int64{0}, nil },
		func([]int64) error { return applyErr }, 3); !errors.Is(err, applyErr) {
		t.Fatalf("apply error not propagated: %v", err)
	}
}
