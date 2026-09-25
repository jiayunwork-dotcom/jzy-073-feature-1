package atmosphere

import (
	"math"
	"testing"
)

const (
	tolEq   = 1e-9 // tight relative tolerance for exact identity relations
	tolPhys = 1e-6 // tolerance for physics/table-derived values
)

func approxEq(got, want, relTol float64) bool {
	den := math.Max(1.0, math.Abs(want))
	return math.Abs(got-want)/den < relTol
}

// statesEqual compares every state value; DensityAltitude is a pointer, so
// compare its pointed-to value (both non-nil) or nullness.
func statesEqual(a, b State) bool {
	if a.Altitude != b.Altitude || a.Layer != b.Layer ||
		a.Temperature != b.Temperature || a.StandardTemperature != b.StandardTemperature ||
		a.Pressure != b.Pressure || a.Density != b.Density ||
		a.SpeedOfSound != b.SpeedOfSound {
		return false
	}
	if (a.DensityAltitude == nil) != (b.DensityAltitude == nil) {
		return false
	}
	return a.DensityAltitude == nil || *a.DensityAltitude == *b.DensityAltitude
}

// Rule: at altitude zero, temperature/pressure/density must come back to the
// nailed sea-level reference values exactly (ISA is a pure function with
// zero offset).
func TestSeaLevelReference(t *testing.T) {
	s, err := Compute(0, 0)
	if err != nil {
		t.Fatalf("Compute(0,0) returned error: %v", err)
	}
	if s.Temperature != SeaLevelTemperature {
		t.Errorf("sea-level temperature = %.12f, want exact %.12f", s.Temperature, SeaLevelTemperature)
	}
	if s.Pressure != SeaLevelPressure {
		t.Errorf("sea-level pressure = %.12f, want exact %.12f", s.Pressure, SeaLevelPressure)
	}
	if !approxEq(s.Density, SeaLevelDensity, tolEq) {
		t.Errorf("sea-level density = %.12f, want %.12f", s.Density, SeaLevelDensity)
	}
	if !approxEq(s.SpeedOfSound, SpeedOfSound(SeaLevelTemperature), tolEq) {
		t.Errorf("sea-level speed of sound = %.9f, want %.9f", s.SpeedOfSound, SpeedOfSound(SeaLevelTemperature))
	}
	if s.Layer != "troposphere" {
		t.Errorf("sea-level layer = %q, want troposphere", s.Layer)
	}
}

// Rule: every kilometre gained in the troposphere lowers temperature by
// exactly 6.5 K, throughout the layer (linear lapse rate).
func TestTroposphericLapseRate(t *testing.T) {
	for h := 0.0; h < TropopauseAltitude; h += 1000.0 {
		sA, _ := Compute(h, 0)
		sB, _ := Compute(h+1000.0, 0)
		drop := sA.Temperature - sB.Temperature
		if !approxEq(drop, 6.5, tolEq) {
			t.Errorf("from %.0f m to %.0f m temperature drop = %.9f K, want 6.5 K",
				h, h+1000, drop)
		}
	}
	// And against the closed-form lapse rate constant directly.
	s, _ := Compute(5500.0, 0)
	want := SeaLevelTemperature - TemperatureLapseRate*5500.0
	if !approxEq(s.Temperature, want, tolEq) {
		t.Errorf("T(5500) = %.12f, want %.12f", s.Temperature, want)
	}
	// The 6.5 K/km slope itself is constant, so the total drop over 11 km
	// is exactly 71.5 K and the tropopause lands on 216.65 K.
	fullDrop := SeaLevelTemperature - TroposphericTemperature(TropopauseAltitude)
	if !approxEq(fullDrop, 71.5, tolEq) {
		t.Errorf("total tropospheric drop = %.12f K, want 71.5 K", fullDrop)
	}
}

