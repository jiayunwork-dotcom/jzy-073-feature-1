package atmosphere

// This file turns a validated trajectory into the delivered cumulative
// accounting: the air mass traversed per unit horizontal area (a geometric
// integral over altitude) and the time-weighted mission-mean pressure,
// density and temperature. Per-leg ("small ledger") and whole-trajectory
// ("master ledger") results are returned together.

// SegmentResult is the cumulative contribution of one leg. A leg crossing the
// tropopause is integrated as two smooth pieces; CrossesTropopause exposes
// that mandatory split.
type SegmentResult struct {
	Index             int     `json:"index"`
	StartTimeS        float64 `json:"start_time_s"`
	EndTimeS          float64 `json:"end_time_s"`
	DurationS         float64 `json:"duration_s"`
	StartAltM         float64 `json:"start_altitude_m"`
	EndAltM           float64 `json:"end_altitude_m"`
	VerticalRateMps   float64 `json:"vertical_rate_mps"`
	CrossesTropopause bool    `json:"crosses_tropopause"`

	// TraversedAirMassKGM2 = integral |rho dh| along the leg. For a level
	// leg (start altitude == end altitude) it is exactly zero.
	TraversedAirMassKGM2 float64 `json:"traversed_air_mass_kg_m2"`

	// Time-weighted mean state over the leg.
	MeanPressurePa   float64 `json:"mean_pressure_pa"`
	MeanDensityKGM3  float64 `json:"mean_density_kg_m3"`
	MeanTemperatureK float64 `json:"mean_temperature_k"`

	// Full atmospheric state at both leg endpoints, evaluated through the
	// single-point implementation so leg numbers can be checked against the
	// point endpoint directly.
	StartState State `json:"start_state"`
	EndState   State `json:"end_state"`
}

// TrajectoryResult is the master ledger over the whole chain plus the per-leg
// breakdown.
type TrajectoryResult struct {
	StartTimeS float64 `json:"start_time_s"`
	EndTimeS   float64 `json:"end_time_s"`
	DurationS  float64 `json:"duration_s"`
	StartAltM  float64 `json:"start_altitude_m"`
	EndAltM    float64 `json:"end_altitude_m"`
	LegCount   int     `json:"leg_count"`

	TemperatureOffsetK float64 `json:"temperature_offset_k"`

	// TraversedAirMassKGM2 is the sum of all per-leg geometric air-column
	// masses: a pure geometric quantity, invariant under time translation
	// and time scaling as long as the altitude walk is unchanged.
	TraversedAirMassKGM2 float64 `json:"traversed_air_mass_kg_m2"`

	// Time-weighted means over the whole trajectory.
	MeanPressurePa   float64 `json:"mean_pressure_pa"`
	MeanDensityKGM3  float64 `json:"mean_density_kg_m3"`
	MeanTemperatureK float64 `json:"mean_temperature_k"`

	Segments []SegmentResult `json:"segments"`
}

// Accumulate integrates the atmosphere along the validated trajectory.
func Accumulate(tr *Trajectory) (*TrajectoryResult, *ModelError) {
	if tr == nil || len(tr.Legs) == 0 {
		return nil, errTrajectoryf("cannot accumulate an empty trajectory")
	}
	offset := tr.TemperatureOffsetK

	segments := make([]SegmentResult, 0, len(tr.Legs))
	var totalMass, totalPressureTime, totalDensityTime, totalTemperatureTime float64

	for _, leg := range tr.Legs {
		pieces := splitAtJunction(leg)
		seg := SegmentResult{
			Index:             leg.Index,
			StartTimeS:        leg.StartTimeS,
			EndTimeS:          leg.EndTimeS,
			DurationS:         leg.DurationS,
			StartAltM:         leg.StartAltM,
			EndAltM:           leg.EndAltM,
			VerticalRateMps:   leg.VerticalRateMps,
			CrossesTropopause: len(pieces) > 1,
		}

		var legMass, legPT, legRT, legTT float64
		for _, p := range pieces {
			pi, err := integratePiece(p, offset)
			if err != nil {
				return nil, err
			}
			legMass += pi.massColumn
			legPT += pi.pressureTime
			legRT += pi.densityTime
			legTT += pi.temperatureTime
		}

		startState, err := Compute(leg.StartAltM, offset)
		if err != nil {
			return nil, err
		}
		endState, err := Compute(leg.EndAltM, offset)
		if err != nil {
			return nil, err
		}

		seg.TraversedAirMassKGM2 = legMass
		seg.MeanPressurePa = legPT / leg.DurationS
		seg.MeanDensityKGM3 = legRT / leg.DurationS
		seg.MeanTemperatureK = legTT / leg.DurationS
		seg.StartState = startState
		seg.EndState = endState
		segments = append(segments, seg)

		totalMass += legMass
		totalPressureTime += legPT
		totalDensityTime += legRT
		totalTemperatureTime += legTT
	}

	totalDuration := tr.Legs[len(tr.Legs)-1].EndTimeS - tr.Legs[0].StartTimeS
	return &TrajectoryResult{
		StartTimeS:           tr.Legs[0].StartTimeS,
		EndTimeS:             tr.Legs[len(tr.Legs)-1].EndTimeS,
		DurationS:            totalDuration,
		StartAltM:            tr.Legs[0].StartAltM,
		EndAltM:              tr.Legs[len(tr.Legs)-1].EndAltM,
		LegCount:             len(tr.Legs),
		TemperatureOffsetK:   offset,
		TraversedAirMassKGM2: totalMass,
		MeanPressurePa:       totalPressureTime / totalDuration,
		MeanDensityKGM3:      totalDensityTime / totalDuration,
		MeanTemperatureK:     totalTemperatureTime / totalDuration,
		Segments:             segments,
	}, nil
}

// AccumulateFromInput is the one-shot convenience used by the service layer:
// parse, validate and accumulate a raw request, or return its first error.
func AccumulateFromInput(in TrajectoryInput) (*TrajectoryResult, *ModelError) {
	tr, err := ParseTrajectory(in)
	if err != nil {
		return nil, err
	}
	return Accumulate(tr)
}
