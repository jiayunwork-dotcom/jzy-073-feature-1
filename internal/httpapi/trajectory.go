package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"isa-service/internal/atmosphere"
)

// This file implements POST /api/v1/atmosphere/trajectory/accumulate.
//
// The body is a JSON object with a "segments" array. Each segment carries
// "start_time_s" plus either "end_time_s" or "duration_s" (equivalent
// forms), and the endpoint altitudes "start_altitude_m"/"end_altitude_m".
// Pointers are used so a missing field is distinguishable from an explicit
// zero. All domain rules (continuity, altitude range, positive duration)
// live in the atmosphere package; here only the JSON shape is checked.

type trajectoryRequest struct {
	Segments []segmentRequest `json:"segments"`
}

type segmentRequest struct {
	StartTimeS     *float64 `json:"start_time_s"`
	EndTimeS       *float64 `json:"end_time_s"`
	DurationS      *float64 `json:"duration_s"`
	StartAltitudeM *float64 `json:"start_altitude_m"`
	EndAltitudeM   *float64 `json:"end_altitude_m"`
}

// trajectoryAccumulateHandler implements
// POST /api/v1/atmosphere/trajectory/accumulate. It validates the request
// body, accumulates the standard atmosphere along the trajectory and
// returns the total and per-segment ledgers.
func trajectoryAccumulateHandler(c *gin.Context) {
	var req trajectoryRequest
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeModelError(c, &atmosphere.ModelError{
			Code:    atmosphere.ErrInvalidParameter,
			Message: "request body must be a single JSON object {\"segments\": [...]}: " + err.Error(),
		})
		return
	}
	if dec.More() {
		writeModelError(c, &atmosphere.ModelError{
			Code:    atmosphere.ErrInvalidParameter,
			Message: "request body must contain exactly one JSON object; trailing data found",
		})
		return
	}
	if req.Segments == nil {
		writeModelError(c, &atmosphere.ModelError{
			Code:    atmosphere.ErrInvalidParameter,
			Message: "missing required field \"segments\" (array of trajectory segments)",
		})
		return
	}

	segments := make([]atmosphere.Segment, 0, len(req.Segments))
	for i := range req.Segments {
		seg, modelErr := req.Segments[i].toSegment(i)
		if modelErr != nil {
			writeModelError(c, modelErr)
			return
		}
		segments = append(segments, seg)
	}

	acc, modelErr := atmosphere.AccumulateTrajectory(segments)
	if modelErr != nil {
		writeModelError(c, modelErr)
		return
	}
	c.JSON(http.StatusOK, acc)
}

// toSegment converts one JSON segment into the domain type, checking that
// the required fields are present and the end of the segment is given
// exactly once (as an end time or as a duration).
func (r *segmentRequest) toSegment(i int) (atmosphere.Segment, *atmosphere.ModelError) {
	missing := func(name string) *atmosphere.ModelError {
		return &atmosphere.ModelError{
			Code:    atmosphere.ErrInvalidParameter,
			Message: fmt.Sprintf("segments[%d]: missing required field %q", i, name),
		}
	}
	if r.StartTimeS == nil {
		return atmosphere.Segment{}, missing("start_time_s")
	}
	if r.StartAltitudeM == nil {
		return atmosphere.Segment{}, missing("start_altitude_m")
	}
	if r.EndAltitudeM == nil {
		return atmosphere.Segment{}, missing("end_altitude_m")
	}
	if r.EndTimeS != nil && r.DurationS != nil {
		return atmosphere.Segment{}, &atmosphere.ModelError{
			Code:    atmosphere.ErrInvalidParameter,
			Message: fmt.Sprintf("segments[%d]: give exactly one of \"end_time_s\" or \"duration_s\", not both", i),
		}
	}
	if r.EndTimeS == nil && r.DurationS == nil {
		return atmosphere.Segment{}, &atmosphere.ModelError{
			Code:    atmosphere.ErrInvalidParameter,
			Message: fmt.Sprintf("segments[%d]: missing end of segment: give \"end_time_s\" or \"duration_s\"", i),
		}
	}

	end := 0.0
	if r.EndTimeS != nil {
		end = *r.EndTimeS
	} else {
		if *r.DurationS <= 0 {
			return atmosphere.Segment{}, &atmosphere.ModelError{
				Code:    atmosphere.ErrInvalidParameter,
				Message: fmt.Sprintf("segments[%d]: \"duration_s\" must be a positive number of seconds, got %v", i, *r.DurationS),
			}
		}
		end = *r.StartTimeS + *r.DurationS
	}
	return atmosphere.Segment{
		StartTime:     *r.StartTimeS,
		EndTime:       end,
		StartAltitude: *r.StartAltitudeM,
		EndAltitude:   *r.EndAltitudeM,
	}, nil
}
