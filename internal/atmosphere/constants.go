// Package atmosphere implements the International Standard Atmosphere (ISA)
// model from mean sea level up to the 20 km implementation ceiling:
// the gradient (tropospheric) layer 0..11 km and the isothermal layer
// 11..20 km.
//
// Every physical constant in this file is a nailed ISA reference value.
// They are package-level constants and cannot be overridden by callers;
// the tropospheric, isothermal and density-altitude-inversion formulas all
// share this single source of truth.
package atmosphere

const (
	// Mean sea level reference conditions (geometric altitude 0 m).
	SeaLevelTemperature = 288.15   // K (15 °C)
	SeaLevelPressure    = 101325.0 // Pa
	SeaLevelDensity     = 1.225    // kg/m^3

	// Physical constants.
	Gravity = 9.80665 // m/s^2, standard gravitational acceleration
	// Specific gas constant for dry air. It is derived from the nailed
	// sea-level reference state so that rho0 = p0/(R*T0) holds to floating
	// point. Numerically 287.05287 J/(kg·K): the US Standard Atmosphere
	// (1976) value, commonly tabulated rounded as 287.053.
	SpecificGasConstant = SeaLevelPressure / (SeaLevelDensity * SeaLevelTemperature)
	HeatCapacityRatio   = 1.4 // cp/cv for dry air, used for the speed of sound

	// Layer geometry.
	TropopauseAltitude = 11000.0 // m: top of troposphere / start of isothermal layer
	ModelTopAltitude   = 20000.0 // m: implementation ceiling, never extrapolated past

	// TemperatureLapseRate is the constant tropospheric lapse rate:
	// temperature drops 6.5 K per 1000 m of altitude gain.
	TemperatureLapseRate = 0.0065 // K/m
)

const (
	// TropopauseTemperature is the temperature at the top of the troposphere.
	// It is also the constant temperature held throughout the isothermal
	// layer, so it is the single shared value anchoring both layers.
	TropopauseTemperature = SeaLevelTemperature - TemperatureLapseRate*TropopauseAltitude // 216.65 K

	// MaxProfilePoints bounds the number of points a single profile request
	// can produce.
	MaxProfilePoints = 100_000
)

// Values derived once from the constants above. The isothermal formulas are
// anchored to these so the two layers meet exactly at TropopauseAltitude
// instead of carrying two independently maintained junction values.
var (
	tropopausePressure = TroposphericPressure(TropopauseAltitude)
	tropopauseDensity  = TroposphericDensity(TropopauseAltitude)
	modelTopDensity    = StratosphericDensity(ModelTopAltitude)
)
