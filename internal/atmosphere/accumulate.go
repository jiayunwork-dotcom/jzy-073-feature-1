package atmosphere

import "math"

// This file accumulates the standard atmosphere along a validated flight
// trajectory, per segment and in total.
//
// The aircraft flies along time but crosses atmospheric layers along
// altitude; within one segment the two are linked by the constant climb
// rate r = dh/dt, so dt = dh/r and every time integral over a leg becomes
// an altitude integral over the leg's altitude span:
//
//	column mass traversed  M = ∫ ρ(h) |dh|        (always >= 0)
//	pressure-time integral   = ∫ p(h) dt = (∫ p(h) dh) / |r|
//	density-time integral    = ∫ ρ(h) dt = M / |r|
//
// The column mass is the mass of the air column the aircraft passed
// through, per unit area: descending through a layer traverses it just as
// climbing does, so the altitude element enters with its absolute value and
// the quantity accumulates along the path instead of cancelling out. Level
// flight crosses no altitude and contributes exactly zero.
//
// All altitude integrals go through integrateOverAltitude, which splits at
// the tropopause, and all sampled values come from standardPressureAt /
// standardDensityAt — the model's own formulas, never copies.

// SegmentAccumulation is the per-segment ledger: how much air column this
// leg traversed and the time-weighted mean conditions along it.
type SegmentAccumulation struct {
	Index           int     `json:"index"`
	StartTimeS      float64 `json:"start_time_s"`
	EndTimeS        float64 `json:"end_time_s"`
	DurationS       float64 `json:"duration_s"`
	StartAltitudeM  float64 `json:"start_altitude_m"`
	EndAltitudeM    float64 `json:"end_altitude_m"`
	ClimbRateMS     float64 `json:"climb_rate_m_s"`
	ColumnMassKgM2  float64 `json:"column_mass_kg_m2"`
	MeanPressurePa  float64 `json:"mean_pressure_pa"`
	MeanDensityKgM3 float64 `json:"mean_density_kg_m3"`

	// Raw time integrals backing the means; the totals aggregate these.
	// Unexported: callers read the means, not the integrals.
	pressureTimeIntegral float64
	densityTimeIntegral  float64
}

// TrajectoryTotals is the whole-trajectory ledger. MeanPressurePa and
// MeanDensityKgM3 are weighted by segment duration, so a long cruise counts
// more than a brief climb — they answer "how dense was the air this mission
// sat in, on average over time".
type TrajectoryTotals struct {
	ColumnMassKgM2  float64 `json:"column_mass_kg_m2"`
	MeanPressurePa  float64 `json:"mean_pressure_pa"`
	MeanDensityKgM3 float64 `json:"mean_density_kg_m3"`
}

// TrajectoryAccumulation is the full result: the total ledger plus the
// per-segment sub-ledgers for checking individual legs.
type TrajectoryAccumulation struct {
	SegmentCount int                   `json:"segment_count"`
	StartTimeS   float64               `json:"start_time_s"`
	EndTimeS     float64               `json:"end_time_s"`
	DurationS    float64               `json:"duration_s"`
	MinAltitudeM float64               `json:"min_altitude_m"`
	MaxAltitudeM float64               `json:"max_altitude_m"`
	Total        TrajectoryTotals      `json:"total"`
	Segments     []SegmentAccumulation `json:"segments"`
}

// AccumulateTrajectory integrates the standard atmosphere along the given
// flight trajectory. The trajectory is validated first; an illegal one is
// rejected wholesale (never partially integrated, never extrapolated).
func AccumulateTrajectory(segments []Segment) (TrajectoryAccumulation, *ModelError) {
	if err := ValidateTrajectory(segments); err != nil {
		return TrajectoryAccumulation{}, err
	}

	acc := TrajectoryAccumulation{
		SegmentCount: len(segments),
		StartTimeS:   segments[0].StartTime,
		EndTimeS:     segments[len(segments)-1].EndTime,
		MinAltitudeM: math.Inf(1),
		MaxAltitudeM: math.Inf(-1),
		Segments:     make([]SegmentAccumulation, 0, len(segments)),
	}
	var columnMass, pressureTime, densityTime float64
	for i, s := range segments {
		sa := accumulateSegment(i, s)
		acc.Segments = append(acc.Segments, sa)
		columnMass += sa.ColumnMassKgM2
		pressureTime += sa.pressureTimeIntegral
		densityTime += sa.densityTimeIntegral
		acc.MinAltitudeM = math.Min(acc.MinAltitudeM, math.Min(s.StartAltitude, s.EndAltitude))
		acc.MaxAltitudeM = math.Max(acc.MaxAltitudeM, math.Max(s.StartAltitude, s.EndAltitude))
	}
	acc.DurationS = acc.EndTimeS - acc.StartTimeS
	acc.Total = TrajectoryTotals{
		ColumnMassKgM2:  columnMass,
		MeanPressurePa:  pressureTime / acc.DurationS,
		MeanDensityKgM3: densityTime / acc.DurationS,
	}
	return acc, nil
}

// accumulateSegment computes one leg's ledger. The caller guarantees the
// segment is part of a validated trajectory.
func accumulateSegment(index int, s Segment) SegmentAccumulation {
	duration := s.Duration()
	out := SegmentAccumulation{
		Index:          index,
		StartTimeS:     s.StartTime,
		EndTimeS:       s.EndTime,
		DurationS:      duration,
		StartAltitudeM: s.StartAltitude,
		EndAltitudeM:   s.EndAltitude,
		ClimbRateMS:    s.ClimbRate(),
	}

	lo := math.Min(s.StartAltitude, s.EndAltitude)
	hi := math.Max(s.StartAltitude, s.EndAltitude)
	if lo == hi {
		// Level flight crosses no altitude: the traversed column mass is
		// exactly zero, and the time-weighted means are exactly the model's
		// point values at this altitude — bit-identical to what the
		// single-point query returns.
		p := standardPressureAt(lo)
		rho := standardDensityAt(lo)
		out.ColumnMassKgM2 = 0
		out.MeanPressurePa = p
		out.MeanDensityKgM3 = rho
		out.pressureTimeIntegral = p * duration
		out.densityTimeIntegral = rho * duration
		return out
	}

	// Climbing or descending: substitute dt = dh/|r| to move every time
	// integral into an altitude integral over [lo, hi]. The density column
	// is computed once and serves both the traversed column mass and the
	// density time integral, keeping the two exactly consistent.
	densityColumn := integrateOverAltitude(standardDensityAt, lo, hi)
	pressureColumn := integrateOverAltitude(standardPressureAt, lo, hi)
	absRate := math.Abs(out.ClimbRateMS)
	out.ColumnMassKgM2 = densityColumn
	out.pressureTimeIntegral = pressureColumn / absRate
	out.densityTimeIntegral = densityColumn / absRate
	out.MeanPressurePa = out.pressureTimeIntegral / duration
	out.MeanDensityKgM3 = out.densityTimeIntegral / duration
	return out
}