// Rule: at 11 km the pressure produced by the tropospheric formula and the
// pressure produced by the isothermal formula must agree to a tiny
// tolerance — the layered junction must be continuous, no jump.
func TestTropopauseJunctionPressureContinuity(t *testing.T) {
	fromTroposphere := TroposphericPressure(TropopauseAltitude)
	fromStratosphere := StratosphericPressure(TropopauseAltitude)
	if math.Abs(fromTroposphere-fromStratosphere) > 1e-9 {
		t.Errorf("pressure jump at 11 km: tropospheric %.9f Pa vs isothermal %.9f Pa, diff %.3e",
			fromTroposphere, fromStratosphere, fromTroposphere-fromStratosphere)
	}
	// Density must be continuous there as well (same physical junction).
	rhoT := TroposphericDensity(TropopauseAltitude)
	rhoS := StratosphericDensity(TropopauseAltitude)
	if math.Abs(rhoT-rhoS) > 1e-12 {
		t.Errorf("density jump at 11 km: %.12g vs %.12g kg/m^3", rhoT, rhoS)
	}
	// Published ISA value at the tropopause: 22632 Pa, ~0.3639 kg/m^3.
	if !approxEq(fromTroposphere, 22632.0, 1e-3) {
		t.Errorf("p(11km) = %.4f Pa, want ~22632 Pa", fromTroposphere)
	}
	if !approxEq(rhoT, 0.3639, 2e-3) {
		t.Errorf("rho(11km) = %.6f kg/m^3, want ~0.3639", rhoT)
	}
}

// Rule: the tropopause temperature computed from the tropospheric formula
// equals the constant temperature used throughout the isothermal layer.
func TestTropopauseTemperatureEqualsStratosphericTemperature(t *testing.T) {
	troposphericTop := TroposphericTemperature(TropopauseAltitude)
	if !approxEq(troposphericTop, TropopauseTemperature, tolEq) {
		t.Errorf("TroposphericTemperature(11km) = %.6f, want TropopauseTemperature %.6f",
			troposphericTop, TropopauseTemperature)
	}
	if !approxEq(TropopauseTemperature, 216.65, tolEq) {
		t.Errorf("TropopauseTemperature = %.6f, want 216.65 K", TropopauseTemperature)
	}
	for _, h := range []float64{11001.0, 15000.0, 19999.0, 20000.0} {
		if !approxEq(StratosphericTemperature(h), TropopauseTemperature, tolEq) {
			t.Errorf("StratosphericTemperature(%.0f) = %.6f, want constant %.6f",
				h, StratosphericTemperature(h), TropopauseTemperature)
		}
	}
}

// Rule: in the isothermal layer, temperature is constant while pressure and
// density keep decreasing monotonically as altitude rises.
func TestStratosphereIsothermalWithDecreasingPressureDensity(t *testing.T) {
	step := 100.0
	prev, _ := Compute(TropopauseAltitude+1.0, 0)
	for h := TropopauseAltitude + step; h <= ModelTopAltitude; h += step {
		cur, err := Compute(h, 0)
		if err != nil {
			t.Fatalf("Compute(%.0f) error: %v", h, err)
		}
		if cur.Temperature != prev.Temperature {
			// Only the 11001..11100 step can legitimately touch the
			// junction; compare numerically anyway to expose any drift.
			if !approxEq(cur.Temperature, prev.Temperature, tolEq) {
				t.Errorf("temperature changed in isothermal layer at %.0f m: %.9f -> %.9f",
					h, prev.Temperature, cur.Temperature)
			}
		}
		if cur.Pressure >= prev.Pressure {
			t.Errorf("pressure not decreasing at %.0f m: %.6f -> %.6f Pa",
				h, prev.Pressure, cur.Pressure)
		}
		if cur.Density >= prev.Density {
			t.Errorf("density not decreasing at %.0f m: %.9f -> %.9f kg/m^3",
				h, prev.Density, cur.Density)
		}
		prev = cur
	}
}

