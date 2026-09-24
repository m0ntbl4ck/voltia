package iforest

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
)

// eulerGamma approximates the harmonic numbers in the average path length.
const eulerGamma = 0.5772156649015329

// Config sets the size of the forest. The seed is fixed so that the same data
// always gives the same scores.
type Config struct {
	Trees int
	// SampleSize is how many rows each tree is grown from. Small samples keep
	// the normal rows from crowding out the outliers.
	SampleSize int
	Seed       uint64
}

func DefaultConfig() Config {
	return Config{Trees: 100, SampleSize: 256, Seed: 7}
}

// node is either a split on one feature or, when feature is -1, a leaf.
type node struct {
	feature     int
	split       float64
	size        int // rows of the sample that reached this node
	left, right *node
}

// Forest is a set of isolation trees over rows of the same length.
type Forest struct {
	trees    []*node
	sample   int // rows each tree was grown from
	features int
}

// Fit grows the forest from rows. Every row must have the same, non-zero length.
func Fit(rows [][]float64, cfg Config) (*Forest, error) {
	switch {
	case len(rows) == 0:
		return nil, errors.New("no rows")
	case cfg.Trees < 1:
		return nil, fmt.Errorf("need at least 1 tree, got %d", cfg.Trees)
	case cfg.SampleSize < 2:
		return nil, fmt.Errorf("sample size must be at least 2, got %d", cfg.SampleSize)
	}
	features := len(rows[0])
	if features == 0 {
		return nil, errors.New("rows have no features")
	}
	for i, r := range rows {
		if len(r) != features {
			return nil, fmt.Errorf("row %d has %d features, want %d", i, len(r), features)
		}
	}

	sample := min(cfg.SampleSize, len(rows))
	limit := int(math.Ceil(math.Log2(float64(sample))))
	rng := rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x9e3779b97f4a7c15))
	f := &Forest{trees: make([]*node, cfg.Trees), sample: sample, features: features}
	for i := range f.trees {
		f.trees[i] = grow(rng, drawSample(rng, rows, sample), 0, limit)
	}
	return f, nil
}

// drawSample picks n rows without replacement, or all of them when n is len(rows).
func drawSample(rng *rand.Rand, rows [][]float64, n int) [][]float64 {
	if n == len(rows) {
		return rows
	}
	out := make([][]float64, n)
	for i, j := range rng.Perm(len(rows))[:n] {
		out[i] = rows[j]
	}
	return out
}

func grow(rng *rand.Rand, rows [][]float64, depth, limit int) *node {
	n := &node{feature: -1, size: len(rows)}
	if depth >= limit || len(rows) <= 1 {
		return n
	}
	// Only a feature that varies can split the rows.
	var spread []int
	for f := range rows[0] {
		lo, hi := bounds(rows, f)
		if lo < hi {
			spread = append(spread, f)
		}
	}
	if len(spread) == 0 {
		return n
	}
	n.feature = spread[rng.IntN(len(spread))]
	lo, hi := bounds(rows, n.feature)
	n.split = cut(lo, hi, rng.Float64())

	var left, right [][]float64
	for _, r := range rows {
		if r[n.feature] <= n.split {
			left = append(left, r)
		} else {
			right = append(right, r)
		}
	}
	n.left = grow(rng, left, depth+1, limit)
	n.right = grow(rng, right, depth+1, limit)
	return n
}

// cut places a split u of the way from lo to hi. Rounding can land it exactly
// on hi, which would leave nothing on the right, so it stays just below.
func cut(lo, hi, u float64) float64 {
	if s := lo + (hi-lo)*u; s < hi {
		return s
	}
	return math.Nextafter(hi, lo)
}

func bounds(rows [][]float64, feature int) (lo, hi float64) {
	lo, hi = rows[0][feature], rows[0][feature]
	for _, r := range rows[1:] {
		lo, hi = math.Min(lo, r[feature]), math.Max(hi, r[feature])
	}
	return lo, hi
}

// Score is the anomaly score of x, from 0 to 1. Points that a few random cuts
// isolate score near 1; a score around 0.5 means the forest sees nothing
// unusual. x must have as many features as the rows the forest was fit on.
func (f *Forest) Score(x []float64) float64 {
	var total float64
	for _, t := range f.trees {
		total += pathLength(t, x)
	}
	mean := total / float64(len(f.trees))
	return math.Pow(2, -mean/averagePath(f.sample))
}

// pathLength is the depth at which x ends up, plus the depth a leaf that still
// holds several rows would have reached had it kept splitting.
func pathLength(t *node, x []float64) float64 {
	depth := 0
	for t.feature >= 0 {
		if x[t.feature] <= t.split {
			t = t.left
		} else {
			t = t.right
		}
		depth++
	}
	return float64(depth) + averagePath(t.size)
}

// averagePath is the average path length of an unsuccessful search in a binary
// search tree of n rows, which is what an isolation tree behaves like.
func averagePath(n int) float64 {
	switch {
	case n <= 1:
		return 0
	case n == 2:
		return 1
	}
	return 2*(math.Log(float64(n-1))+eulerGamma) - 2*float64(n-1)/float64(n)
}

// Attribute says how much of the isolation of x each feature explains, as
// shares that add up to 1 (all zeros when no tree could split at all). Every
// cut on x's path narrows the rows that look like x from n to m, and that
// narrowing, ln(n/m), is credited to the feature that was cut. A feature that
// isolates x in few cuts collects most of the credit; a feature on which x is
// ordinary only ever cuts rows that x shares.
func (f *Forest) Attribute(x []float64) []float64 {
	credit := make([]float64, f.features)
	var total float64
	for _, t := range f.trees {
		for t.feature >= 0 {
			next := t.right
			if x[t.feature] <= t.split {
				next = t.left
			}
			gain := math.Log(float64(t.size) / float64(next.size))
			credit[t.feature] += gain
			total += gain
			t = next
		}
	}
	if total == 0 {
		return credit
	}
	for i := range credit {
		credit[i] /= total
	}
	return credit
}
