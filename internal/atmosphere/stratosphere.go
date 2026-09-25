package atmosphere

import "math"

// This file holds the isothermal-layer formulas valid for
// TropopauseAltitude < h <= ModelTopAltitude. Temperature is pinned at
// TropopauseTemperature and pressure decays exponentially. The formulas are
// anchored to tropopausePressure/tropopauseDensity (constants.go), which are
// evaluated from the tropospheric formulas, so the junction at 11 km is
// continuous by construction.

// StratosphericTemperature returns the constant temperature of the
// isothermal layer: TropopauseTemperature (216.65 K), independent of h.
func StratosphericTemperature(h float64) float64 {
	return TropopauseTemperature
}

// StratosphericPressure returns the standard pressure in the isothermal
// layer via the barometric formula:
//
//	p(h) = p(11km) * exp( -g*(h-11km) / (R*T_tropopause) )
func StratosphericPressure(h float64) float64 {
	scaleHeight := SpecificGasConstant * TropopauseTemperature / Gravity
	return tropopausePressure * math.Exp(-(h-TropopauseAltitude)/scaleHeight)
}

// StratosphericDensity returns the standard density in the isothermal layer
// via the ideal-gas equation with the constant layer temperature.
func StratosphericDensity(h float64) float64 {
	return StratosphericPressure(h) / (SpecificGasConstant * TropopauseTemperature)
}
