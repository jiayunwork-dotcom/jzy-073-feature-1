package atmosphere

import "math"

// State is the complete atmospheric state at one altitude.
//
// StandardTemperature is the pure ISA temperature; Temperature is the
// operative temperature used for density and speed of sound: it equals
// StandardTemperature + temperatureOffsetK. Pressure always follows the
// standard pressure profile regardless of the offset; density is recomputed
// from standard pressure and the operative temperature.
type State struct {
	Altitude            float64 `json:"altitude_m"`
	Layer               string  `json:"layer"`
	Temperature         float64 `json:"temperature_k"`
	StandardTemperature float64 `json:"standard_temperature_k"`
	Pressure            float64 `json:"pressure_pa"`
	Density             float64 `json:"density_kg_m3"`
	SpeedOfSound        float64 `json:"speed_of_sound_m_s"`
	// DensityAltitude is the altitude where the STANDARD atmosphere has the
	// same density as the operative density here. It is null when the
	// operative density has no equivalent altitude inside the 0..20 km model.
	DensityAltitude *float64 `json:"density_altitude_m"`
}

// Compute evaluates the atmosphere at geometric altitude h (metres).
//
// temperatureOffsetK shifts the operative temperature to model a warmer or
// colder day. The pressure profile stays the standard one; only temperature,
// density (ideal-gas with the shifted temperature) and speed of sound react
// to it. With offset 0 every returned value is the pure ISA value.
//
// DensityAltitude is the altitude where the STANDARD atmosphere has the same
// density as the operative density here. With offset 0 it equals h; with a
// non-zero offset it differs from h (that difference is the point of density
// altitude). It is null when the operative density has no equivalent
// altitude inside the 0..20 km standard model.
func Compute(h, temperatureOffsetK float64) (State, *ModelError) {
	if err := validateAltitude(h); err != nil {
		return State{}, err
	}
	if math.IsNaN(temperatureOffsetK) || math.IsInf(temperatureOffsetK, 0) {
		return State{}, errInvalidTemperatureOffset(temperatureOffsetK, 0)
	}

	var layer string
	var stdT, p float64
	if h <= TropopauseAltitude {
		layer = "troposphere"
		stdT = TroposphericTemperature(h)
		p = TroposphericPressure(h)
	} else {
		layer = "stratosphere"
		stdT = StratosphericTemperature(h)
		p = StratosphericPressure(h)
	}

	t := stdT + temperatureOffsetK
	if t <= 0 {
		return State{}, errInvalidTemperatureOffset(temperatureOffsetK, t)
	}

	// rho = p / (R*T): standard pressure profile, operative temperature.
	rho := p / (SpecificGasConstant * t)

	var densityAltitude *float64
	if dh, invErr := DensityAltitude(rho); invErr == nil {
		densityAltitude = &dh
	}

	return State{
		Altitude:            h,
		Layer:               layer,
		Temperature:         t,
		StandardTemperature: stdT,
		Pressure:            p,
		Density:             rho,
		SpeedOfSound:        SpeedOfSound(t),
		DensityAltitude:     densityAltitude,
	}, nil
}

// Profile evaluates the atmosphere at start, start+step, ... up to and
// including end (end is always the final point even when it does not land on
// the step grid). Points beyond the model ceiling are never produced.
func Profile(start, end, step, temperatureOffsetK float64) ([]State, *ModelError) {
	if math.IsNaN(start) || math.IsInf(start, 0) || math.IsNaN(end) || math.IsInf(end, 0) {
		return nil, &ModelError{Code: ErrInvalidRange, Message: "start and end must be finite numbers"}
	}
	if start < 0 {
		return nil, errBelowSeaLevel(start)
	}
	if end > ModelTopAltitude {
		return nil, errAboveCeiling(end)
	}
	if end < start {
		return nil, &ModelError{
			Code:    ErrInvalidRange,
			Message: "end altitude must be greater than or equal to start altitude",
		}
	}
	if math.IsNaN(step) || math.IsInf(step, 0) || step <= 0 {
		return nil, &ModelError{Code: ErrInvalidStep, Message: "step must be a positive number of metres"}
	}
	span := end - start
	if span/step+1 > MaxProfilePoints {
		return nil, &ModelError{
			Code:    ErrInvalidStep,
			Message: "profile would produce more than 100000 points; use a larger step",
		}
	}

	n := int(span/step) + 1
	states := make([]State, 0, n+1)
	for i := 0; i < n; i++ {
		h := start + float64(i)*step
		// Guard against floating-point overshoot near the end point.
		if h > end {
			break
		}
		s, err := Compute(h, temperatureOffsetK)
		if err != nil {
			return nil, err
		}
		states = append(states, s)
	}

	// Guarantee the declared interval end is present exactly.
	last := start + float64(n-1)*step
	if len(states) == 0 || math.Abs(last-end) > 1e-9*math.Max(1.0, end) {
		s, err := Compute(end, temperatureOffsetK)
		if err != nil {
			return nil, err
		}
		states = append(states, s)
	}
	return states, nil
}

// SpeedOfSound returns the adiabatic speed of sound in an ideal gas:
//
//	a = sqrt(gamma * R * T)
//
// It depends only on absolute temperature, never on pressure.
func SpeedOfSound(t float64) float64 {
	return math.Sqrt(HeatCapacityRatio * SpecificGasConstant * t)
}

// standardPressureAt and standardDensityAt evaluate the pure standard
// atmosphere (zero temperature offset) at geometric altitude h by
// dispatching to the layer formulas exactly the way Compute does. They are
// the sampling points of the trajectory integrator: integrating these
// functions is integrating the model's own single implementation, never a
// re-derived copy. h must already be validated inside the model domain.
// At h == TropopauseAltitude both branches agree (junction continuity is
// pinned by tests), so the branch choice there is immaterial.
func standardPressureAt(h float64) float64 {
	if h <= TropopauseAltitude {
		return TroposphericPressure(h)
	}
	return StratosphericPressure(h)
}

func standardDensityAt(h float64) float64 {
	if h <= TropopauseAltitude {
		return TroposphericDensity(h)
	}
	// Evaluate through the same runtime operations Compute performs (layer
	// temperature in a variable, then the ideal-gas division) instead of
	// calling StratosphericDensity: that formula's denominator is a
	// compile-time constant expression, which can round one ulp differently
	// from Compute's runtime multiplication, and a level leg's mean density
	// must be bit-identical to the single-point query.
	stdT := StratosphericTemperature(h)
	return StratosphericPressure(h) / (SpecificGasConstant * stdT)
}

func validateAltitude(h float64) *ModelError {
	if math.IsNaN(h) || math.IsInf(h, 0) {
		return errInvalidAltitude(h)
	}
	if h < 0 {
		return errBelowSeaLevel(h)
	}
	if h > ModelTopAltitude {
		return errAboveCeiling(h)
	}
	return nil
}
