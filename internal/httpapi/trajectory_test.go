package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"isa-service/internal/atmosphere"
)

func postTrajectory(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, TrajectoryAccumulatePath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	setup().ServeHTTP(w, req)
	return w
}

func TestTrajectoryAccumulateEndpoint(t *testing.T) {
	body := `{"segments": [
		{"start_time_s": 0, "end_time_s": 600, "start_altitude_m": 0, "end_altitude_m": 11000},
		{"start_time_s": 600, "duration_s": 1800, "start_altitude_m": 11000, "end_altitude_m": 11000},
		{"start_time_s": 2400, "end_time_s": 3000, "start_altitude_m": 11000, "end_altitude_m": 3000}
	]}`
	w := postTrajectory(t, body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	resp := decode(t, w)

	if resp["segment_count"].(float64) != 3 {
		t.Errorf("segment_count = %v, want 3", resp["segment_count"])
	}
	if resp["duration_s"].(float64) != 3000 {
		t.Errorf("duration_s = %v, want 3000", resp["duration_s"])
	}
	if resp["min_altitude_m"].(float64) != 0 || resp["max_altitude_m"].(float64) != 11000 {
		t.Errorf("altitude span = %v..%v", resp["min_altitude_m"], resp["max_altitude_m"])
	}

	total := resp["total"].(map[string]any)
	if total["column_mass_kg_m2"].(float64) <= 0 {
		t.Errorf("total column mass = %v, want positive", total["column_mass_kg_m2"])
	}
	if total["mean_pressure_pa"].(float64) <= 0 || total["mean_density_kg_m3"].(float64) <= 0 {
		t.Errorf("total means must be positive: %v", total)
	}

	segments := resp["segments"].([]any)
	cruise := segments[1].(map[string]any)
	if cruise["column_mass_kg_m2"].(float64) != 0 {
		t.Errorf("cruise leg column mass = %v, want exactly 0", cruise["column_mass_kg_m2"])
	}
	climb := segments[0].(map[string]any)
	if climb["climb_rate_m_s"].(float64) != 11000.0/600.0 {
		t.Errorf("climb rate = %v, want %v", climb["climb_rate_m_s"], 11000.0/600.0)
	}
	if climb["duration_s"].(float64) != 600 {
		t.Errorf("climb duration = %v, want 600", climb["duration_s"])
	}

	// The HTTP result must be the domain result: recompute directly and
	// compare the total ledger.
	direct, err := atmosphere.AccumulateTrajectory([]atmosphere.Segment{
		{StartTime: 0, EndTime: 600, StartAltitude: 0, EndAltitude: 11000},
		{StartTime: 600, EndTime: 2400, StartAltitude: 11000, EndAltitude: 11000},
		{StartTime: 2400, EndTime: 3000, StartAltitude: 11000, EndAltitude: 3000},
	})
	if err != nil {
		t.Fatalf("direct accumulation error: %v", err)
	}
	if !floatNear(total["column_mass_kg_m2"].(float64), direct.Total.ColumnMassKgM2, 1e-12) {
		t.Errorf("HTTP column mass %v != domain %v",
			total["column_mass_kg_m2"], direct.Total.ColumnMassKgM2)
	}
	if !floatNear(total["mean_pressure_pa"].(float64), direct.Total.MeanPressurePa, 1e-12) {
		t.Errorf("HTTP mean pressure %v != domain %v",
			total["mean_pressure_pa"], direct.Total.MeanPressurePa)
	}
}

// The two equivalent segment time forms must give the same answer.
func TestTrajectoryAccumulateDurationFormEqualsEndTimeForm(t *testing.T) {
	endForm := `{"segments": [
		{"start_time_s": 0, "end_time_s": 600, "start_altitude_m": 0, "end_altitude_m": 10000},
		{"start_time_s": 600, "end_time_s": 1200, "start_altitude_m": 10000, "end_altitude_m": 5000}
	]}`
	durationForm := `{"segments": [
		{"start_time_s": 0, "duration_s": 600, "start_altitude_m": 0, "end_altitude_m": 10000},
		{"start_time_s": 600, "duration_s": 600, "start_altitude_m": 10000, "end_altitude_m": 5000}
	]}`
	w1, w2 := postTrajectory(t, endForm), postTrajectory(t, durationForm)
	if w1.Code != http.StatusOK || w2.Code != http.StatusOK {
		t.Fatalf("status = %d / %d", w1.Code, w2.Code)
	}
	t1 := decode(t, w1)["total"].(map[string]any)
	t2 := decode(t, w2)["total"].(map[string]any)
	for _, key := range []string{"column_mass_kg_m2", "mean_pressure_pa", "mean_density_kg_m3"} {
		if t1[key] != t2[key] {
			t.Errorf("%s: end_time form %v != duration form %v", key, t1[key], t2[key])
		}
	}
}

// Rule (end to end): a level leg's means seen through the trajectory
// endpoint are the same numbers the point endpoint returns for that
// altitude — one model, one set of constants, two lenses.
func TestTrajectoryLevelMeansMatchPointEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, PointPath+"?altitude=5000", nil)
	w := httptest.NewRecorder()
	setup().ServeHTTP(w, req)
	point := decode(t, w)["state"].(map[string]any)

	w = postTrajectory(t, `{"segments": [
		{"start_time_s": 0, "end_time_s": 7200, "start_altitude_m": 5000, "end_altitude_m": 5000}
	]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	seg := decode(t, w)["segments"].([]any)[0].(map[string]any)
	if seg["mean_pressure_pa"] != point["pressure_pa"] {
		t.Errorf("level mean pressure %v != point endpoint %v",
			seg["mean_pressure_pa"], point["pressure_pa"])
	}
	if seg["mean_density_kg_m3"] != point["density_kg_m3"] {
		t.Errorf("level mean density %v != point endpoint %v",
			seg["mean_density_kg_m3"], point["density_kg_m3"])
	}
}

func TestTrajectoryAccumulateRejectsIllegalRequests(t *testing.T) {
	cases := map[string]struct {
		body     string
		wantCode string
	}{
		"malformed json":        {`{"segments": [`, string(atmosphere.ErrInvalidParameter)},
		"empty body":            {``, string(atmosphere.ErrInvalidParameter)},
		"trailing data":         {`{"segments": []} {"x": 1}`, string(atmosphere.ErrInvalidParameter)},
		"missing segments":      {`{}`, string(atmosphere.ErrInvalidParameter)},
		"unknown field":         {`{"segments": [{"start_time_s": 0, "end_time_s": 1, "start_altitude_m": 0, "end_altitude_m": 1, "foo": 2}]}`, string(atmosphere.ErrInvalidParameter)},
		"missing altitude":      {`{"segments": [{"start_time_s": 0, "end_time_s": 1, "start_altitude_m": 0}]}`, string(atmosphere.ErrInvalidParameter)},
		"missing start time":    {`{"segments": [{"end_time_s": 1, "start_altitude_m": 0, "end_altitude_m": 1}]}`, string(atmosphere.ErrInvalidParameter)},
		"both end forms":        {`{"segments": [{"start_time_s": 0, "end_time_s": 1, "duration_s": 1, "start_altitude_m": 0, "end_altitude_m": 1}]}`, string(atmosphere.ErrInvalidParameter)},
		"neither end form":      {`{"segments": [{"start_time_s": 0, "start_altitude_m": 0, "end_altitude_m": 1}]}`, string(atmosphere.ErrInvalidParameter)},
		"negative duration":     {`{"segments": [{"start_time_s": 0, "duration_s": -5, "start_altitude_m": 0, "end_altitude_m": 1}]}`, string(atmosphere.ErrInvalidParameter)},
		"empty segment list":    {`{"segments": []}`, string(atmosphere.ErrInvalidTrajectory)},
		"zero length segment":   {`{"segments": [{"start_time_s": 5, "end_time_s": 5, "start_altitude_m": 0, "end_altitude_m": 1}]}`, string(atmosphere.ErrInvalidTrajectory)},
		"altitude gap":          {`{"segments": [{"start_time_s": 0, "end_time_s": 10, "start_altitude_m": 0, "end_altitude_m": 5000}, {"start_time_s": 10, "end_time_s": 20, "start_altitude_m": 4000, "end_altitude_m": 1000}]}`, string(atmosphere.ErrTrajectoryGap)},
		"time gap":              {`{"segments": [{"start_time_s": 0, "end_time_s": 10, "start_altitude_m": 0, "end_altitude_m": 5000}, {"start_time_s": 11, "end_time_s": 20, "start_altitude_m": 5000, "end_altitude_m": 1000}]}`, string(atmosphere.ErrTrajectoryGap)},
		"below sea level":       {`{"segments": [{"start_time_s": 0, "end_time_s": 10, "start_altitude_m": -1, "end_altitude_m": 100}]}`, string(atmosphere.ErrAltitudeBelowSeaLevel)},
		"above ceiling":         {`{"segments": [{"start_time_s": 0, "end_time_s": 10, "start_altitude_m": 0, "end_altitude_m": 20001}]}`, string(atmosphere.ErrAltitudeAboveCeiling)},
		"out of domain mid-way": {`{"segments": [{"start_time_s": 0, "end_time_s": 10, "start_altitude_m": 0, "end_altitude_m": 5000}, {"start_time_s": 10, "end_time_s": 20, "start_altitude_m": 5000, "end_altitude_m": 30000}]}`, string(atmosphere.ErrAltitudeAboveCeiling)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := postTrajectory(t, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
			}
			e := decode(t, w)["error"].(map[string]any)
			if e["code"] != tc.wantCode {
				t.Errorf("error code = %v, want %s (body: %s)", e["code"], tc.wantCode, w.Body.String())
			}
			if e["message"] == "" {
				t.Errorf("error message empty")
			}
		})
	}
}

// A broken junction must say which segment broke, over HTTP too.
func TestTrajectoryGapErrorNamesSegment(t *testing.T) {
	w := postTrajectory(t, `{"segments": [
		{"start_time_s": 0, "end_time_s": 10, "start_altitude_m": 0, "end_altitude_m": 5000},
		{"start_time_s": 10, "end_time_s": 20, "start_altitude_m": 4000, "end_altitude_m": 1000}
	]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	e := decode(t, w)["error"].(map[string]any)
	if !strings.Contains(e["message"].(string), "segments[1]") {
		t.Errorf("gap error must name the broken segment: %v", e["message"])
	}
}

// The endpoint answers POST only; anything else falls through to the
// structured 404 like every other unknown route.
func TestTrajectoryAccumulateWrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, TrajectoryAccumulatePath, nil)
	w := httptest.NewRecorder()
	setup().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET %s: status = %d, want 404", TrajectoryAccumulatePath, w.Code)
	}
	if decode(t, w)["error"].(map[string]any)["code"] != "NOT_FOUND" {
		t.Errorf("body = %s", w.Body.String())
	}
}

