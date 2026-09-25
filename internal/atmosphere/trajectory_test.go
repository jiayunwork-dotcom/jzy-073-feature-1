package atmosphere

import (
	"math"
	"testing"
	"time"
)

// ptr returns a pointer to v, for building SegmentInput values.
func ptr(v float64) *float64 { return &v }

// leg builds one segment specified by absolute times.
func leg(t0, t1, h0, h1 float64) SegmentInput {
	return SegmentInput{
		StartTimeS: ptr(t0),
		EndTimeS:   ptr(t1),
		StartAltM:  ptr(h0),
		EndAltM:    ptr(h1),
	}
}

// legDur builds one segment specified by start time and duration.
func legDur(t0, duration, h0, h1 float64) SegmentInput {
	return SegmentInput{
		StartTimeS: ptr(t0),
		DurationS:  ptr(duration),
		StartAltM:  ptr(h0),
		EndAltM:    ptr(h1),
	}
}

func mustAccumulate(t *testing.T, in TrajectoryInput) *TrajectoryResult {
	t.Helper()
	res, err := AccumulateFromInput(in)
	if err != nil {
		t.Fatalf("unexpected trajectory error: %v", err)
	}
	return res
}

// ---------------------------------------------------------------------------
// Invariant 1: a level leg traverses ZERO air column, no matter how long it
// lasts. h never changes, so |rho dh| is identically zero at every node.
// ---------------------------------------------------------------------------

func TestLevelLegTraversesZeroMass(t *testing.T) {
	for _, duration := range []float64{1.0, 600.0, 86400.0} {
		for _, h := range []float64{0.0, 4500.0, TropopauseAltitude, 17500.0, ModelTopAltitude} {
			res := mustAccumulate(t, TrajectoryInput{Segments: []SegmentInput{
				legDur(0, duration, h, h),
			}})
			seg := res.Segments[0]
			if seg.TraversedAirMassKGM2 != 0.0 {
				t.Errorf("level leg at h=%.0f for %.0f s traversed mass %.6e, want exactly 0",
					h, duration, seg.TraversedAirMassKGM2)
			}
			if res.TraversedAirMassKGM2 != 0.0 {
				t.Errorf("total mass %.6e, want exactly 0 for a level leg", res.TraversedAirMassKGM2)
			}
			if seg.CrossesTropopause {
				t.Errorf("level leg should not be flagged as crossing the tropopause")
			}
		}
	}
}

// A level leg's time means must equal the point values at its altitude:
// every quadrature node samples the same state.
func TestLevelLegMeansEqualPointState(t *testing.T) {
	for _, h := range []float64{0.0, 8000.0, 11000.0, 14000.0, 20000.0} {
		res := mustAccumulate(t, TrajectoryInput{
			TemperatureOffsetK: 10,
			Segments:           []SegmentInput{legDur(100, 500, h, h)},
		})
		want, err := Compute(h, 10)
		if err != nil {
			t.Fatalf("Compute: %v", err)
		}
		seg := res.Segments[0]
		if seg.MeanPressurePa != want.Pressure {
			t.Errorf("h=%.0f mean pressure %.12f != point pressure %.12f", h, seg.MeanPressurePa, want.Pressure)
		}
		if seg.MeanDensityKGM3 != want.Density {
			t.Errorf("h=%.0f mean density %.12f != point density %.12f", h, seg.MeanDensityKGM3, want.Density)
		}
		if seg.MeanTemperatureK != want.Temperature {
			t.Errorf("h=%.0f mean temperature %.12f != point temperature %.12f", h, seg.MeanTemperatureK, want.Temperature)
		}
	}
}

// ---------------------------------------------------------------------------
// Invariant 2: additivity over junction splits. A single ground-to-ceiling
// leg and the same leg cut at the tropopause must accumulate to the same
// master ledger (mass and every time mean) within a tiny tolerance.
// ---------------------------------------------------------------------------