// Rule: pressure and density decrease monotonically in the troposphere too.
func TestTroposphereMonotonicDecrease(t *testing.T) {
	prev, _ := Compute(0, 0)
	for h := 500.0; h <= TropopauseAltitude; h += 500.0 {
		cur, _ := Compute(h, 0)
		if cur.Pressure >= prev.Pressure || cur.Density >= prev.Density {
			t.Errorf("state not decreasing at %.0f m", h)
		}
		prev = cur
	}
}

// Rule: speed of sound depends only on temperature. Two states sharing the
// same operative temperature but different pressures must report the same
// speed of sound — including across a temperature offset.
func TestSpeedOfSoundIndependentOfPressure(t *testing.T) {
	// Two different altitudes forced to the same operative temperature
	// (one via offset): pressures differ ~2x, temperatures identical.
	a, _ := Compute(5000.0, 0)   // T = 255.65 K
	b, _ := Compute(6000.0, 6.5) // 6000 m is 6.5 K colder standard; a +6.5 K offset restores 255.65 K
	if !approxEq(a.Temperature, b.Temperature, 1e-12) {
		t.Fatalf("test setup: temperatures differ %.9f vs %.9f", a.Temperature, b.Temperature)
	}
	if a.Pressure == b.Pressure {
		t.Fatal("test setup: expected different pressures")
	}
	if math.Abs(a.SpeedOfSound-b.SpeedOfSound) > 1e-9 {
		t.Errorf("speed of sound changed with pressure: %.12f vs %.12f m/s",
			a.SpeedOfSound, b.SpeedOfSound)
	}
	// And explicitly: doubling pressure at fixed T cannot move a.
	a2 := SpeedOfSound(a.Temperature)
	if a.SpeedOfSound != a2 {
		t.Errorf("speed of sound = %.12f, want %.12f", a.SpeedOfSound, a2)
	}
	// Warmer air => higher speed of sound; colder => lower, compared with
	// the standard sea-level value at 288.15 K.
	stdSL, _ := Compute(0, 0)
	warm, _ := Compute(0, 10.0)
	cold, _ := Compute(0, -10.0)
	if !(cold.SpeedOfSound < stdSL.SpeedOfSound && stdSL.SpeedOfSound < warm.SpeedOfSound) {
		t.Errorf("speed of sound temperature ordering wrong: cold %.3f std %.3f warm %.3f",
			cold.SpeedOfSound, stdSL.SpeedOfSound, warm.SpeedOfSound)
	}
}

// Rule: DensityAltitude is the exact inverse of the standard density profile.
// With no temperature offset, density altitude must equal geometric altitude.
func TestDensityAltitudeInverseRoundTrip(t *testing.T) {
	heights := []float64{
		0, 1, 100, 1000, 3048, 5000, 8848, 10000, 10999.99, 11000,
		11000.01, 12500, 15000, 18000, 19999.9, 20000,
	}
	for _, h := range heights {
		s, err := Compute(h, 0)
		if err != nil {
			t.Fatalf("Compute(%.2f,0) error: %v", h, err)
		}
		got, invErr := DensityAltitude(s.Density)
		if invErr != nil {
			t.Fatalf("DensityAltitude(rho(%.2f)) error: %v", h, invErr)
		}
		if math.Abs(got-h) > 1e-6 {
			t.Errorf("density altitude round-trip at %.3f m: got %.9f m, abs err %.3e m",
				h, got, got-h)
		}
		if s.DensityAltitude == nil {
			t.Errorf("Compute densityAltitude at %.3f m is null, want %.3f", h, h)
		} else if !approxEq(*s.DensityAltitude, h, 1e-9) {
			t.Errorf("Compute densityAltitude at %.3f m = %.9f m", h, *s.DensityAltitude)
		}
	}
}

