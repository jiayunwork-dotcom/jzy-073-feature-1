package atmosphere

import "math"

// This file inverts the standard-atmosphere profile: given a density that
// could appear in the standard model, it returns the equivalent geometric
// altitude. The inversions are exact analytic inverses of the tropospheric
// and isothermal density formulas — they share the same constants and the
// same junction values, so DensityAltitude is a true inverse of the standard
// density profile over the implemented domain.
//
// Domain: modelTopDensity <= rho <= SeaLevelDensity (i.e. 0..20 km).
// A density outside that interval has no equivalent altitude in the
// implemented model and is rejected rather than extrapolated.

// DensityAltitude returns the standard-atmosphere altitude whose standard
// density equals rho. With zero temperature offset, Compute(h) followed by
// DensityAltitude(state.Density) recovers h.
func DensityAltitude(rho float64) (float64, *ModelError) {
	if math.IsNaN(rho) || math.IsInf(rho, 0) {
		return 0, &ModelError{
			Code:    ErrDensityOutOfDomain,
			Message: "density must be a finite number",
		}
	}
	// The bounds are evaluated through the same formulas below, but callers
	// hand back densities computed along slightly different arithmetic chains
	// (e.g. with a temperature offset), so a floating-point epsilon is used
	// rather than exact inequality — without accepting genuinely out-of-domain
	// values.
	const relEpsilon = 1e-12
	lo := modelTopDensity * (1 - relEpsilon)
	hi := SeaLevelDensity * (1 + relEpsilon)
	if rho < lo || rho > hi {
		return 0, errDensityOutOfDomain(rho)
	}

	// Tropopause point: density == rho(11 km). "Above" the junction (lower
	// density) uses the isothermal inversion; the junction itself and the
	// denser air below use the tropospheric inversion. Both branches agree
	// at the junction anyway.
	if rho < tropopauseDensity {
		return invertStratosphericDensity(rho), nil
	}
	return invertTroposphericDensity(rho), nil
}

// invertTroposphericDensity inverts
//
//	rho(h) = rho0 * (1 - lambda*h/T0)^( g/(R*lambda) - 1 )
//
// analytically:
//
//	h = T0/lambda * ( 1 - (rho/rho0)^( 1/(g/(R*lambda) - 1) ) )
func invertTroposphericDensity(rho float64) float64 {
	pressureExponent := Gravity / (SpecificGasConstant * TemperatureLapseRate)
	densityExponent := pressureExponent - 1.0 // g/(R*lambda) - 1
	return SeaLevelTemperature / TemperatureLapseRate *
		(1.0 - math.Pow(rho/SeaLevelDensity, 1.0/densityExponent))
}

// invertStratosphericDensity inverts
//
//	rho(h) = rho(11km) * exp( -g*(h-11km)/(R*T_tropopause) )
//
// analytically:
//
//	h = 11km - (R*T_tropopause/g) * ln( rho / rho(11km) )
func invertStratosphericDensity(rho float64) float64 {
	scaleHeight := SpecificGasConstant * TropopauseTemperature / Gravity
	return TropopauseAltitude - scaleHeight*math.Log(rho/tropopauseDensity)
}