func TestSingleLegEqualsLegsSplitAtTropopause(t *testing.T) {
	// Climb 0 -> 20000 m in one leg, 2000 s.
	one := mustAccumulate(t, TrajectoryInput{Segments: []SegmentInput{
		leg(0, 2000, 0, ModelTopAltitude),
	}})
	if !one.Segments[0].CrossesTropopause {
		t.Fatalf("the 0..20000 m leg must be flagged as crossing the tropopause")
	}

	// Same climb cut into 0..11000 and 11000..20000, 1100 s + 900 s so the
	// per-leg rates and the time parameterization match exactly.
	split := mustAccumulate(t, TrajectoryInput{Segments: []SegmentInput{
		leg(0, 1100, 0, TropopauseAltitude),
		leg(1100, 2000, TropopauseAltitude, ModelTopAltitude),
	}})
	if split.Segments[0].CrossesTropopause || split.Segments[1].CrossesTropopause {
		t.Fatalf("neither sub-leg crosses the junction, must not be split again")
	}

	const tol = 1e-12
	if !approxEq(one.TraversedAirMassKGM2, split.TraversedAirMassKGM2, tol) {
		t.Errorf("mass one-leg %.12f vs split %.12f, rel diff %.3e",
			one.TraversedAirMassKGM2, split.TraversedAirMassKGM2,
			math.Abs(one.TraversedAirMassKGM2-split.TraversedAirMassKGM2)/split.TraversedAirMassKGM2)
	}
	if !approxEq(one.MeanPressurePa, split.MeanPressurePa, tol) {
		t.Errorf("mean pressure one-leg %.12f vs split %.12f", one.MeanPressurePa, split.MeanPressurePa)
	}
	if !approxEq(one.MeanDensityKGM3, split.MeanDensityKGM3, tol) {
		t.Errorf("mean density one-leg %.12f vs split %.12f", one.MeanDensityKGM3, split.MeanDensityKGM3)
	}
	if !approxEq(one.MeanTemperatureK, split.MeanTemperatureK, tol) {
		t.Errorf("mean temperature one-leg %.12f vs split %.12f", one.MeanTemperatureK, split.MeanTemperatureK)
	}

	// The two per-leg masses of the split must sum to the crossing leg's mass.
	if !approxEq(split.Segments[0].TraversedAirMassKGM2+split.Segments[1].TraversedAirMassKGM2,
		one.Segments[0].TraversedAirMassKGM2, tol) {
		t.Errorf("per-leg masses do not reconstruct the crossing leg")
	}
}

// Same additivity for a descending crossing leg (ceiling -> ground).
func TestDescendingLegSplitAtTropopause(t *testing.T) {
	one := mustAccumulate(t, TrajectoryInput{Segments: []SegmentInput{
		leg(500, 2500, ModelTopAltitude, 0),
	}})
	split := mustAccumulate(t, TrajectoryInput{Segments: []SegmentInput{
		leg(500, 1400, ModelTopAltitude, TropopauseAltitude),
		leg(1400, 2500, TropopauseAltitude, 0),
	}})
	const tol = 1e-12
	if !approxEq(one.TraversedAirMassKGM2, split.TraversedAirMassKGM2, tol) {
		t.Errorf("descent mass %.12f vs split %.12f", one.TraversedAirMassKGM2, split.TraversedAirMassKGM2)
	}
	if !approxEq(one.MeanPressurePa, split.MeanPressurePa, tol) {
		t.Errorf("descent mean pressure %.12f vs split %.12f", one.MeanPressurePa, split.MeanPressurePa)
	}
}