// Rule: a temperature offset separates the standard pressure profile from
// the operative temperature. Pressure is unchanged by the offset; density is
// recomputed with the shifted temperature; density altitude then deviates
// from geometric altitude (that is the meaning of density altitude).
func TestTemperatureOffsetSemantics(t *testing.T) {
	for _, h := range []float64{0, 3000, 7500, 11000, 14000, 20000} {
		std, _ := Compute(h, 0)
		warm, _ := Compute(h, 15.0)
		cold, _ := Compute(h, -15.0)

		// Standard pressure profile is untouched by the offset.
		if warm.Pressure != std.Pressure || cold.Pressure != std.Pressure {
			t.Errorf("pressure changed with temperature offset at %.0f m", h)
		}
		// Operative temperature moves by exactly the offset.
		if warm.Temperature-std.Temperature != 15.0 || std.Temperature-cold.Temperature != 15.0 {
			t.Errorf("temperature offset not applied additively at %.0f m", h)
		}
		// Standard temperature field is still reported alongside.
		if warm.StandardTemperature != std.Temperature {
			t.Errorf("standard temperature altered by offset at %.0f m", h)
		}
		// Warm air is less dense, cold air denser.
		if !(warm.Density < std.Density && std.Density < cold.Density) {
			t.Errorf("density offset ordering wrong at %.0f m: warm %.6f std %.6f cold %.6f",
				h, warm.Density, std.Density, cold.Density)
		}
		// Density follows the ideal-gas law with standard pressure.
		wantWarmRho := std.Pressure / (SpecificGasConstant * (std.Temperature + 15.0))
		if !approxEq(warm.Density, wantWarmRho, 1e-12) {
			t.Errorf("warm density %.12f, want p/(R*T) %.12f at %.0f m",
				warm.Density, wantWarmRho, h)
		}
		// Warmer-than-standard day => density altitude above geometric
		// altitude; colder day => below it (when the equivalent altitude
		// stays inside the 0..20 km model domain; otherwise null).
		if h > 0 && warm.DensityAltitude != nil && *warm.DensityAltitude <= h {
			t.Errorf("+15 K density altitude %.3f not above geometric %.3f m",
				*warm.DensityAltitude, h)
		}
		if h < ModelTopAltitude && cold.DensityAltitude != nil &&
			*cold.DensityAltitude >= h && h-*cold.DensityAltitude > 1e-6 {
			t.Errorf("-15 K density altitude %.3f not below geometric %.3f m",
				*cold.DensityAltitude, h)
		}
	}
}

// Rule: zero offset must reproduce the pure standard atmosphere exactly —
// no drift introduced by the offset code path.
func TestZeroOffsetEqualsPureStandard(t *testing.T) {
	for _, h := range []float64{0, 2500, 9000, 11000, 17000, 20000} {
		explicit, _ := Compute(h, 0)
		implicit, _ := Compute(h, 0.0)
		if !statesEqual(explicit, implicit) {
			t.Errorf("Compute(%.0f, 0) unstable: %+v vs %+v", h, explicit, implicit)
		}
		var stdT, p float64
		if h <= TropopauseAltitude {
			stdT, p = TroposphericTemperature(h), TroposphericPressure(h)
		} else {
			stdT, p = StratosphericTemperature(h), StratosphericPressure(h)
		}
		if explicit.Temperature != stdT || explicit.Pressure != p {
			t.Errorf("offset-0 result deviates from standard formulas at %.0f m", h)
		}
		if !approxEq(explicit.Density, p/(SpecificGasConstant*stdT), 1e-12) {
			t.Errorf("offset-0 density deviates from ideal-gas value at %.0f m", h)
		}
	}
}

