package iforest

import (
	"math"
	"math/rand/v2"
	"testing"
)

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %.12f, want %.12f", name, got, want)
	}
}

// Values from an independent computation of 2(ln(n-1)+γ) - 2(n-1)/n.
func TestAveragePath(t *testing.T) {
	near(t, "c(0)", averagePath(0), 0)
	near(t, "c(1)", averagePath(1), 0)
	near(t, "c(2)", averagePath(2), 1)
	near(t, "c(3)", averagePath(3), 1.207392357589623)
	near(t, "c(4)", averagePath(4), 1.8516559071392855)
	near(t, "c(256)", averagePath(256), 10.244770920119917)
}

// One tree over four rows: it splits feature 0 at 5 and leaves one row on the
// left and three on the right. A point on the left is isolated at depth 1, so
// its path is 1 + c(1); on the right it is 1 + c(3). Both are scaled by c(4).
func TestScoreOnAHandBuiltTree(t *testing.T) {
	tree := &node{feature: 0, split: 5, size: 4,
		left:  &node{feature: -1, size: 1},
		right: &node{feature: -1, size: 3},
	}
	f := &Forest{trees: []*node{tree}, sample: 4, features: 2}
	near(t, "left", f.Score([]float64{1, 0}), 0.6877436677788327)
	near(t, "right", f.Score([]float64{9, 0}), 0.4376598631629993)
	near(t, "on the split", f.Score([]float64{5, 0}), 0.6877436677788327)
}

// A second tree splits feature 1 at 2 into two leaves of two rows. The path
// of (1, 9) is 1 in the first tree and 1 + c(2) = 2 in the second.
func TestScoreAveragesTheTrees(t *testing.T) {
	first := &node{feature: 0, split: 5, size: 4,
		left:  &node{feature: -1, size: 1},
		right: &node{feature: -1, size: 3},
	}
	second := &node{feature: 1, split: 2, size: 4,
		left:  &node{feature: -1, size: 2},
		right: &node{feature: -1, size: 2},
	}
	f := &Forest{trees: []*node{first, second}, sample: 4, features: 2}
	near(t, "score", f.Score([]float64{1, 9}), 0.5703479706671019)
}