// splitAtJunction itself: only legs whose altitude interval strictly contains
// the junction are cut.
func TestSplitAtJunctionGeometry(t *testing.T) {
	crossing := []Leg{
		{StartTimeS: 0, EndTimeS: 1, DurationS: 1, StartAltM: 10000, EndAltM: 12000},
		{StartTimeS: 0, EndTimeS: 1, DurationS: 1, StartAltM: 12000, EndAltM: 10000},
	}
	for _, l := range crossing {
		pieces := splitAtJunction(l)
		if len(pieces) != 2 {
			t.Fatalf("crossing leg produced %d pieces, want 2", len(pieces))
		}
		if pieces[0].endAltM != TropopauseAltitude || pieces[1].startAltM != TropopauseAltitude {
			t.Errorf("pieces do not meet at the junction: %+v %+v", pieces[0], pieces[1])
		}
		if math.Abs(pieces[0].endTimeS-pieces[1].startTimeS) > 0 {
			t.Errorf("pieces do not meet in time: %+v %+v", pieces[0], pieces[1])
		}
		if math.Abs((pieces[0].endTimeS-pieces[0].startTimeS)+
			(pieces[1].endTimeS-pieces[1].startTimeS)-1) > 1e-12 {
			t.Errorf("piece durations do not add up to the leg duration")
		}
	}
	nonCrossing := []Leg{
		{StartTimeS: 0, EndTimeS: 1, DurationS: 1, StartAltM: 0, EndAltM: 11000},
		{StartTimeS: 0, EndTimeS: 1, DurationS: 1, StartAltM: 11000, EndAltM: 20000},
		{StartTimeS: 0, EndTimeS: 1, DurationS: 1, StartAltM: 9000, EndAltM: 9000},
		{StartTimeS: 0, EndTimeS: 1, DurationS: 1, StartAltM: 11000, EndAltM: 11000},
	}
	for i, l := range nonCrossing {
		if pieces := splitAtJunction(l); len(pieces) != 1 {
			t.Errorf("non-crossing case %d produced %d pieces, want 1", i, len(pieces))
		}
	}
}

// ---------------------------------------------------------------------------
// The mandatory split is not optional politeness: a single smooth GL rule
// applied over the kink is measurably wrong. This test pins both directions —
// the service answer matches the piecewise analytic integral, while an
// unsplit GL8 evaluation of the same leg shows a bias orders of magnitude
// larger. If the split were ever removed, the first comparison fails.
// ---------------------------------------------------------------------------

func TestJunctionSplitBeatsUnsplitQuadrature(t *testing.T) {
	h0, h1 := 10000.0, 12000.0
	res := mustAccumulate(t, TrajectoryInput{Segments: []SegmentInput{
		leg(0, 200, h0, h1),
	}})
	wantMass := analyticColumnMass(h0, h1)

	got := res.Segments[0].TraversedAirMassKGM2
	serviceErr := math.Abs(got-wantMass) / wantMass
	if serviceErr > 1e-11 {
		t.Fatalf("service mass %.12f vs analytic %.12f, rel err %.3e", got, wantMass, serviceErr)
	}

	// What a naive caller gets by applying one GL8 rule straight across the
	// kink (time measure, dh linear in t).
	unsplit := unsplitGL8ColumnMass(h0, h1)
	naiveErr := math.Abs(unsplit-wantMass) / wantMass
	if naiveErr < 1e-6 {
		t.Fatalf("unsplit quadrature error %.3e unexpectedly tiny; the kink test would not catch a regression",
			naiveErr)
	}
	if naiveErr <= serviceErr*1000 {
		t.Fatalf("splitting provides no meaningful advantage: unsplit err %.3e vs split %.3e",
			naiveErr, serviceErr)
	}
}

// analyticColumnMass evaluates integral_{h0}^{h1} rho(h) dh using exact
// antiderivatives of the two layers, splitting at the tropopause itself.
func analyticColumnMass(h0, h1 float64) float64 {
	if h1 < h0 {
		h0, h1 = h1, h0
	}
	if h1 <= TropopauseAltitude {
		return analyticTroposphericMass(h0, h1)
	}
	if h0 >= TropopauseAltitude {
		return analyticStratosphericMass(h0, h1)
	}
	return analyticTroposphericMass(h0, TropopauseAltitude) +
		analyticStratosphericMass(TropopauseAltitude, h1)
}

