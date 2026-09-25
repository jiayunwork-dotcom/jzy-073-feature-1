package atmosphere

import (
	"math"
	"testing"
)

// Rule: the tropopause is a forced quadrature node. Any interval straddling
// 11 km is decomposed there; intervals not straddling it (including ones
// merely touching it at an endpoint) are left alone.
func TestSplitAtJunctions(t *testing.T) {
	cases := []struct {
		name string
		a, b float64
		want [][2]float64
	}{
		{"below junction", 5000, 10000, [][2]float64{{5000, 10000}}},
		{"above junction", 12000, 15000, [][2]float64{{12000, 15000}}},
		{"straddling", 10000, 12000, [][2]float64{{10000, TropopauseAltitude}, {TropopauseAltitude, 12000}}},
		{"full model span", 0, ModelTopAltitude, [][2]float64{{0, TropopauseAltitude}, {TropopauseAltitude, ModelTopAltitude}}},
		{"junction is start", TropopauseAltitude, 15000, [][2]float64{{TropopauseAltitude, 15000}}},
		{"junction is end", 5000, TropopauseAltitude, [][2]float64{{5000, TropopauseAltitude}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitAtJunctions(tc.a, tc.b)
			if len(got) != len(tc.want) {
				t.Fatalf("splitAtJunctions(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("splitAtJunctions(%v, %v) piece %d = %v, want %v",
						tc.a, tc.b, i, got[i], tc.want[i])
				}
			}
			// The pieces must tile [a, b] exactly: consecutive, no overlap.
			if got[0][0] != tc.a || got[len(got)-1][1] != tc.b {
				t.Errorf("pieces do not span [%v, %v]: %v", tc.a, tc.b, got)
			}
			for i := 1; i < len(got); i++ {
				if got[i][0] != got[i-1][1] {
					t.Errorf("pieces not consecutive: %v", got)
				}
			}
		})
	}
}

// Rule: a kinked integrand is integrated exactly when the kink sits on the
// junction. |h - 11km| is piecewise linear with its kink at the tropopause;
// Simpson's rule is exact for linear functions, so splitting at the junction
// must reproduce the analytic integral to machine precision. A quadrature
// that smoothed over the junction could not do this.
func TestIntegrateKinkedFunctionExactAtJunction(t *testing.T) {
	v := func(h float64) float64 { return math.Abs(h - TropopauseAltitude) }

	// Symmetric span [10000, 12000]: two triangles of side 1000.
	got := integrateOverAltitude(v, 10000, 12000)
	want := 1000.0 * 1000.0 // 2 * (1000^2 / 2)
	if !approxEq(got, want, tolEq) {
		t.Errorf("∫|h-11km|dh over [10000,12000] = %.12f, want %.12f", got, want)
	}

	// Asymmetric span [10500, 11700]: (500^2 + 700^2) / 2.
	got = integrateOverAltitude(v, 10500, 11700)
	want = (500.0*500.0 + 700.0*700.0) / 2
	if !approxEq(got, want, tolEq) {
		t.Errorf("∫|h-11km|dh over [10500,11700] = %.12f, want %.12f", got, want)
	}
}

// Rule: the integrator reproduces analytic references on the model's own
// functions. The model is hydrostatic by construction, so the density column
// between two altitudes must equal the pressure difference divided by g.
// This holds inside each layer and across the junction, and it only comes
// out this clean because the integrator samples the model's own formulas.
func TestIntegrateDensityMatchesHydrostaticColumn(t *testing.T) {
	spans := [][2]float64{
		{0, 5000},                 // troposphere only
		{12000, ModelTopAltitude}, // stratosphere only
		{10000, 12000},            // straddling the junction
		{0, ModelTopAltitude},     // whole model
		{3000, 3000},              // degenerate: zero width
	}
	for _, span := range spans {
		lo, hi := span[0], span[1]
		got := integrateOverAltitude(standardDensityAt, lo, hi)
		sLo, errLo := Compute(lo, 0)
		sHi, errHi := Compute(hi, 0)
		if errLo != nil || errHi != nil {
			t.Fatalf("Compute error: %v, %v", errLo, errHi)
		}
		want := (sLo.Pressure - sHi.Pressure) / Gravity
		if !approxEq(got, want, 1e-9) {
			t.Errorf("density column over [%.0f, %.0f] = %.9f kg/m^2, hydrostatic reference %.9f",
				lo, hi, got, want)
		}
	}
}

// Rule: quadrature is additive over sub-intervals — integrating a span in
// one piece must agree with integrating it in two pieces and summing. This
// is the integral-level form of the trajectory additivity invariant.
func TestIntegrateAdditiveOverSubintervals(t *testing.T) {
	whole := integrateOverAltitude(standardDensityAt, 0, ModelTopAltitude)
	parts := integrateOverAltitude(standardDensityAt, 0, TropopauseAltitude) +
		integrateOverAltitude(standardDensityAt, TropopauseAltitude, ModelTopAltitude)
	if !approxEq(whole, parts, tolEq) {
		t.Errorf("additivity broken: whole %.12f vs split %.12f kg/m^2", whole, parts)
	}
	// Same for pressure, and with a split point that is NOT the junction.
	wholeP := integrateOverAltitude(standardPressureAt, 2000, 18000)
	partsP := integrateOverAltitude(standardPressureAt, 2000, 7777) +
		integrateOverAltitude(standardPressureAt, 7777, 18000)
	if !approxEq(wholeP, partsP, tolEq) {
		t.Errorf("pressure additivity broken: whole %.12f vs split %.12f", wholeP, partsP)
	}
}
