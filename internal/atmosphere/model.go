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
	stdT, t, p, rho, err := evaluate(h, temperatureOffsetK)
	if err != nil {
		return State{}, err
	}

	layer := "troposphere"
	if h > TropopauseAltitude {
		layer = "stratosphere"
	}

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

// evaluate is the single thermodynamic core of the model: given geometric
// altitude and temperature offset it returns the standard temperature, the
// operative temperature, the standard pressure and the operative density.
//
// Compute (single-point queries) and the along-trajectory integrator both go
// through this function, so there is exactly one implementation of the layer
// selection and of every formula. Integrated point values and single-point
// query values are therefore identical by construction; the integrator never
// keeps its own copy of constants or formulas.
func evaluate(h, temperatureOffsetK float64) (stdT, t, p, rho float64, err *ModelError) {
	if err = validateAltitude(h); err != nil {
		return
	}
	if math.IsNaN(temperatureOffsetK) || math.IsInf(temperatureOffsetK, 0) {
		err = errInvalidTemperatureOffset(temperatureOffsetK, 0)
		return
	}

	if h <= TropopauseAltitude {
		stdT = TroposphericTemperature(h)
		p = TroposphericPressure(h)
	} else {
		stdT = StratosphericTemperature(h)
		p = StratosphericPressure(h)
	}

	t = stdT + temperatureOffsetK
	if t <= 0 {
		err = errInvalidTemperatureOffset(temperatureOffsetK, t)
		return
	}

	// rho = p / (R*T): standard pressure profile, operative temperature.
	rho = p / (SpecificGasConstant * t)
	return
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