// ∫ rho dh in the gradient layer:
// rho(h) = rho0 (1-λh/T0)^n, n = g/(Rλ)-1.
func analyticTroposphericMass(h0, h1 float64) float64 {
	n := Gravity/(SpecificGasConstant*TemperatureLapseRate) - 1
	u := func(h float64) float64 { return 1 - TemperatureLapseRate*h/SeaLevelTemperature }
	return SeaLevelDensity * SeaLevelTemperature / TemperatureLapseRate *
		(math.Pow(u(h0), n+1) - math.Pow(u(h1), n+1)) / (n + 1)
}

// ∫ rho dh in the isothermal layer: rho = rho_t exp(-(h-H)/H_s).
func analyticStratosphericMass(h0, h1 float64) float64 {
	hs := SpecificGasConstant * TropopauseTemperature / Gravity
	return tropopauseDensity * hs *
		(math.Exp(-(h0-TropopauseAltitude)/hs) - math.Exp(-(h1-TropopauseAltitude)/hs))
}

// unsplitGL8ColumnMass applies the same 8-point Gauss rule directly over the
// altitude interval WITHOUT cutting at the junction — the deliberately wrong
// baseline used to prove the mandatory split matters.
func unsplitGL8ColumnMass(h0, h1 float64) float64 {
	mid := 0.5 * (h0 + h1)
	half := 0.5 * (h1 - h0)
	var sum float64
	for k := range gaussLegendre8.nodes {
		h := mid + half*gaussLegendre8.nodes[k]
		// Evaluate rho via the model's own piecewise function (the kink is
		// then hidden inside the samples, exactly the caller's mistake).
		var rho float64
		if h <= TropopauseAltitude {
			rho = TroposphericDensity(h)
		} else {
			rho = StratosphericDensity(h)
		}
		sum += gaussLegendre8.weights[k] * rho * half
	}
	return sum
}

// Smooth-region accuracy away from the kink: the GL8 result must agree with
// the analytic antiderivatives to round-off in each layer separately.
func TestSmoothRegionMatchesAnalytic(t *testing.T) {
	cases := []struct{ h0, h1 float64 }{
		{0, 1000},
		{2500, 9000},
		{11000, 15000},
		{15000, 20000},
		{0, 11000},
		{11000, 20000},
	}
	for _, tc := range cases {
		res := mustAccumulate(t, TrajectoryInput{Segments: []SegmentInput{
			leg(0, 100, tc.h0, tc.h1),
		}})
		got := res.Segments[0].TraversedAirMassKGM2
		want := analyticColumnMass(tc.h0, tc.h1)
		if rel := math.Abs(got-want) / want; rel > 1e-12 {
			t.Errorf("mass %.0f..%.0f = %.12f, analytic %.12f, rel err %.3e",
				tc.h0, tc.h1, got, want, rel)
		}
	}
}

// Time means on a smooth climb must equal the altitude-parameter mean
// (1/Δh)∫p dh, since time is linear in altitude along a constant-rate leg.
func TestMeanPressureMatchesAnalyticAltitudeMean(t *testing.T) {
	cases := []struct{ h0, h1 float64 }{
		{0, 10000},
		{11000, 20000},
	}
	for _, tc := range cases {
		res := mustAccumulate(t, TrajectoryInput{Segments: []SegmentInput{
			leg(0, 333, tc.h0, tc.h1),
		}})
		want := analyticMeanPressure(tc.h0, tc.h1)
		got := res.Segments[0].MeanPressurePa
		if rel := math.Abs(got-want) / want; rel > 1e-11 {
			t.Errorf("mean pressure %.0f..%.0f = %.9f, analytic %.9f, rel err %.3e",
				tc.h0, tc.h1, got, want, rel)
		}
	}
}

func analyticMeanPressure(h0, h1 float64) float64 {
	var integral float64
	if h1 <= TropopauseAltitude {
		integral = analyticTroposphericPressureIntegral(h0, h1)
	} else if h0 >= TropopauseAltitude {
		integral = analyticStratosphericPressureIntegral(h0, h1)
	} else {
		integral = analyticTroposphericPressureIntegral(h0, TropopauseAltitude) +
			analyticStratosphericPressureIntegral(TropopauseAltitude, h1)
	}
	return integral / (h1 - h0)
}

