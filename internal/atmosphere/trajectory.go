package atmosphere

import (
	"math"
)

// This file defines a flight trajectory: an ordered chain of legs, each one a
// constant vertical rate over a closed time interval. Parsing validates the
// chain the caller supplies and refuses anything the model cannot integrate
// honestly — broken continuity or any altitude outside the single existing
// atmosphere domain. Nothing here knows about HTTP.

// MaxTrajectoryLegs bounds the size of one trajectory request. Hundreds of
// sampling points per leg are cheap; this cap only rejects abusive payloads.
const MaxTrajectoryLegs = 100_000

// SegmentInput is one leg as supplied by the caller. The leg ends either at an
// absolute end_time_s or after duration_s; exactly one of the two must be set
// (a nil pointer means the field was absent from the JSON).
type SegmentInput struct {
	StartTimeS *float64 `json:"start_time_s"`
	EndTimeS   *float64 `json:"end_time_s,omitempty"`
	DurationS  *float64 `json:"duration_s,omitempty"`
	StartAltM  *float64 `json:"start_altitude_m"`
	EndAltM    *float64 `json:"end_altitude_m"`
}

// TrajectoryInput is the full along-trajectory accumulation request.
type TrajectoryInput struct {
	// TemperatureOffsetK shifts the operative temperature exactly as in the
	// single-point/profile endpoints; it defaults to zero (pure ISA).
	TemperatureOffsetK float64        `json:"temperature_offset_k"`
	Segments           []SegmentInput `json:"segments"`
}

// Leg is one validated trajectory leg: a linear altitude law
//
//	h(t) = StartAltM + VerticalRateMps * (t - StartTimeS)
//
// over [StartTimeS, EndTimeS]. Equal end altitudes means a level leg.
type Leg struct {
	Index           int     `json:"index"`
	StartTimeS      float64 `json:"start_time_s"`
	EndTimeS        float64 `json:"end_time_s"`
	DurationS       float64 `json:"duration_s"`
	StartAltM       float64 `json:"start_altitude_m"`
	EndAltM         float64 `json:"end_altitude_m"`
	VerticalRateMps float64 `json:"vertical_rate_mps"`
}

// Trajectory is a validated chain of legs plus the operative temperature
// offset applied at every altitude along the way.
type Trajectory struct {
	Legs               []Leg
	TemperatureOffsetK float64
}

// ParseTrajectory validates the raw request and produces a chain of legs.
//
// Rules enforced here:
//   - at least one leg, at most MaxTrajectoryLegs;
//   - every required field present and finite; exactly one of end_time_s /
//     duration_s given; duration strictly positive;
//   - every endpoint altitude inside the existing 0..ModelTopAltitude domain
//     (a linear leg cannot leave the range between two in-range endpoints);
//   - adjacent legs meet exactly (within floating-point tolerance) in both
//     time and altitude.
//
// The atmosphere itself is never extended: no leg endpoint may sit outside
// the domain, and no extrapolation is performed downstream.
func ParseTrajectory(in TrajectoryInput) (*Trajectory, *ModelError) {
	if len(in.Segments) == 0 {
		return nil, errTrajectoryf("trajectory must contain at least one leg")
	}
	if len(in.Segments) > MaxTrajectoryLegs {
		return nil, errTrajectoryf("trajectory has %d legs, more than the %d limit",
			len(in.Segments), MaxTrajectoryLegs)
	}
	if math.IsNaN(in.TemperatureOffsetK) || math.IsInf(in.TemperatureOffsetK, 0) {
		return nil, errInvalidTemperatureOffset(in.TemperatureOffsetK, 0)
	}

	legs := make([]Leg, 0, len(in.Segments))
	for i := range in.Segments {
		// 1-based index in every message: the position in the caller's chain.
		leg, err := parseLeg(i+1, in.Segments[i])
		if err != nil {
			return nil, err
		}
		if i > 0 {
			prev := legs[i-1]
			if !numbersMeet(prev.EndTimeS, leg.StartTimeS) {
				return nil, errTrajectoryf(
					"leg %d does not continue leg %d in time: leg %d ends at t=%.9g s but leg %d starts at t=%.9g s",
					leg.Index, prev.Index, prev.Index, prev.EndTimeS, leg.Index, leg.StartTimeS)
			}
			if !numbersMeet(prev.EndAltM, leg.StartAltM) {
				return nil, errTrajectoryf(
					"leg %d does not continue leg %d in altitude: leg %d ends at h=%.6g m but leg %d starts at h=%.6g m",
					leg.Index, prev.Index, prev.Index, prev.EndAltM, leg.Index, leg.StartAltM)
			}
			// Snap the new start onto the previous end so the arithmetic
			// chain is bit-for-bit continuous, not just tolerance-continuous,
			// and rebuild the rate from the snapped endpoints.
			leg.StartAltM = prev.EndAltM
			leg.StartTimeS = prev.EndTimeS
			leg.VerticalRateMps = (leg.EndAltM - leg.StartAltM) / (leg.EndTimeS - leg.StartTimeS)
		}
		legs = append(legs, leg)
	}

	// Validate the temperature offset against the coldest point actually
	// visited (the standard profile is coldest at the highest endpoint), so
	// that an offset yielding non-positive operative temperature is rejected
	// before any integration starts, with the existing domain error.
	highest := legs[0].StartAltM
	for _, l := range legs {
		highest = math.Max(highest, l.EndAltM)
	}
	if _, _, _, _, err := evaluate(highest, in.TemperatureOffsetK); err != nil {
		return nil, err
	}

	return &Trajectory{Legs: legs, TemperatureOffsetK: in.TemperatureOffsetK}, nil
}

