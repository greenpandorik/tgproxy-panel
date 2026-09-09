package api

import "testing"

// TestSampleKeepsFirstAndLast guards against a stride loop that lands short of the last index (e.g.
func TestSampleKeepsFirstAndLast(t *testing.T) {
	for _, n := range []int{1000, 1799} {
		const maxPoints = 600
		items := make([]int, n)
		for i := range items {
			items[i] = i
		}
		out := capSamples(items, maxPoints)
		if len(out) > maxPoints {
			t.Fatalf("n=%d: len(out)=%d exceeds cap %d", n, len(out), maxPoints)
		}
		if len(out) == 0 {
			t.Fatalf("n=%d: got no samples", n)
		}
		if out[0] != items[0] {
			t.Fatalf("n=%d: first item %d, want %d", n, out[0], items[0])
		}
		if out[len(out)-1] != items[len(items)-1] {
			t.Fatalf("n=%d: last item %d, want %d", n, out[len(out)-1], items[len(items)-1])
		}
	}
}

// TestSampleUnderCapIsUnchanged confirms capSamples is a no-op when the input already fits within maxPoints.
func TestSampleUnderCapIsUnchanged(t *testing.T) {
	items := []int{1, 2, 3}
	out := capSamples(items, 600)
	if len(out) != 3 || out[0] != 1 || out[2] != 3 {
		t.Fatalf("unexpected %v", out)
	}
}