// ∫ p dh in the gradient layer, p(h) = p0 (1-λh/T0)^m with m = g/(Rλ).
func analyticTroposphericPressureIntegral(h0, h1 float64) float64 {
	m := Gravity / (SpecificGasConstant * TemperatureLapseRate)
	u := func(h float64) float64 { return 1 - TemperatureLapseRate*h/SeaLevelTemperature }
	return SeaLevelPressure * SeaLevelTemperature / TemperatureLapseRate *
		(math.Pow(u(h0), m+1) - math.Pow(u(h1), m+1)) / (m + 1)
}

func analyticStratosphericPressureIntegral(h0, h1 float64) float64 {
	hs := SpecificGasConstant * TropopauseTemperature / Gravity
	return tropopausePressure * hs *
		(math.Exp(-(h0-TropopauseAltitude)/hs) - math.Exp(-(h1-TropopauseAltitude)/hs))
}

// ---------------------------------------------------------------------------
// Invariant 3: the air-column mass is a pure geometric integral. Translating
// the time axis or rescaling every duration (faster/slower climb) while
// keeping the altitude walk identical must leave it unchanged; the time
// means are rate-independent too (the atmosphere is static).
// ---------------------------------------------------------------------------

func TestTimeTranslationAndScaleInvariance(t *testing.T) {
	base := []SegmentInput{
		leg(0, 1000, 0, 12000),
		leg(1000, 3000, 12000, 12000),
		leg(3000, 4000, 12000, 0),
	}
	baseRes := mustAccumulate(t, TrajectoryInput{Segments: base})

	// Translate the time origin by +1e6 s and scale every duration by 3.7,
	// rebuilding each leg from its own start instant and duration so the
	// chain itself stays continuous.
	const shift = 1e6
	const factor = 3.7
	moved := make([]SegmentInput, len(base))
	var clock float64 = shift
	for i, s := range base {
		d := (*s.EndTimeS - *s.StartTimeS) * factor
		moved[i] = leg(clock, clock+d, *s.StartAltM, *s.EndAltM)
		clock += d
	}
	movedRes := mustAccumulate(t, TrajectoryInput{Segments: moved})

	// Mass depends on geometry alone: bit-for-bit identical.
	if movedRes.TraversedAirMassKGM2 != baseRes.TraversedAirMassKGM2 {
		t.Errorf("mass changed under time translation/scaling: %.15f vs %.15f",
			movedRes.TraversedAirMassKGM2, baseRes.TraversedAirMassKGM2)
	}
	for i := range base {
		a := baseRes.Segments[i].TraversedAirMassKGM2
		b := movedRes.Segments[i].TraversedAirMassKGM2
		if a != b {
			t.Errorf("leg %d mass %.15f vs %.15f after time transform", i+1, a, b)
		}
	}
	const tol = 1e-12
	if !approxEq(movedRes.MeanPressurePa, baseRes.MeanPressurePa, tol) ||
		!approxEq(movedRes.MeanDensityKGM3, baseRes.MeanDensityKGM3, tol) ||
		!approxEq(movedRes.MeanTemperatureK, baseRes.MeanTemperatureK, tol) {
		t.Errorf("time means changed under time scaling: base %+v moved %+v",
			baseRes, movedRes)
	}
	if movedRes.DurationS != 4000*3.7 {
		t.Errorf("duration = %.1f, want %.1f", movedRes.DurationS, 4000*3.7)
	}
}

// ---------------------------------------------------------------------------
// Invariant 4: integrated point values reuse the single existing atmosphere
// implementation. Endpoint states must be exactly what Compute returns, and
// the offset semantics of the point endpoint carry over unchanged.
// ---------------------------------------------------------------------------

