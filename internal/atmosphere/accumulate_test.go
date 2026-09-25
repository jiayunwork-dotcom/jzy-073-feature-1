package atmosphere

import (
	"math"
	"testing"
	"time"
)

// Rule: level flight crosses no altitude, so its traversed column mass is
// exactly zero — not approximately, whatever the duration.
func TestLevelFlightColumnMassExactlyZero(t *testing.T) {
	for _, duration := range []float64{0.5, 60, 7200, 86400} {
		acc, err := AccumulateTrajectory([]Segment{
			{StartTime: 0, EndTime: duration, StartAltitude: 8848, EndAltitude: 8848},
		})
		if err != nil {
			t.Fatalf("AccumulateTrajectory error: %v", err)
		}
		if acc.Segments[0].ColumnMassKgM2 != 0.0 {
			t.Errorf("level flight %v s: segment column mass = %v, want exactly 0",
				duration, acc.Segments[0].ColumnMassKgM2)
		}
		if acc.Total.ColumnMassKgM2 != 0.0 {
			t.Errorf("level flight %v s: total column mass = %v, want exactly 0",
				duration, acc.Total.ColumnMassKgM2)
		}
	}
}

// Rule: the column mass integral is additive over segmentation. A single
// sea-level-to-ceiling leg and the same leg cut in two at the tropopause
// must accumulate to the same value within a small tolerance — this only
// holds when the junction is honoured as a forced integration node.
func TestColumnMassAdditiveAcrossTropopauseSplit(t *testing.T) {
	single, err := AccumulateTrajectory([]Segment{
		{StartTime: 0, EndTime: 1200, StartAltitude: 0, EndAltitude: ModelTopAltitude},
	})
	if err != nil {
		t.Fatalf("single-leg accumulation error: %v", err)
	}
	split, err := AccumulateTrajectory([]Segment{
		{StartTime: 0, EndTime: 660, StartAltitude: 0, EndAltitude: TropopauseAltitude},
		{StartTime: 660, EndTime: 1200, StartAltitude: TropopauseAltitude, EndAltitude: ModelTopAltitude},
	})
	if err != nil {
		t.Fatalf("split-leg accumulation error: %v", err)
	}

	got, want := single.Total.ColumnMassKgM2, split.Total.ColumnMassKgM2
	if !approxEq(got, want, 1e-9) {
		t.Errorf("additivity across tropopause broken: single %.9f vs split %.9f kg/m^2",
			got, want)
	}
	// The split legs must also sum to the whole individually.
	sum := split.Segments[0].ColumnMassKgM2 + split.Segments[1].ColumnMassKgM2
	if !approxEq(sum, want, tolEq) {
		t.Errorf("segment ledger does not sum to total: %.12f vs %.12f", sum, want)
	}
}

// Rule: the column mass is a pure geometric integral — shifting or
// uniformly rescaling the time axis while keeping the altitude path cannot
// change it.
func TestColumnMassInvariantUnderTimeShiftAndScaling(t *testing.T) {
	altitudes := [][2]float64{{0, 11000}, {11000, 11000}, {11000, 5000}, {5000, 8000}}
	build := func(t0, scale float64) []Segment {
		durations := []float64{600, 1800, 400, 900}
		segs := make([]Segment, 0, len(altitudes))
		start := t0
		for i, d := range durations {
			end := start + d*scale
			segs = append(segs, Segment{
				StartTime: start, EndTime: end,
				StartAltitude: altitudes[i][0], EndAltitude: altitudes[i][1],
			})
			start = end
		}
		return segs
	}

	base, err := AccumulateTrajectory(build(0, 1))
	if err != nil {
		t.Fatalf("base accumulation error: %v", err)
	}
	warped, err := AccumulateTrajectory(build(123456.0, 2.5))
	if err != nil {
		t.Fatalf("warped accumulation error: %v", err)
	}

	if !approxEq(base.Total.ColumnMassKgM2, warped.Total.ColumnMassKgM2, 1e-12) {
		t.Errorf("column mass moved with the time axis: %.12f vs %.12f kg/m^2",
			base.Total.ColumnMassKgM2, warped.Total.ColumnMassKgM2)
	}
	for i := range base.Segments {
		if !approxEq(base.Segments[i].ColumnMassKgM2, warped.Segments[i].ColumnMassKgM2, 1e-12) {
			t.Errorf("segment %d column mass moved: %.12f vs %.12f",
				i, base.Segments[i].ColumnMassKgM2, warped.Segments[i].ColumnMassKgM2)
		}
	}
}

