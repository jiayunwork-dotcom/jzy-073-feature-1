package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"isa-service/internal/atmosphere"
)

func setup() http.Handler {
	return NewRouter()
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v\nbody: %s", err, w.Body.String())
	}
	return body
}

func TestPointEndpointSeaLevel(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, PointPath+"?altitude=0", nil)
	w := httptest.NewRecorder()
	setup().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := decode(t, w)
	state := body["state"].(map[string]any)
	if state["temperature_k"].(float64) != atmosphere.SeaLevelTemperature {
		t.Errorf("temperature_k = %v", state["temperature_k"])
	}
	if state["pressure_pa"].(float64) != atmosphere.SeaLevelPressure {
		t.Errorf("pressure_pa = %v", state["pressure_pa"])
	}
	if mathRel(state["density_kg_m3"].(float64), atmosphere.SeaLevelDensity) > 1e-9 {
		t.Errorf("density_kg_m3 = %v", state["density_kg_m3"])
	}
}

func TestPointEndpointWithOffset(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, PointPath+"?altitude=5000&temperature_offset=15", nil)
	w := httptest.NewRecorder()
	setup().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	state := decode(t, w)["state"].(map[string]any)
	// 288.15 - 0.0065*5000 = 255.65; +15 => 270.65
	if !floatNear(state["temperature_k"].(float64), 270.65, 1e-12) {
		t.Errorf("temperature_k = %v, want 270.65", state["temperature_k"])
	}
	if !floatNear(state["standard_temperature_k"].(float64), 255.65, 1e-12) {
		t.Errorf("standard_temperature_k = %v, want 255.65", state["standard_temperature_k"])
	}
	da := state["density_altitude_m"].(float64)
	if !(da > 5000) {
		t.Errorf("density altitude %.2f should exceed 5000 on a +15 K day", da)
	}
}

func TestPointEndpointRejectsIllegalAltitude(t *testing.T) {
	cases := map[string]string{
		"negative":      PointPath + "?altitude=-100",
		"aboveCeiling":  PointPath + "?altitude=25000",
		"missing":       PointPath,
		"notANumber":    PointPath + "?altitude=abc",
		"badOffset":     PointPath + "?altitude=0&temperature_offset=xyz",
		"offsetTooCold": PointPath + "?altitude=0&temperature_offset=-300",
	}
	wantCode := map[string]string{
		"negative":      string(atmosphere.ErrAltitudeBelowSeaLevel),
		"aboveCeiling":  string(atmosphere.ErrAltitudeAboveCeiling),
		"missing":       string(atmosphere.ErrInvalidParameter),
		"notANumber":    string(atmosphere.ErrInvalidParameter),
		"badOffset":     string(atmosphere.ErrInvalidParameter),
		"offsetTooCold": string(atmosphere.ErrInvalidTemperatureOffset),
	}
	for name, url := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()
			setup().ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
			}
			body := decode(t, w)
			e := body["error"].(map[string]any)
			if e["code"] != wantCode[name] {
				t.Errorf("error code = %v, want %s", e["code"], wantCode[name])
			}
			if e["message"] == "" {
				t.Errorf("error message empty")
			}
			if !strings.Contains(w.Header().Get("Content-Type"), "json") {
				t.Errorf("Content-Type = %q", w.Header().Get("Content-Type"))
			}
		})
	}
}

func TestProfileEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, ProfilePath+"?start=0&end=3000&step=1000", nil)
	w := httptest.NewRecorder()
	setup().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := decode(t, w)
	if body["count"].(float64) != 4 {
		t.Errorf("count = %v, want 4", body["count"])
	}
	points := body["points"].([]any)
	last := points[len(points)-1].(map[string]any)
	if last["altitude_m"].(float64) != 3000 {
		t.Errorf("last altitude = %v", last["altitude_m"])
	}
}

func TestProfileEndpointValidation(t *testing.T) {
	bad := []string{
		ProfilePath + "?start=0&end=3000", // missing step
		ProfilePath + "?start=0&end=3000&step=0",
		ProfilePath + "?start=-10&end=3000&step=100",
		ProfilePath + "?start=0&end=99999&step=100",
	}
	for _, url := range bad {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		w := httptest.NewRecorder()
		setup().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("url %s: status = %d, want 400", url, w.Code)
		}
	}
}

func TestUnknownRouteStructured404(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/weather", nil)
	w := httptest.NewRecorder()
	setup().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	body := decode(t, w)
	if body["error"].(map[string]any)["code"] != "NOT_FOUND" {
		t.Errorf("body = %s", w.Body.String())
	}
}

func mathRel(got, want float64) float64 {
	return abs(got-want) / want
}

func floatNear(got, want, relTol float64) bool {
	return mathRel(got, want) < relTol
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