func TestEndpointStatesReuseSinglePoint(t *testing.T) {
	res := mustAccumulate(t, TrajectoryInput{
		TemperatureOffsetK: 12.5,
		Segments: []SegmentInput{
			leg(0, 500, 0, 11000),
			leg(500, 900, 11000, 20000),
		},
	})
	checks := []struct {
		got State
		h   float64
	}{
		{res.Segments[0].StartState, 0},
		{res.Segments[0].EndState, 11000},
		{res.Segments[1].StartState, 11000},
		{res.Segments[1].EndState, 20000},
	}
	for _, c := range checks {
		want, err := Compute(c.h, 12.5)
		if err != nil {
			t.Fatalf("Compute: %v", err)
		}
		if !statesEqual(c.got, want) {
			t.Errorf("endpoint state at %.0f differs from Compute: %+v vs %+v", c.h, c.got, want)
		}
	}
	// Offset applied along the whole route: zero offset is strictly denser
	// than a +20 K offset everywhere (mass shrinks on a warm day).
	warm := mustAccumulate(t, TrajectoryInput{
		TemperatureOffsetK: 20,
		Segments:           []SegmentInput{leg(0, 1000, 0, 20000)},
	})
	cold := mustAccumulate(t, TrajectoryInput{Segments: []SegmentInput{leg(0, 1000, 0, 20000)}})
	if !(warm.TraversedAirMassKGM2 < cold.TraversedAirMassKGM2) {
		t.Errorf("warm-day mass %.6f should be below standard-day mass %.6f",
			warm.TraversedAirMassKGM2, cold.TraversedAirMassKGM2)
	}
}

// ---------------------------------------------------------------------------
// Direction: descending through the same layer traverses the same air mass
// as climbing; a climb followed by the matching descent counts twice.
// ---------------------------------------------------------------------------

func TestDescentMassEqualsClimbAndRoundTripDoubles(t *testing.T) {
	up := mustAccumulate(t, TrajectoryInput{Segments: []SegmentInput{leg(0, 500, 0, 15000)}})
	down := mustAccumulate(t, TrajectoryInput{Segments: []SegmentInput{leg(900, 1400, 15000, 0)}})
	if !approxEq(up.TraversedAirMassKGM2, down.TraversedAirMassKGM2, 1e-14) {
		t.Errorf("up mass %.15f != down mass %.15f", up.TraversedAirMassKGM2, down.TraversedAirMassKGM2)
	}
	roundTrip := mustAccumulate(t, TrajectoryInput{Segments: []SegmentInput{
		leg(0, 500, 0, 15000),
		leg(500, 1000, 15000, 0),
	}})
	if !approxEq(roundTrip.TraversedAirMassKGM2, 2*up.TraversedAirMassKGM2, 1e-14) {
		t.Errorf("round trip mass %.15f, want twice one-way %.15f",
			roundTrip.TraversedAirMassKGM2, 2*up.TraversedAirMassKGM2)
	}
}

// Master ledger consistency: total means are the duration-weighted
// combination of the per-leg means.
func TestMasterLedgerIsDurationWeightedSum(t *testing.T) {
	res := mustAccumulate(t, TrajectoryInput{Segments: []SegmentInput{
		leg(0, 1000, 0, 11000),
		leg(1000, 3000, 11000, 11000),
		leg(3000, 4500, 11000, 0),
	}})
	var wp, wr, wt, dur float64
	for _, s := range res.Segments {
		d := s.EndTimeS - s.StartTimeS
		wp += s.MeanPressurePa * d
		wr += s.MeanDensityKGM3 * d
		wt += s.MeanTemperatureK * d
		dur += d
	}
	if math.Abs(wp/dur-res.MeanPressurePa) > 1e-9 {
		t.Errorf("master mean pressure %.12f != weighted legs %.12f", res.MeanPressurePa, wp/dur)
	}
	if math.Abs(wr/dur-res.MeanDensityKGM3) > 1e-12 {
		t.Errorf("master mean density %.12f != weighted legs %.12f", res.MeanDensityKGM3, wr/dur)
	}
	if math.Abs(wt/dur-res.MeanTemperatureK) > 1e-9 {
		t.Errorf("master mean temperature %.12f != weighted legs %.12f", res.MeanTemperatureK, wt/dur)
	}
	if math.Abs(dur-res.DurationS) > 1e-9 {
		t.Errorf("master duration %.6f != sum of leg durations %.6f", res.DurationS, dur)
	}
	// Per-leg mass sum equals master mass.
	var m float64
	for _, s := range res.Segments {
		m += s.TraversedAirMassKGM2
	}
	if m != res.TraversedAirMassKGM2 {
		t.Errorf("per-leg mass sum %.12f != master %.12f", m, res.TraversedAirMassKGM2)
	}
}