// Rule: the traversed column mass is physical — it equals the hydrostatic
// column (pressure difference over g) for any monotonic leg, across the
// junction included. Climbing or descending the same span traverses the
// same air, and flying it twice traverses it twice.
func TestColumnMassPhysics(t *testing.T) {
	hydrostatic := func(lo, hi float64) float64 {
		sLo, errLo := Compute(lo, 0)
		sHi, errHi := Compute(hi, 0)
		if errLo != nil || errHi != nil {
			t.Fatalf("Compute error: %v, %v", errLo, errHi)
		}
		return (sLo.Pressure - sHi.Pressure) / Gravity
	}

	climb, err := AccumulateTrajectory([]Segment{
		{StartTime: 0, EndTime: 900, StartAltitude: 2000, EndAltitude: 15000},
	})
	if err != nil {
		t.Fatalf("climb accumulation error: %v", err)
	}
	if want := hydrostatic(2000, 15000); !approxEq(climb.Total.ColumnMassKgM2, want, 1e-9) {
		t.Errorf("climb column mass = %.9f, hydrostatic reference %.9f kg/m^2",
			climb.Total.ColumnMassKgM2, want)
	}

	// Descending the same span traverses the same (positive) air mass.
	descent, err := AccumulateTrajectory([]Segment{
		{StartTime: 0, EndTime: 900, StartAltitude: 15000, EndAltitude: 2000},
	})
	if err != nil {
		t.Fatalf("descent accumulation error: %v", err)
	}
	if !approxEq(descent.Total.ColumnMassKgM2, climb.Total.ColumnMassKgM2, tolEq) {
		t.Errorf("descent column mass %.9f != climb %.9f kg/m^2",
			descent.Total.ColumnMassKgM2, climb.Total.ColumnMassKgM2)
	}
	if descent.Total.ColumnMassKgM2 <= 0 {
		t.Errorf("descent column mass must be positive, got %v", descent.Total.ColumnMassKgM2)
	}

	// Up and back down accumulates both crossings — the quantity tracks the
	// path, not just the endpoints.
	roundTrip, err := AccumulateTrajectory([]Segment{
		{StartTime: 0, EndTime: 900, StartAltitude: 2000, EndAltitude: 15000},
		{StartTime: 900, EndTime: 1800, StartAltitude: 15000, EndAltitude: 2000},
	})
	if err != nil {
		t.Fatalf("round trip accumulation error: %v", err)
	}
	if want := 2 * climb.Total.ColumnMassKgM2; !approxEq(roundTrip.Total.ColumnMassKgM2, want, tolEq) {
		t.Errorf("round trip column mass = %.9f, want 2x one-way %.9f kg/m^2",
			roundTrip.Total.ColumnMassKgM2, want)
	}
}

// Rule: point values seen through the trajectory lens are the model's own
// point values. A level leg's time-weighted means are bit-identical to what
// Compute returns for that altitude — the integrator reuses the model's
// formulas instead of carrying its own copies.
func TestLevelFlightMeansMatchPointQuery(t *testing.T) {
	for _, h := range []float64{0, 5000, 11000, 15000, ModelTopAltitude} {
		acc, err := AccumulateTrajectory([]Segment{
			{StartTime: 0, EndTime: 3600, StartAltitude: h, EndAltitude: h},
		})
		if err != nil {
			t.Fatalf("AccumulateTrajectory(%.0f m level) error: %v", h, err)
		}
		point, err := Compute(h, 0)
		if err != nil {
			t.Fatalf("Compute(%.0f) error: %v", h, err)
		}
		seg := acc.Segments[0]
		if seg.MeanPressurePa != point.Pressure {
			t.Errorf("level %.0f m: mean pressure %.12f != point query %.12f",
				h, seg.MeanPressurePa, point.Pressure)
		}
		if seg.MeanDensityKgM3 != point.Density {
			t.Errorf("level %.0f m: mean density %.12f != point query %.12f",
				h, seg.MeanDensityKgM3, point.Density)
		}
		// The trajectory total is a sum divided by a duration, so allow one
		// rounding level of slack against the exact point value.
		if !approxEq(acc.Total.MeanPressurePa, point.Pressure, 1e-12) {
			t.Errorf("level %.0f m: total mean pressure %.12f != point query %.12f",
				h, acc.Total.MeanPressurePa, point.Pressure)
		}
		if !approxEq(acc.Total.MeanDensityKgM3, point.Density, 1e-12) {
			t.Errorf("level %.0f m: total mean density %.12f != point query %.12f",
				h, acc.Total.MeanDensityKgM3, point.Density)
		}
	}
}

