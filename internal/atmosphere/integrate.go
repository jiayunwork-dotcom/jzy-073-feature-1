package atmosphere

import "math"

// This file is the numerical quadrature used for along-trajectory
// accumulation. Everything reduces to one-dimensional integrals of model
// quantities (pressure, density) over geometric altitude.
//
// The one rule that matters here: the tropopause at TropopauseAltitude is a
// FORCED quadrature node. The model is two analytic layers spliced together —
// continuous in value, but the derivatives of pressure and density kink at
// 11 km. A smooth quadrature rule spanning the junction would sample across
// the kink and silently bias the result, so any interval that straddles the
// junction is split there first and the two sides are integrated separately.

// integrationRelTol is the relative tolerance the adaptive integrator works
// to. It is deliberately far tighter than the tolerances the accumulated
// results are checked against (1e-9), so quadrature error never leaks into
// the service's answers.
const integrationRelTol = 1e-12

// simpsonMaxDepth caps the adaptive refinement recursion. The integrands are
// analytic on each layer, so convergence happens long before this depth; the
// cap only guards against pathological input.
const simpsonMaxDepth = 40

// integrateOverAltitude integrates f(h) over geometric altitude from a to b
// (a <= b, both inside the model domain). The interval is first decomposed
// at every layer junction it straddles, then each smooth piece is integrated
// by adaptive Simpson quadrature. integrateOverAltitude(f, a, a) == 0.
func integrateOverAltitude(f func(h float64) float64, a, b float64) float64 {
	if a == b {
		return 0
	}
	total := 0.0
	for _, piece := range splitAtJunctions(a, b) {
		total += adaptiveSimpson(f, piece[0], piece[1])
	}
	return total
}

// splitAtJunctions decomposes [a, b] into consecutive sub-intervals that
// never straddle a layer junction. The two-layer model has exactly one
// interior junction — the tropopause at TropopauseAltitude — so the result
// is either [a, b] itself or the two pieces [a, junction], [junction, b].
// A junction coinciding with an endpoint is already a boundary and does not
// produce an empty piece.
func splitAtJunctions(a, b float64) [][2]float64 {
	if a < TropopauseAltitude && TropopauseAltitude < b {
		return [][2]float64{{a, TropopauseAltitude}, {TropopauseAltitude, b}}
	}
	return [][2]float64{{a, b}}
}

// adaptiveSimpson integrates the smooth function f over [a, b] by recursive
// adaptive Simpson quadrature to integrationRelTol relative accuracy.
func adaptiveSimpson(f func(h float64) float64, a, b float64) float64 {
	fa, fb := f(a), f(b)
	m := 0.5 * (a + b)
	fm := f(m)
	whole := simpsonEstimate(a, b, fa, fm, fb)
	tol := integrationRelTol * math.Max(1.0, math.Abs(whole))
	return simpsonRefine(f, a, b, fa, fm, fb, whole, tol, simpsonMaxDepth)
}

// simpsonEstimate is Simpson's rule over [a, b] given the endpoint values
// and the midpoint value.
func simpsonEstimate(a, b, fa, fm, fb float64) float64 {
	return (b - a) / 6 * (fa + 4*fm + fb)
}

// simpsonRefine bisects [a, b] and compares the two half-interval estimates
// with the whole-interval one. By Richardson extrapolation the refined
// estimate's error is about (left+right-whole)/15; once that is within tol
// the extrapolated value is returned, otherwise each half is refined
// recursively with halved tolerance.
func simpsonRefine(f func(h float64) float64, a, b, fa, fm, fb, whole, tol float64, depth int) float64 {
	m := 0.5 * (a + b)
	lm := 0.5 * (a + m)
	rm := 0.5 * (m + b)
	flm, frm := f(lm), f(rm)
	left := simpsonEstimate(a, m, fa, flm, fm)
	right := simpsonEstimate(m, b, fm, frm, fb)
	delta := left + right - whole
	if depth <= 0 || math.Abs(delta) <= 15*tol {
		return left + right + delta/15
	}
	return simpsonRefine(f, a, m, fa, flm, fm, left, tol/2, depth-1) +
		simpsonRefine(f, m, b, fm, frm, fb, right, tol/2, depth-1)
}