// ---------------------------------------------------------------------------
// Validation: illegal trajectories are rejected with precise reasons.
// ---------------------------------------------------------------------------

func TestRejectsIllegalTrajectories(t *testing.T) {
	cases := []struct {
		name     string
		in       TrajectoryInput
		wantCode ErrorCode
	}{
		{"no legs", TrajectoryInput{}, ErrInvalidTrajectory},
		{"missing start time", TrajectoryInput{Segments: []SegmentInput{{
			EndTimeS: ptr(1), StartAltM: ptr(0), EndAltM: ptr(1000),
		}}}, ErrInvalidTrajectory},
		{"missing start altitude", TrajectoryInput{Segments: []SegmentInput{
			leg(0, 1, 0, 0), // rebuilt below without start altitude
		}}, ErrInvalidTrajectory},
		{"both end time and duration", TrajectoryInput{Segments: []SegmentInput{{
			StartTimeS: ptr(0), EndTimeS: ptr(1), DurationS: ptr(1),
			StartAltM: ptr(0), EndAltM: ptr(0),
		}}}, ErrInvalidTrajectory},
		{"neither end time nor duration", TrajectoryInput{Segments: []SegmentInput{{
			StartTimeS: ptr(0), StartAltM: ptr(0), EndAltM: ptr(0),
		}}}, ErrInvalidTrajectory},
		{"zero duration", TrajectoryInput{Segments: []SegmentInput{
			legDur(0, 0, 0, 0),
		}}, ErrInvalidTrajectory},
		{"negative duration", TrajectoryInput{Segments: []SegmentInput{
			legDur(0, -5, 0, 1000),
		}}, ErrInvalidTrajectory},
		{"end before start", TrajectoryInput{Segments: []SegmentInput{
			leg(10, 5, 0, 1000),
		}}, ErrInvalidTrajectory},
		{"broken time chain", TrajectoryInput{Segments: []SegmentInput{
			leg(0, 100, 0, 5000),
			leg(101, 200, 5000, 8000),
		}}, ErrInvalidTrajectory},
		{"broken altitude chain", TrajectoryInput{Segments: []SegmentInput{
			leg(0, 100, 0, 5000),
			leg(100, 200, 5010, 8000),
		}}, ErrInvalidTrajectory},
		{"start below sea level", TrajectoryInput{Segments: []SegmentInput{
			leg(0, 100, -1, 5000),
		}}, ErrAltitudeBelowSeaLevel},
		{"end above ceiling", TrajectoryInput{Segments: []SegmentInput{
			leg(0, 100, 5000, 20001),
		}}, ErrAltitudeAboveCeiling},
		{"later leg starts above ceiling", TrajectoryInput{Segments: []SegmentInput{
			leg(0, 100, 0, 1000),
			leg(100, 200, 20001, 0),
		}}, ErrAltitudeAboveCeiling},
		{"non-finite time", TrajectoryInput{Segments: []SegmentInput{
			leg(math.NaN(), 1, 0, 1000),
		}}, ErrInvalidTrajectory},
		{"non-finite altitude", TrajectoryInput{Segments: []SegmentInput{
			leg(0, 1, math.Inf(1), 1000),
		}}, ErrInvalidTrajectory},
		{"offset too cold", TrajectoryInput{
			TemperatureOffsetK: -300,
			Segments:           []SegmentInput{leg(0, 1, 0, 1000)},
		}, ErrInvalidTemperatureOffset},
	}
	// Replace the one case whose builder can't express "missing altitude".
	for i := range cases {
		if cases[i].name == "missing start altitude" {
			cases[i].in.Segments[0] = SegmentInput{
				StartTimeS: ptr(0), EndTimeS: ptr(1), EndAltM: ptr(1000),
			}
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := AccumulateFromInput(tc.in)
			if err == nil {
				t.Fatalf("expected error %s, got success", tc.wantCode)
			}
			if err.Code != tc.wantCode {
				t.Errorf("code = %s, want %s; message: %s", err.Code, tc.wantCode, err.Message)
			}
			if err.Message == "" {
				t.Errorf("error message is empty")
			}
		})
	}
}

