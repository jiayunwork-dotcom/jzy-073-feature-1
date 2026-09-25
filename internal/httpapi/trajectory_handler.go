package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"isa-service/internal/atmosphere"
)

// trajectoryRequest is the POST body of the trajectory endpoint. It mirrors
// atmosphere.TrajectoryInput; a separate binding type lets the HTTP layer
// produce its own INVALID_PARAMETER diagnostics for malformed JSON without
// coupling the model package to transport concerns.
type trajectoryRequest struct {
	TemperatureOffsetK *float64                  `json:"temperature_offset_k"`
	Segments           []atmosphere.SegmentInput `json:"segments"`
}

// maxTrajectoryBodyBytes bounds a single request payload. The 100000-leg cap
// in the model corresponds to roughly 10 MB of JSON; 16 MiB leaves headroom.
const maxTrajectoryBodyBytes = 16 << 20

// trajectoryHandler implements
// POST /api/v1/atmosphere/trajectory with a JSON body. It accumulates the
// atmospheric quantities traversed along a complete, validated flight
// trajectory (chains of constant-rate legs) and returns both the master and
// per-leg ledgers. Illegal trajectories are rejected with the same structured
// error envelope as every other endpoint.
func trajectoryHandler(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxTrajectoryBodyBytes)
	raw, readErr := io.ReadAll(c.Request.Body)
	if readErr != nil {
		writeModelError(c, &atmosphere.ModelError{
			Code:    atmosphere.ErrInvalidParameter,
			Message: "could not read request body (max 16 MiB): " + readErr.Error(),
		})
		return
	}
	if len(raw) == 0 {
		writeModelError(c, &atmosphere.ModelError{
			Code:    atmosphere.ErrInvalidTrajectory,
			Message: "request body must be a JSON object with a \"segments\" array",
		})
		return
	}

	var req trajectoryRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeModelError(c, &atmosphere.ModelError{
			Code:    atmosphere.ErrInvalidParameter,
			Message: "request body must be valid JSON: " + err.Error(),
		})
		return
	}

	in := atmosphere.TrajectoryInput{Segments: req.Segments}
	if req.TemperatureOffsetK != nil {
		in.TemperatureOffsetK = *req.TemperatureOffsetK
	}

	result, modelErr := atmosphere.AccumulateFromInput(in)
	if modelErr != nil {
		writeModelError(c, modelErr)
		return
	}
	c.JSON(http.StatusOK, trajectoryResponse{Trajectory: result})
}