// Rule: the total means are time-weighted. A level-climb-level chain must
// average its legs by duration, and the weights are checkable by hand: level
// legs contribute point value x duration, the climb contributes its altitude
// column divided by its climb rate.
func TestTotalMeansAreTimeWeighted(t *testing.T) {
	acc, err := AccumulateTrajectory([]Segment{
		{StartTime: 0, EndTime: 100, StartAltitude: 5000, EndAltitude: 5000},
		{StartTime: 100, EndTime: 200, StartAltitude: 5000, EndAltitude: 10000},
		{StartTime: 200, EndTime: 400, StartAltitude: 10000, EndAltitude: 10000},
	})
	if err != nil {
		t.Fatalf("AccumulateTrajectory error: %v", err)
	}
	p1, _ := Compute(5000, 0)
	p2, _ := Compute(10000, 0)
	const climbRate = 5000.0 / 100.0 // m/s over the middle leg

	// Pressure: the climb's time integral is its pressure column over the
	// climb rate; evaluate the column with the same integrator.
	pressureColumn := integrateOverAltitude(standardPressureAt, 5000, 10000)
	wantP := (p1.Pressure*100 + pressureColumn/climbRate + p2.Pressure*200) / 400
	if !approxEq(acc.Total.MeanPressurePa, wantP, 1e-12) {
		t.Errorf("time-weighted mean pressure = %.9f, want %.9f", acc.Total.MeanPressurePa, wantP)
	}

	// Density: the climb's density column is known analytically from the
	// hydrostatic identity, no integrator needed for the expectation.
	densityColumn := (p1.Pressure - p2.Pressure) / Gravity
	wantRho := (p1.Density*100 + densityColumn/climbRate + p2.Density*200) / 400
	if !approxEq(acc.Total.MeanDensityKgM3, wantRho, 1e-9) {
		t.Errorf("time-weighted mean density = %.9f, want %.9f", acc.Total.MeanDensityKgM3, wantRho)
	}

	// Sanity: the weighted means sit between the two level-leg values.
	if !(acc.Total.MeanPressurePa < p1.Pressure && acc.Total.MeanPressurePa > p2.Pressure) {
		t.Errorf("mean pressure %.3f not between leg values %.3f and %.3f",
			acc.Total.MeanPressurePa, p1.Pressure, p2.Pressure)
	}
}

// Rule: along a climbing leg the time-weighted means lie strictly between
// the endpoint point values — a mean that escapes the endpoint bracket
// would signal an integrator sampling outside the leg.
func TestClimbMeansBracketedByEndpoints(t *testing.T) {
	acc, err := AccumulateTrajectory([]Segment{
		{StartTime: 0, EndTime: 660, StartAltitude: 0, EndAltitude: TropopauseAltitude},
	})
	if err != nil {
		t.Fatalf("AccumulateTrajectory error: %v", err)
	}
	lo, _ := Compute(0, 0)
	hi, _ := Compute(TropopauseAltitude, 0)
	seg := acc.Segments[0]
	if !(seg.MeanPressurePa < lo.Pressure && seg.MeanPressurePa > hi.Pressure) {
		t.Errorf("mean pressure %.3f not bracketed by %.3f and %.3f",
			seg.MeanPressurePa, lo.Pressure, hi.Pressure)
	}
	if !(seg.MeanDensityKgM3 < lo.Density && seg.MeanDensityKgM3 > hi.Density) {
		t.Errorf("mean density %.6f not bracketed by %.6f and %.6f",
			seg.MeanDensityKgM3, lo.Density, hi.Density)
	}
}