func parseLeg(index int, s SegmentInput) (Leg, *ModelError) {
	if s.StartTimeS == nil {
		return Leg{}, errTrajectoryf("leg %d: missing required field \"start_time_s\" (seconds)", index)
	}
	if s.StartAltM == nil {
		return Leg{}, errTrajectoryf("leg %d: missing required field \"start_altitude_m\" (metres)", index)
	}
	if s.EndAltM == nil {
		return Leg{}, errTrajectoryf("leg %d: missing required field \"end_altitude_m\" (metres)", index)
	}
	switch {
	case s.EndTimeS == nil && s.DurationS == nil:
		return Leg{}, errTrajectoryf("leg %d: must specify exactly one of \"end_time_s\" or \"duration_s\", got neither", index)
	case s.EndTimeS != nil && s.DurationS != nil:
		return Leg{}, errTrajectoryf("leg %d: must specify exactly one of \"end_time_s\" or \"duration_s\", got both", index)
	}

	t0 := *s.StartTimeS
	h0 := *s.StartAltM
	h1 := *s.EndAltM
	if !finite(t0) {
		return Leg{}, errTrajectoryf("leg %d: start_time_s must be a finite number, got %.9g", index, t0)
	}
	if !finite(h0) {
		return Leg{}, errTrajectoryf("leg %d: start_altitude_m must be a finite number, got %.9g", index, h0)
	}
	if !finite(h1) {
		return Leg{}, errTrajectoryf("leg %d: end_altitude_m must be a finite number, got %.9g", index, h1)
	}
	if err := validateAltitude(h0); err != nil {
		return Leg{}, errTrajectoryAltitude(index, "start", err)
	}
	if err := validateAltitude(h1); err != nil {
		return Leg{}, errTrajectoryAltitude(index, "end", err)
	}

	var t1, givenDuration float64
	if s.EndTimeS != nil {
		t1 = *s.EndTimeS
		if !finite(t1) {
			return Leg{}, errTrajectoryf("leg %d: end_time_s must be a finite number, got %.9g", index, t1)
		}
	} else {
		givenDuration = *s.DurationS
		if !finite(givenDuration) {
			return Leg{}, errTrajectoryf("leg %d: duration_s must be a finite number, got %.9g", index, givenDuration)
		}
		if givenDuration <= 0 {
			return Leg{}, errTrajectoryf("leg %d: duration_s must be strictly positive, got %.9g", index, givenDuration)
		}
		t1 = t0 + givenDuration
	}
	if !finite(t1) {
		return Leg{}, errTrajectoryf("leg %d: end time overflows to a non-finite value (start %.9g plus %.9g s)",
			index, t0, givenDuration)
	}
	if t1 <= t0 {
		return Leg{}, errTrajectoryf("leg %d: end_time_s %.9g must be strictly after start_time_s %.9g (duration must be positive)",
			index, t1, t0)
	}

	duration := t1 - t0
	return Leg{
		Index:           index,
		StartTimeS:      t0,
		EndTimeS:        t1,
		DurationS:       duration,
		StartAltM:       h0,
		EndAltM:         h1,
		VerticalRateMps: (h1 - h0) / duration,
	}, nil
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// numbersMeet is the continuity test between adjacent legs: relative
// tolerance 1e-9 with an absolute floor, so vastly different scales (a flight
// starting at t=0 versus one starting at t=1e6) both behave sensibly.
func numbersMeet(a, b float64) bool {
	const relTol = 1e-9
	const absTol = 1e-9
	return math.Abs(a-b) <= absTol+relTol*math.Max(math.Abs(a), math.Abs(b))
}