// The pre-existing endpoints are untouched by the new capability.
func TestExistingEndpointsUnchangedByTrajectoryFeature(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, PointPath+"?altitude=10000", nil)
	w := httptest.NewRecorder()
	setup().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("point status = %d", w.Code)
	}
	state := decode(t, w)["state"].(map[string]any)
	if !floatNear(state["temperature_k"].(float64), 223.15, 1e-12) {
		t.Errorf("temperature_k = %v, want 223.15", state["temperature_k"])
	}

	req = httptest.NewRequest(http.MethodGet, ProfilePath+"?start=0&end=2000&step=1000", nil)
	w = httptest.NewRecorder()
	setup().ServeHTTP(w, req)
	if w.Code != http.StatusOK || decode(t, w)["count"].(float64) != 3 {
		t.Errorf("profile endpoint changed: status %d body %s", w.Code, w.Body.String())
	}
}

// JSON round-trip sanity: the response is one self-contained document with
// no hidden state — re-posting the same trajectory reproduces it exactly.
func TestTrajectoryAccumulateIsStateless(t *testing.T) {
	body := `{"segments": [{"start_time_s": 0, "end_time_s": 900, "start_altitude_m": 1000, "end_altitude_m": 15000}]}`
	w1, w2 := postTrajectory(t, body), postTrajectory(t, body)
	var a, b map[string]any
	if err := json.Unmarshal(w1.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if w1.Body.String() != w2.Body.String() {
		t.Errorf("same request, different responses:\n%s\n%s", w1.Body.String(), w2.Body.String())
	}
}