func TestFitRejectsBadInput(t *testing.T) {
	ok := [][]float64{{1, 2}, {3, 4}}
	cases := map[string]struct {
		rows [][]float64
		cfg  Config
	}{
		"no rows":         {nil, DefaultConfig()},
		"no trees":        {ok, Config{Trees: 0, SampleSize: 4}},
		"tiny sample":     {ok, Config{Trees: 1, SampleSize: 1}},
		"no features":     {[][]float64{{}, {}}, DefaultConfig()},
		"ragged rows":     {[][]float64{{1, 2}, {3}}, DefaultConfig()},
		"empty first row": {[][]float64{{}, {1}}, DefaultConfig()},
	}
	for name, c := range cases {
		if _, err := Fit(c.rows, c.cfg); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// cloud draws n rows around the origin with the given number of features.
func cloud(n, features int, seed uint64) [][]float64 {
	rng := rand.New(rand.NewPCG(seed, 1))
	rows := make([][]float64, n)
	for i := range rows {
		rows[i] = make([]float64, features)
		for j := range rows[i] {
			rows[i][j] = rng.NormFloat64()
		}
	}
	return rows
}

func TestFitIsDeterministic(t *testing.T) {
	rows := cloud(300, 3, 1)
	a, err := Fit(rows, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Fit(rows, DefaultConfig())
	other, _ := Fit(rows, Config{Trees: 100, SampleSize: 256, Seed: 8})

	x := []float64{2.5, -1, 0.3}
	if a.Score(x) != b.Score(x) {
		t.Errorf("same seed gave %v and %v", a.Score(x), b.Score(x))
	}
	if a.Score(x) == other.Score(x) {
		t.Error("a different seed should grow different trees")
	}
}

func TestOutlierScoresAboveTheCloud(t *testing.T) {
	rows := cloud(400, 3, 2)
	f, err := Fit(rows, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, r := range rows {
		sum += f.Score(r)
	}
	if mean := sum / float64(len(rows)); mean > 0.5 {
		t.Errorf("mean score of the cloud = %.3f, want at most 0.5", mean)
	}
	if far := f.Score([]float64{9, 9, 9}); far < 0.65 {
		t.Errorf("score far outside the cloud = %.3f, want at least 0.65", far)
	}
	if centre := f.Score([]float64{0, 0, 0}); centre > 0.45 {
		t.Errorf("score at the centre = %.3f, want at most 0.45", centre)
	}
}

// The forest isolates points that are unusual as a whole. One feature far out
// with the rest ordinary only lifts the score a little (numpy reference:
// 0.50 against 0.38 at the centre), which is why single-variable outliers stay
// with the z-score detectors.
func TestOutlierInASingleFeatureOnlyLiftsTheScoreALittle(t *testing.T) {
	f, err := Fit(cloud(400, 4, 3), Config{Trees: 500, SampleSize: 256, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	centre := f.Score([]float64{0, 0, 0, 0})
	one := f.Score([]float64{0, 0, 12, 0})
	all := f.Score([]float64{9, 9, 9, 9})
	if !(centre+0.05 < one && one < all-0.1) {
		t.Errorf("scores centre=%.3f one feature=%.3f all features=%.3f, want them to rise in that order", centre, one, all)
	}
}

func TestIdenticalRowsScoreOneHalf(t *testing.T) {
	rows := make([][]float64, 10)
	for i := range rows {
		rows[i] = []float64{3, 4}
	}
	f, err := Fit(rows, Config{Trees: 5, SampleSize: 256, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	near(t, "score", f.Score([]float64{3, 4}), 0.5)
}

func TestSampleIsCappedByTheRows(t *testing.T) {
	f, err := Fit(cloud(50, 2, 4), Config{Trees: 3, SampleSize: 256, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if f.sample != 50 {
		t.Errorf("sample = %d, want 50", f.sample)
	}
	g, _ := Fit(cloud(500, 2, 4), Config{Trees: 3, SampleSize: 64, Seed: 1})
	if g.sample != 64 {
		t.Errorf("sample = %d, want 64", g.sample)
	}
}

// Every split must send at least one row each way, sizes must add up and no
// tree may grow past ceil(log2(sample)).
func TestTreesAreWellFormed(t *testing.T) {
	f, err := Fit(cloud(300, 3, 5), Config{Trees: 20, SampleSize: 128, Seed: 3})
	if err != nil {
		t.Fatal(err)
	}
	limit := 7 // ceil(log2(128))
	var walk func(n *node, depth int)
	walk = func(n *node, depth int) {
		if depth > limit {
			t.Fatalf("depth %d beyond the limit %d", depth, limit)
		}
		if n.feature < 0 {
			return
		}
		if n.left.size < 1 || n.right.size < 1 {
			t.Fatalf("split with an empty side: %d and %d rows", n.left.size, n.right.size)
		}
		if n.left.size+n.right.size != n.size {
			t.Fatalf("children hold %d rows, node %d", n.left.size+n.right.size, n.size)
		}
		walk(n.left, depth+1)
		walk(n.right, depth+1)
	}
	for _, tree := range f.trees {
		if tree.size != 128 {
			t.Fatalf("root holds %d rows, want 128", tree.size)
		}
		walk(tree, 0)
	}
}

// The limit is ceil(log2(sample)): 100 rows give 7, and with that many random
// splits at least one tree reaches it.
func TestTreesGrowUpToTheHeightLimit(t *testing.T) {
	f, err := Fit(cloud(300, 3, 6), Config{Trees: 30, SampleSize: 100, Seed: 3})
	if err != nil {
		t.Fatal(err)
	}
	var height func(n *node) int
	height = func(n *node) int {
		if n.feature < 0 {
			return 0
		}
		return 1 + max(height(n.left), height(n.right))
	}
	deepest := 0
	for _, tree := range f.trees {
		deepest = max(deepest, height(tree))
	}
	if deepest != 7 {
		t.Errorf("deepest tree has height %d, want 7", deepest)
	}
}

// x goes right at the root (8 rows, 4 on its side), then left on the second
// cut (4 rows, 1 on its side). The credit is ln(8/4) for feature 0 and ln(4/1)
// for feature 1, so their shares are 1/3 and 2/3.
func TestAttributeOnAHandBuiltTree(t *testing.T) {
	tree := &node{feature: 0, split: 5, size: 8,
		left: &node{feature: -1, size: 4},
		right: &node{feature: 1, split: 2, size: 4,
			left:  &node{feature: -1, size: 1},
			right: &node{feature: -1, size: 3},
		},
	}
	f := &Forest{trees: []*node{tree}, sample: 8, features: 3}
	got := f.Attribute([]float64{9, 0, 0})
	want := []float64{1.0 / 3, 2.0 / 3, 0}
	for i := range want {
		near(t, "share", got[i], want[i])
	}
}

// A second tree cuts feature 2 once, leaving x among 2 of 8 rows: ln(8/2).
func TestAttributeAddsUpTheTrees(t *testing.T) {
	first := &node{feature: 0, split: 5, size: 8,
		left: &node{feature: -1, size: 4},
		right: &node{feature: 1, split: 2, size: 4,
			left:  &node{feature: -1, size: 1},
			right: &node{feature: -1, size: 3},
		},
	}
	second := &node{feature: 2, split: 0, size: 8,
		left:  &node{feature: -1, size: 2},
		right: &node{feature: -1, size: 6},
	}
	f := &Forest{trees: []*node{first, second}, sample: 8, features: 3}
	got := f.Attribute([]float64{9, 0, -1})
	// ln2 + ln4 + ln4 = 5 ln2, so the shares are 1/5, 2/5 and 2/5.
	want := []float64{0.2, 0.4, 0.4}
	for i := range want {
		near(t, "share", got[i], want[i])
	}
}

// A point exactly on a cut goes left, the same way Score sends it. There it
// meets a second cut: ln(8/2) goes to feature 0 and ln(2/1) to feature 1, so
// 2/3 and 1/3. Sent right it would end in a leaf of six with feature 0 alone.
func TestAttributeSendsAPointOnTheCutLeft(t *testing.T) {
	tree := &node{feature: 0, split: 5, size: 8,
		left: &node{feature: 1, split: 1, size: 2,
			left:  &node{feature: -1, size: 1},
			right: &node{feature: -1, size: 1},
		},
		right: &node{feature: -1, size: 6},
	}
	f := &Forest{trees: []*node{tree}, sample: 8, features: 2}
	got := f.Attribute([]float64{5, 0})
	near(t, "feature 0", got[0], 2.0/3)
	near(t, "feature 1", got[1], 1.0/3)
}

func TestAttributeIsZeroWhenNothingSplits(t *testing.T) {
	f := &Forest{trees: []*node{{feature: -1, size: 5}}, sample: 5, features: 2}
	for i, s := range f.Attribute([]float64{1, 1}) {
		if s != 0 {
			t.Errorf("share %d = %v, want 0", i, s)
		}
	}
}

// Numpy reference on a similar cloud: the far feature takes about 0.6 of the
// credit and the other three about 0.13 each; two far features take about 0.4
// each; at the centre no feature stands out.
func TestAttributeFindsTheFeaturesThatIsolate(t *testing.T) {
	f, err := Fit(cloud(400, 4, 3), Config{Trees: 500, SampleSize: 256, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	sum := func(s []float64) (total float64) {
		for _, v := range s {
			total += v
		}
		return total
	}

	one := f.Attribute([]float64{0, 0, 12, 0})
	near(t, "shares add up", sum(one), 1)
	if one[2] < 0.5 || one[0] > 0.2 || one[1] > 0.2 || one[3] > 0.2 {
		t.Errorf("one far feature: shares %.2f, want feature 2 above 0.5 and the rest below 0.2", one)
	}

	two := f.Attribute([]float64{0, 4, 0, 4})
	if two[1] < 0.3 || two[3] < 0.3 || two[0] > 0.2 || two[2] > 0.2 {
		t.Errorf("two far features: shares %.2f, want features 1 and 3 above 0.3 and the rest below 0.2", two)
	}

	centre := f.Attribute([]float64{0, 0, 0, 0})
	for i, s := range centre {
		if s < 0.15 || s > 0.35 {
			t.Errorf("centre: share %d = %.2f, want between 0.15 and 0.35", i, s)
		}
	}
}

func TestCutStaysInsideTheRange(t *testing.T) {
	near(t, "middle", cut(2, 4, 0.5), 3)
	near(t, "start", cut(2, 4, 0), 2)

	// Here lo + (hi-lo)*u rounds up to hi itself.
	lo := 1.0
	hi := math.Nextafter(lo, 2)
	u := math.Nextafter(1, 0)
	if lo+(hi-lo)*u != hi {
		t.Fatal("the case no longer rounds up to hi, pick another")
	}
	if got := cut(lo, hi, u); got < lo || got >= hi {
		t.Errorf("cut = %v, want in [%v, %v)", got, lo, hi)
	}
}