// Rule: negative altitude and altitude above 20 km are illegal — structured
// errors, never silent extrapolation.
func TestInvalidAltitudesRejected(t *testing.T) {
	for _, h := range []float64{-0.001, -1, -1000, -11000} {
		if _, err := Compute(h, 0); err == nil || err.Code != ErrAltitudeBelowSeaLevel {
			t.Errorf("Compute(%.3f): expected ErrAltitudeBelowSeaLevel, got %v", h, err)
		}
	}
	for _, h := range []float64{20000.001, 21000, 50000} {
		if _, err := Compute(h, 0); err == nil || err.Code != ErrAltitudeAboveCeiling {
			t.Errorf("Compute(%.3f): expected ErrAltitudeAboveCeiling, got %v", h, err)
		}
	}
	// The boundaries themselves are legal.
	if _, err := Compute(0, 0); err != nil {
		t.Errorf("Compute(0) rejected: %v", err)
	}
	if _, err := Compute(ModelTopAltitude, 0); err != nil {
		t.Errorf("Compute(20000) rejected: %v", err)
	}
	// Density inversion must not extrapolate outside the standard domain either.
	_, err := DensityAltitude(SeaLevelDensity + 0.1)
	if err == nil || err.Code != ErrDensityOutOfDomain {
		t.Errorf("DensityAltitude above rho0: expected domain error, got %v", err)
	}
	_, err = DensityAltitude(modelTopDensity / 2)
	if err == nil || err.Code != ErrDensityOutOfDomain {
		t.Errorf("DensityAltitude below rho(20km): expected domain error, got %v", err)
	}
}

// Offset that would force non-positive absolute temperature is rejected.
func TestInvalidTemperatureOffsetRejected(t *testing.T) {
	if _, err := Compute(0, -SeaLevelTemperature); err == nil ||
		err.Code != ErrInvalidTemperatureOffset {
		t.Errorf("T<=0 offset accepted: %v", err)
	}
}

// Built-in demonstration case: the common cruise altitude just past 10 km.
// Standard T = 223.15 K (-50.00 °C), p ≈ 26436 Pa — fixed numbers anyone
// can check against an ISA table.
func TestDemoCruiseAltitude10km(t *testing.T) {
	s, err := Compute(10000, 0)
	if err != nil {
		t.Fatalf("Compute(10000) error: %v", err)
	}
	if !approxEq(s.Temperature, 223.15, tolEq) {
		t.Errorf("T(10km) = %.6f K, want 223.15 K (-50.00 °C)", s.Temperature)
	}
	if !approxEq(s.Pressure, 26436.3, 1e-3) {
		t.Errorf("p(10km) = %.4f Pa, want ~26436.3 Pa", s.Pressure)
	}
	if !approxEq(s.Density, 0.4127, 2e-3) {
		t.Errorf("rho(10km) = %.6f kg/m^3, want ~0.4127", s.Density)
	}
	if !approxEq(s.SpeedOfSound, 299.5, 2e-3) {
		t.Errorf("a(10km) = %.4f m/s, want ~299.5", s.SpeedOfSound)
	}
}

// 20 km ceiling cross-check against the tabulated ISA value (~5475 Pa).
func TestModelTopValue(t *testing.T) {
	s, err := Compute(20000, 0)
	if err != nil {
		t.Fatalf("Compute(20000) error: %v", err)
	}
	if s.Temperature != 216.65 {
		t.Errorf("T(20km) = %.4f, want 216.65 K", s.Temperature)
	}
	if !approxEq(s.Pressure, 5474.9, 1e-3) {
		t.Errorf("p(20km) = %.4f Pa, want ~5474.9 Pa", s.Pressure)
	}
}

// A warm offset at the model ceiling can produce density lower than the
// standard 20 km density; such a density has no equivalent altitude in the
// implemented domain, so density altitude must be reported as null rather
// than an extrapolated number.
func TestOutOfDomainDensityAltitudeIsNull(t *testing.T) {
	// rho(20km, +15K) < standard rho(20km) => above-ceiling equivalent.
	s, err := Compute(ModelTopAltitude, 15.0)
	if err != nil {
		t.Fatalf("Compute(20000,15) error: %v", err)
	}
	if s.DensityAltitude != nil {
		t.Errorf("density altitude = %.3f, want null (rho %.6f below rho(20km) %.6f)",
			*s.DensityAltitude, s.Density, modelTopDensity)
	}
	// Pure standard at the ceiling still resolves exactly.
	s0, _ := Compute(ModelTopAltitude, 0)
	if s0.DensityAltitude == nil || *s0.DensityAltitude != ModelTopAltitude {
		t.Errorf("standard density altitude at ceiling = %v, want 20000", s0.DensityAltitude)
	}
}