// The error message for a broken chain must name the leg that broke it.
func TestBrokenChainMessageNamesLeg(t *testing.T) {
	_, err := AccumulateFromInput(TrajectoryInput{Segments: []SegmentInput{
		leg(0, 100, 0, 5000),
		leg(100, 200, 5000, 8000),
		leg(200, 300, 9000, 11000), // leg 3 starts at 9000, leg 2 ended at 8000
	}})
	if err == nil || err.Code != ErrInvalidTrajectory {
		t.Fatalf("got %v, want INVALID_TRAJECTORY", err)
	}
	if !contains(err.Message, "leg 3") || !contains(err.Message, "leg 2") {
		t.Errorf("message %q does not name legs 2 and 3", err.Message)
	}
}

// Duration-based legs are equivalent to absolute-time legs.
func TestDurationLegsEquivalentToAbsoluteTimes(t *testing.T) {
	a := mustAccumulate(t, TrajectoryInput{Segments: []SegmentInput{
		leg(0, 500, 0, 10000),
		leg(500, 1500, 10000, 10000),
	}})
	b := mustAccumulate(t, TrajectoryInput{Segments: []SegmentInput{
		legDur(0, 500, 0, 10000),
		legDur(500, 1000, 10000, 10000),
	}})
	if a.TraversedAirMassKGM2 != b.TraversedAirMassKGM2 ||
		a.MeanDensityKGM3 != b.MeanDensityKGM3 {
		t.Errorf("duration-based legs differ from absolute-time legs: %+v vs %+v", a, b)
	}
	if b.Segments[1].EndTimeS != 1500 {
		t.Errorf("duration leg end time = %.1f, want 1500", b.Segments[1].EndTimeS)
	}
}

// Continuity tolerance: a chain whose adjacent values differ by a floating
// whisker is accepted and snapped together; visibly broken chains are not.
func TestContinuityTolerance(t *testing.T) {
	eps := 1e-12
	_, err := AccumulateFromInput(TrajectoryInput{Segments: []SegmentInput{
		leg(0, 100, 0, 5000),
		leg(100+eps, 200, 5000+eps, 8000),
	}})
	if err != nil {
		t.Errorf("whisker-level discontinuity rejected: %v", err)
	}
	_, err = AccumulateFromInput(TrajectoryInput{Segments: []SegmentInput{
		leg(0, 100, 0, 5000),
		leg(100, 200, 5000.1, 8000),
	}})
	if err == nil || err.Code != ErrInvalidTrajectory {
		t.Errorf("10 cm altitude break accepted, want rejection")
	}
}

// Performance: thousands of legs with hundreds of metres of altitude span
// each must complete comfortably fast (8 GL evaluations per piece).
func TestPerformanceThousandsOfLegs(t *testing.T) {
	const n = 4000
	segs := make([]SegmentInput, n)
	for i := 0; i < n; i++ {
		// 4000 legs alternating 0->500 and 500->0 m.
		if i%2 == 0 {
			segs[i] = leg(float64(i), float64(i)+1, 0, 500)
		} else {
			segs[i] = leg(float64(i), float64(i)+1, 500, 0)
		}
	}
	start := time.Now()
	res := mustAccumulate(t, TrajectoryInput{Segments: segs})
	elapsed := time.Since(start)
	if res.LegCount != n {
		t.Errorf("leg count = %d, want %d", res.LegCount, n)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("accumulating %d legs took %v, want under 500 ms", n, elapsed)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
