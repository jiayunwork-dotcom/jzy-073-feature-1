package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"isa-service/internal/atmosphere"
)

func postTrajectory(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, TrajectoryPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	setup().ServeHTTP(w, req)
	return w
}

func ptr2(v float64) *float64 { return &v }

func TestTrajectoryEndpointFullMission(t *testing.T) {
	// Climb to cruise, level off, descend; the climb crosses 11 km.
	body := `{
	  "temperature_offset_k": 0,
	  "segments": [
	    {"start_time_s": 0,    "end_time_s": 1100, "start_altitude_m": 0,     "end_altitude_m": 11000},
	    {"start_time_s": 1100, "duration_s": 2000, "start_altitude_m": 11000, "end_altitude_m": 11000},
	    {"start_time_s": 3100, "end_time_s": 4200, "start_altitude_m": 11000, "end_altitude_m": 0}
	  ]
	}`
	w := postTrajectory(t, body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Trajectory struct {
			StartTimeS           float64 `json:"start_time_s"`
			EndTimeS             float64 `json:"end_time_s"`
			DurationS            float64 `json:"duration_s"`
			LegCount             int     `json:"leg_count"`
			TraversedAirMassKGM2 float64 `json:"traversed_air_mass_kg_m2"`
			MeanPressurePa       float64 `json:"mean_pressure_pa"`
			MeanDensityKGM3      float64 `json:"mean_density_kg_m3"`
			MeanTemperatureK     float64 `json:"mean_temperature_k"`
			Segments             []struct {
				Index                int     `json:"index"`
				CrossesTropopause    bool    `json:"crosses_tropopause"`
				TraversedAirMassKGM2 float64 `json:"traversed_air_mass_kg_m2"`
				StartState           struct {
					PressurePa float64 `json:"pressure_pa"`
				} `json:"start_state"`
			} `json:"segments"`
		} `json:"trajectory"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, w.Body.String())
	}
	tr := resp.Trajectory
	if tr.LegCount != 3 || tr.StartTimeS != 0 || tr.EndTimeS != 4200 || tr.DurationS != 4200 {
		t.Errorf("master timing wrong: %+v", tr)
	}
	if tr.Segments[0].CrossesTropopause {
		t.Errorf("0..11000 leg ends exactly at the junction, must not be split")
	}
	// Level cruise leg traverses exactly zero column.
	if tr.Segments[1].TraversedAirMassKGM2 != 0 {
		t.Errorf("level cruise leg mass = %.6e, want 0", tr.Segments[1].TraversedAirMassKGM2)
	}
	// Climb and descent traverse the same layer, equal column mass.
	if !floatNear(tr.Segments[0].TraversedAirMassKGM2, tr.Segments[2].TraversedAirMassKGM2, 1e-12) {
		t.Errorf("climb mass %.9f != descent mass %.9f",
			tr.Segments[0].TraversedAirMassKGM2, tr.Segments[2].TraversedAirMassKGM2)
	}
	// Master mass is the sum of legs (the level one adds nothing).
	wantTotal := tr.Segments[0].TraversedAirMassKGM2 + tr.Segments[2].TraversedAirMassKGM2
	if !floatNear(tr.TraversedAirMassKGM2, wantTotal, 1e-12) {
		t.Errorf("total mass %.9f != leg sum %.9f", tr.TraversedAirMassKGM2, wantTotal)
	}
	// Time-mean density must lie within the standard atmosphere's range.
	if tr.MeanDensityKGM3 <= 0 || tr.MeanDensityKGM3 > atmosphere.SeaLevelDensity {
		t.Errorf("mean density %.6f outside plausible range", tr.MeanDensityKGM3)
	}
	if tr.MeanPressurePa <= 0 || tr.MeanPressurePa > atmosphere.SeaLevelPressure {
		t.Errorf("mean pressure %.3f outside plausible range", tr.MeanPressurePa)
	}
}

func TestTrajectoryEndpointCrossingFlag(t *testing.T) {
	body := `{"segments":[
	  {"start_time_s":0,"end_time_s":100,"start_altitude_m":10500,"end_altitude_m":11500},
	  {"start_time_s":100,"end_time_s":200,"start_altitude_m":11500,"end_altitude_m":10500}
	]}`
	w := postTrajectory(t, body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got := decode(t, w)["trajectory"].(map[string]any)
	segs := got["segments"].([]any)
	for i, s := range segs {
		if !s.(map[string]any)["crosses_tropopause"].(bool) {
			t.Errorf("segment %d should be flagged as crossing the tropopause", i+1)
		}
	}
}

func TestTrajectoryEndpointOffset(t *testing.T) {
	mk := func(offset *float64) *httptest.ResponseRecorder {
		reqBody := map[string]any{
			"segments": []map[string]any{{
				"start_time_s": 0, "end_time_s": 500,
				"start_altitude_m": 0, "end_altitude_m": 10000,
			}},
		}
		if offset != nil {
			reqBody["temperature_offset_k"] = *offset
		}
		raw, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, TrajectoryPath, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		setup().ServeHTTP(w, req)
		return w
	}
	if w := mk(ptr2(0)); w.Code != http.StatusOK {
		t.Fatalf("offset 0 rejected: %s", w.Body.String())
	}
	std := decode(t, mk(nil))["trajectory"].(map[string]any)
	warm := decode(t, mk(ptr2(20)))["trajectory"].(map[string]any)
	cold := decode(t, mk(ptr2(-20)))["trajectory"].(map[string]any)
	if !(warm["traversed_air_mass_kg_m2"].(float64) < std["traversed_air_mass_kg_m2"].(float64) &&
		std["traversed_air_mass_kg_m2"].(float64) < cold["traversed_air_mass_kg_m2"].(float64)) {
		t.Errorf("mass ordering cold > std > warm broken: %v %v %v",
			cold["traversed_air_mass_kg_m2"], std["traversed_air_mass_kg_m2"], warm["traversed_air_mass_kg_m2"])
	}
	// Pressure means must be offset-independent.
	if std["mean_pressure_pa"] != warm["mean_pressure_pa"] {
		t.Errorf("mean pressure changed with temperature offset")
	}
}

func TestTrajectoryEndpointRejectsIllegal(t *testing.T) {
	cases := map[string]string{
		"emptyBody":          ``,
		"notJSON":            `{not json`,
		"noSegments":         `{}`,
		"missingEnd":         `{"segments":[{"start_time_s":0,"start_altitude_m":0,"end_altitude_m":1000}]}`,
		"bothEndAndDuration": `{"segments":[{"start_time_s":0,"end_time_s":1,"duration_s":1,"start_altitude_m":0,"end_altitude_m":0}]}`,
		"zeroDuration":       `{"segments":[{"start_time_s":0,"duration_s":0,"start_altitude_m":0,"end_altitude_m":0}]}`,
		"brokenTime":         `{"segments":[{"start_time_s":0,"end_time_s":100,"start_altitude_m":0,"end_altitude_m":1000},{"start_time_s":101,"end_time_s":200,"start_altitude_m":1000,"end_altitude_m":2000}]}`,
		"brokenAltitude":     `{"segments":[{"start_time_s":0,"end_time_s":100,"start_altitude_m":0,"end_altitude_m":1000},{"start_time_s":100,"end_time_s":200,"start_altitude_m":1100,"end_altitude_m":2000}]}`,
		"belowSeaLevel":      `{"segments":[{"start_time_s":0,"end_time_s":100,"start_altitude_m":-5,"end_altitude_m":1000}]}`,
		"aboveCeiling":       `{"segments":[{"start_time_s":0,"end_time_s":100,"start_altitude_m":0,"end_altitude_m":20001}]}`,
		"badOffset":          `{"temperature_offset_k":"warm","segments":[{"start_time_s":0,"end_time_s":1,"start_altitude_m":0,"end_altitude_m":0}]}`,
		"offsetTooCold":      `{"temperature_offset_k":-300,"segments":[{"start_time_s":0,"end_time_s":100,"start_altitude_m":0,"end_altitude_m":10000}]}`,
	}
	wantCode := map[string]string{
		"emptyBody":          "INVALID_TRAJECTORY",
		"notJSON":            "INVALID_PARAMETER",
		"noSegments":         "INVALID_TRAJECTORY",
		"missingEnd":         "INVALID_TRAJECTORY",
		"bothEndAndDuration": "INVALID_TRAJECTORY",
		"zeroDuration":       "INVALID_TRAJECTORY",
		"brokenTime":         "INVALID_TRAJECTORY",
		"brokenAltitude":     "INVALID_TRAJECTORY",
		"belowSeaLevel":      "ALTITUDE_BELOW_SEA_LEVEL",
		"aboveCeiling":       "ALTITUDE_ABOVE_CEILING",
		"badOffset":          "INVALID_PARAMETER",
		"offsetTooCold":      "INVALID_TEMPERATURE_OFFSET",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			w := postTrajectory(t, body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
			}
			e := decode(t, w)["error"].(map[string]any)
			if e["code"] != wantCode[name] {
				t.Errorf("code = %v, want %s", e["code"], wantCode[name])
			}
			if e["message"] == "" {
				t.Errorf("empty error message")
			}
			if !strings.Contains(w.Header().Get("Content-Type"), "json") {
				t.Errorf("Content-Type = %q", w.Header().Get("Content-Type"))
			}
		})
	}
}

// Broken-chain errors must name the two legs involved so the caller can find
// the gap without re-deriving it.
func TestTrajectoryBrokenChainMessage(t *testing.T) {
	body := `{"segments":[
	  {"start_time_s":0,"end_time_s":100,"start_altitude_m":0,"end_altitude_m":1000},
	  {"start_time_s":100,"end_time_s":200,"start_altitude_m":2000,"end_altitude_m":3000}
	]}`
	w := postTrajectory(t, body)
	e := decode(t, w)["error"].(map[string]any)
	msg := e["message"].(string)
	if !strings.Contains(msg, "leg 2") || !strings.Contains(msg, "leg 1") {
		t.Errorf("message %q does not name legs 1 and 2", msg)
	}
}

// GET must not be accepted on the POST-only trajectory route.
func TestTrajectoryMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, TrajectoryPath, nil)
	w := httptest.NewRecorder()
	setup().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d", w.Code)
	}
}

// Sanity: a large payload (thousands of legs) is answered promptly.
func TestTrajectoryLargePayload(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString(`{"segments":[`)
	const n = 3000
	for i := 0; i < n; i++ {
		if i > 0 {
			buf.WriteByte(',')
		}
		h0, h1 := 0, 500
		if i%2 == 1 {
			h0, h1 = 500, 0
		}
		buf.WriteString(`{"start_time_s":`)
		buf.WriteString(itoa(i))
		buf.WriteString(`,"end_time_s":`)
		buf.WriteString(itoa(i + 1))
		buf.WriteString(`,"start_altitude_m":`)
		buf.WriteString(itoa(h0))
		buf.WriteString(`,"end_altitude_m":`)
		buf.WriteString(itoa(h1))
		buf.WriteByte('}')
	}
	buf.WriteString(`]}`)

	req := httptest.NewRequest(http.MethodPost, TrajectoryPath, &buf)
	w := httptest.NewRecorder()
	setup().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %.300s", w.Code, w.Body.String())
	}
	if got := decode(t, w)["trajectory"].(map[string]any)["leg_count"].(float64); int(got) != n {
		t.Errorf("leg_count = %v, want %d", got, n)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
