package atmosphere

import "math"

// This file integrates atmospheric quantities along a validated trajectory.
//
// The model is piecewise defined with a slope kink at TropopauseAltitude:
// temperature is linear below and constant above, so dp/dh and d rho/dh are
// discontinuous at the junction even though p and rho themselves are
// continuous. A single smooth quadrature rule applied across that kink would
// bias every crossing leg. The junction is therefore a MANDATORY integration
// node: every leg is split into within-layer pieces (see splitAtJunction) and
// each piece is integrated separately with a high-order Gauss-Legendre rule.
//
// Every sampled altitude is evaluated through evaluate (the same core behind
// Compute), so the integrator uses no constants or formulas of its own.

// gaussLegendre8 holds the 8-point Gauss-Legendre nodes on (-1,1) and their
// weights. Degree-15 polynomial exactness is far more than enough on either
// side of the kink; the smooth pressure/density profiles are integrated to
// round-off with a single rule application per piece.
var gaussLegendre8 = struct {
	nodes   [8]float64
	weights [8]float64
}{
	nodes: [8]float64{
		-0.9602898564975363,
		-0.7966664774136267,
		-0.5255324099163290,
		-0.1834346424956498,
		0.1834346424956498,
		0.5255324099163290,
		0.7966664774136267,
		0.9602898564975363,
	},
	weights: [8]float64{
		0.1012285362903763,
		0.2223810344533745,
		0.3137066458778873,
		0.3626837833783620,
		0.3626837833783620,
		0.3137066458778873,
		0.2223810344533745,
		0.1012285362903763,
	},
}

// piece is one within-layer integration span of one leg. Non-crossing legs
// produce a single piece equal to the leg; legs crossing TropopauseAltitude
// produce two pieces that meet at the junction.
type piece struct {
	startTimeS float64
	endTimeS   float64
	startAltM  float64
	endAltM    float64
}

// splitAtJunction cuts a leg at TropopauseAltitude whenever the junction lies
// strictly inside its altitude interval. Pieces touching the junction from
// either side stay entirely within one layer, so every piece is smooth.
func splitAtJunction(l Leg) []piece {
	h0, h1 := l.StartAltM, l.EndAltM
	crosses := (h0 < TropopauseAltitude && h1 > TropopauseAltitude) ||
		(h0 > TropopauseAltitude && h1 < TropopauseAltitude)
	if !crosses {
		return []piece{{l.StartTimeS, l.EndTimeS, h0, h1}}
	}
	// Linear altitude law: time at which the leg passes the junction.
	frac := (TropopauseAltitude - h0) / (h1 - h0)
	tJ := l.StartTimeS + frac*l.DurationS
	return []piece{
		{l.StartTimeS, tJ, h0, TropopauseAltitude},
		{tJ, l.EndTimeS, TropopauseAltitude, h1},
	}
}

// pieceIntegrals holds every quantity accumulated over one smooth piece.
type pieceIntegrals struct {
	// massColumn is integral |rho dh| over the geometric altitude path:
	// kilograms of air traversed per square metre of horizontal cross section.
	massColumn float64
	// timeIntegrals of pressure (Pa*s), density (kg/m^3*s) and operative
	// temperature (K*s) drive the time-weighted mission averages.
	pressureTime    float64
	densityTime     float64
	temperatureTime float64
}

// integratePiece integrates one within-layer piece with 8-point
// Gauss-Legendre quadrature. A level piece is exact by construction: it
// samples one altitude, contributes exactly zero column mass (no altitude is
// traversed) and time means equal the single-point state bit for bit.
func integratePiece(p piece, temperatureOffsetK float64) (pieceIntegrals, *ModelError) {
	duration := p.endTimeS - p.startTimeS
	halfDh := 0.5 * (p.endAltM - p.startAltM)

	// Level piece: h is constant, so every "integral" is the point value
	// times the duration; column mass is exactly zero.
	if halfDh == 0 {
		_, temp, press, rho, err := evaluate(p.startAltM, temperatureOffsetK)
		if err != nil {
			return pieceIntegrals{}, err
		}
		return pieceIntegrals{
			massColumn:      0,
			pressureTime:    press * duration,
			densityTime:     rho * duration,
			temperatureTime: temp * duration,
		}, nil
	}

	midAlt := 0.5 * (p.startAltM + p.endAltM)
	halfDt := 0.5 * duration
	// |halfDh| in the geometric measure makes a climb and the matching
	// descent traverse bit-for-bit identical air mass.
	absHalfDh := math.Abs(halfDh)

	var out pieceIntegrals
	for k := 0; k < len(gaussLegendre8.nodes); k++ {
		// Iterate nodes so that sampled altitudes are always visited in
		// ascending order. Nodes and weights are symmetric, so a descending
		// piece just uses the mirrored node; this permutation keeps the rule
		// exact while making a climb and the matching descent accumulate
		// bit-for-bit identical sums (same evaluation and summation order).
		idx := k
		if halfDh < 0 {
			idx = len(gaussLegendre8.nodes) - 1 - k
		}
		xi := gaussLegendre8.nodes[idx]
		w := gaussLegendre8.weights[idx]
		// Linear altitude law inside the leg: the same xi maps t and h.
		h := midAlt + halfDh*xi

		_, temp, press, rho, err := evaluate(h, temperatureOffsetK)
		if err != nil {
			return pieceIntegrals{}, err
		}
		out.massColumn += w * rho * absHalfDh
		out.pressureTime += w * press * halfDt
		out.densityTime += w * rho * halfDt
		out.temperatureTime += w * temp * halfDt
	}
	return out, nil
}
