package atmosphere

import "math"

// This file holds the tropospheric (constant-lapse-rate) formulas valid for
// 0 <= h <= TropopauseAltitude. All of them read their constants from
// constants.go; no layer-local copies are kept.

// TroposphericTemperature returns the standard temperature in the
// troposphere. It decreases linearly from 288.15 K at sea level at
// TemperatureLapseRate (6.5 K/km).
func TroposphericTemperature(h float64) float64 {
	return SeaLevelTemperature - TemperatureLapseRate*h
}

// TroposphericPressure returns the standard pressure in the troposphere.
// It is the analytic solution of hydrostatic equilibrium with a constant
// temperature lapse rate:
//
//	p(h) = p0 * (T(h)/T0)^( g / (R*lambda) )
func TroposphericPressure(h float64) float64 {
	tRatio := TroposphericTemperature(h) / SeaLevelTemperature
	exponent := Gravity / (SpecificGasConstant * TemperatureLapseRate)
	return SeaLevelPressure * math.Pow(tRatio, exponent)
}

// TroposphericDensity returns the standard density in the troposphere,
// derived from temperature and pressure through the ideal-gas equation
// p = rho*R*T.
func TroposphericDensity(h float64) float64 {
	return TroposphericPressure(h) / (SpecificGasConstant * TroposphericTemperature(h))
}