// Rule: the response carries a consistent ledger — totals equal the sum of
// the per-segment entries, and the bookkeeping fields describe the path.
func TestLedgerConsistency(t *testing.T) {
	segs := []Segment{
		{StartTime: 100, EndTime: 700, StartAltitude: 0, EndAltitude: 11000},
		{StartTime: 700, EndTime: 2500, StartAltitude: 11000, EndAltitude: 11000},
		{StartTime: 2500, EndTime: 3100, StartAltitude: 11000, EndAltitude: 3000},
	}
	acc, err := AccumulateTrajectory(segs)
	if err != nil {
		t.Fatalf("AccumulateTrajectory error: %v", err)
	}
	if acc.SegmentCount != 3 || len(acc.Segments) != 3 {
		t.Errorf("segment count = %d / %d, want 3", acc.SegmentCount, len(acc.Segments))
	}
	if acc.StartTimeS != 100 || acc.EndTimeS != 3100 || acc.DurationS != 3000 {
		t.Errorf("time bookkeeping wrong: start %v end %v duration %v",
			acc.StartTimeS, acc.EndTimeS, acc.DurationS)
	}
	if acc.MinAltitudeM != 0 || acc.MaxAltitudeM != 11000 {
		t.Errorf("altitude bookkeeping wrong: min %v max %v", acc.MinAltitudeM, acc.MaxAltitudeM)
	}
	var columnSum float64
	for i, s := range acc.Segments {
		columnSum += s.ColumnMassKgM2
		if s.Index != i {
			t.Errorf("segment %d has index %d", i, s.Index)
		}
		if s.DurationS != s.EndTimeS-s.StartTimeS {
			t.Errorf("segment %d duration inconsistent", i)
		}
	}
	if !approxEq(columnSum, acc.Total.ColumnMassKgM2, tolEq) {
		t.Errorf("segment column masses sum %.12f != total %.12f", columnSum, acc.Total.ColumnMassKgM2)
	}
	// The cruise leg is level: exactly zero column, and its climb rate is 0.
	if acc.Segments[1].ColumnMassKgM2 != 0 || acc.Segments[1].ClimbRateMS != 0 {
		t.Errorf("cruise leg ledger wrong: %+v", acc.Segments[1])
	}
	// The descent leg has a negative climb rate.
	if acc.Segments[2].ClimbRateMS >= 0 {
		t.Errorf("descent climb rate = %v, want negative", acc.Segments[2].ClimbRateMS)
	}
}

// Rule: long trajectories stay cheap. Thousands of segments — a fine-grained
// flight profile — must accumulate fast and stay accurate.
func TestManySegmentTrajectoryStaysFastAndAccurate(t *testing.T) {
	const legs = 5000
	segs := make([]Segment, 0, legs)
	for i := 0; i < legs; i++ {
		segs = append(segs, Segment{
			StartTime:     float64(i) * 0.5,
			EndTime:       float64(i+1) * 0.5,
			StartAltitude: ModelTopAltitude * float64(i) / legs,
			EndAltitude:   ModelTopAltitude * float64(i+1) / legs,
		})
	}

	start := time.Now()
	acc, err := AccumulateTrajectory(segs)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("AccumulateTrajectory error: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("accumulating %d segments took %v, too slow", legs, elapsed)
	}

	// Accuracy is unaffected by fine segmentation: still the hydrostatic column.
	s0, _ := Compute(0, 0)
	sTop, _ := Compute(ModelTopAltitude, 0)
	want := (s0.Pressure - sTop.Pressure) / Gravity
	if !approxEq(acc.Total.ColumnMassKgM2, want, 1e-9) {
		t.Errorf("%d-leg column mass = %.9f, hydrostatic reference %.9f kg/m^2",
			legs, acc.Total.ColumnMassKgM2, want)
	}
	if math.IsNaN(acc.Total.MeanPressurePa) || acc.Total.MeanPressurePa <= 0 {
		t.Errorf("mean pressure over %d legs = %v", legs, acc.Total.MeanPressurePa)
	}
}
