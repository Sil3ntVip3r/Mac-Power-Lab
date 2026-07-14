package collector

import (
	"math"
	"testing"
)

func TestFirstRespectsAliasPriorityAndDeterministicTraversal(t *testing.T) {
	root := map[string]any{
		"z-child": map[string]any{"Current": float64(9)},
		"a-child": map[string]any{"Amperage": float64(3), "Current": float64(4)},
	}
	for i := 0; i < 100; i++ {
		if got := asFloat(first(root, "Amperage", "Current")); got != 3 {
			t.Fatalf("iteration %d got %.1f", i, got)
		}
	}
}

func TestNumberRejectsNonFiniteValues(t *testing.T) {
	for _, value := range []any{"NaN", "+Inf", math.Inf(1)} {
		if _, ok := number(value); ok {
			t.Fatalf("accepted non-finite value %v", value)
		}
	}
}
