package atmosphere

import "testing"

func TestProfileGridIncludesBothEndpoints(t *testing.T) {
	states, err := Profile(0, 5000, 1000, 0)
	if err != nil {
		t.Fatalf("Profile error: %v", err)
	}
	if len(states) != 6 {
		t.Fatalf("profile length = %d, want 6 (0..5000 step 1000)", len(states))
	}
	for i, s := range states {
		wantH := float64(i) * 1000
		if s.Altitude != wantH {
			t.Errorf("point %d altitude = %.1f, want %.1f", i, s.Altitude, wantH)
		}
	}
	if states[0].Altitude != 0 || states[len(states)-1].Altitude != 5000 {
		t.Errorf("endpoints missing: first %.1f last %.1f",
			states[0].Altitude, states[len(states)-1].Altitude)
	}
}

// When the end does not land on the step grid it must still be appended
// exactly, so the front end never has to interpolate the endpoint itself.
func TestProfileAppendsNonGridEndpoint(t *testing.T) {
	states, err := Profile(0, 3500, 1000, 0)
	if err != nil {
		t.Fatalf("Profile error: %v", err)
	}
	if states[len(states)-1].Altitude != 3500 {
		t.Errorf("last altitude = %.1f, want 3500", states[len(states)-1].Altitude)
	}
	// Grid points 0,1000,2000,3000 plus endpoint 3500.
	if len(states) != 5 {
		t.Errorf("profile length = %d, want 5", len(states))
	}
}

// A profile crossing the 11 km junction must not show a discontinuity.
func TestProfileCrossesJunctionContinuously(t *testing.T) {
	states, err := Profile(10500, 11500, 250, 0)
	if err != nil {
		t.Fatalf("Profile error: %v", err)
	}
	var before, at, after *State
	for i := range states {
		switch states[i].Altitude {
		case 10750:
			before = &states[i]
		case 11000:
			at = &states[i]
		case 11250:
			after = &states[i]
		}
	}
	if before == nil || at == nil || after == nil {
		t.Fatalf("expected 10750/11000/11250 points, got %v", heights(states))
	}
	if at.Layer != "troposphere" {
		t.Errorf("11000 layer = %q, want troposphere (inclusive)", at.Layer)
	}
	if after.Layer != "stratosphere" {
		t.Errorf("11250 layer = %q, want stratosphere", after.Layer)
	}
	// Smooth temperature evolution across the junction: each 250 m step
	// inside/right at the junction drops 1.625 K, then it freezes.
	if !relEq(before.Temperature-at.Temperature, TemperatureLapseRate*250) {
		t.Errorf("junction lapse broken: %.4f -> %.4f", before.Temperature, at.Temperature)
	}
	if !relEq(after.Temperature, at.Temperature) {
		t.Errorf("temperature changed after tropopause: %.4f -> %.4f",
			at.Temperature, after.Temperature)
	}
}

func relEq(a, b float64) bool {
	const eps = 1e-12
	den := b
	if den < 0 {
		den = -den
	}
	if den < 1 {
		den = 1
	}
	d := a - b
	if d < 0 {
		d = -d
	}
	return d/den < eps
}

func TestProfileRejectsBadInput(t *testing.T) {
	cases := []struct {
		name       string
		start, end float64
		step       float64
		want       ErrorCode
	}{
		{"negative start", -1, 1000, 100, ErrAltitudeBelowSeaLevel},
		{"end above ceiling", 0, ModelTopAltitude + 1, 100, ErrAltitudeAboveCeiling},
		{"end before start", 5000, 4000, 100, ErrInvalidRange},
		{"zero step", 0, 1000, 0, ErrInvalidStep},
		{"negative step", 0, 1000, -100, ErrInvalidStep},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Profile(tc.start, tc.end, tc.step, 0)
			if err == nil || err.Code != tc.want {
				t.Errorf("got %v, want code %s", err, tc.want)
			}
		})
	}
}

func TestProfileRespectsOffset(t *testing.T) {
	states, err := Profile(0, 3000, 1000, 15.0)
	if err != nil {
		t.Fatalf("Profile error: %v", err)
	}
	point, _ := Compute(2000, 15.0)
	if !statesEqual(states[2], point) {
		t.Errorf("offset profile point differs from Compute: %+v vs %+v", states[2], point)
	}
}

func heights(states []State) []float64 {
	out := make([]float64, len(states))
	for i, s := range states {
		out[i] = s.Altitude
	}
	return out
}
