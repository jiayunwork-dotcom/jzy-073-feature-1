package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"isa-service/internal/atmosphere"
)

// pointResponse wraps a single evaluated state.
type pointResponse struct {
	State atmosphere.State `json:"state"`
}

// profileResponse wraps an evaluated altitude profile.
type profileResponse struct {
	StartM             float64            `json:"start_m"`
	EndM               float64            `json:"end_m"`
	StepM              float64            `json:"step_m"`
	TemperatureOffsetK float64            `json:"temperature_offset_k"`
	Count              int                `json:"count"`
	Points             []atmosphere.State `json:"points"`
}

// errorResponse is the single structured error envelope.
type errorResponse struct {
	Error atmosphere.ModelError `json:"error"`
}

func writeModelError(c *gin.Context, e *atmosphere.ModelError) {
	writeError(c, statusForCode(e.Code), e)
}

func writeError(c *gin.Context, status int, e *atmosphere.ModelError) {
	c.AbortWithStatusJSON(status, errorResponse{Error: *e})
}

// statusForCode maps every domain error code onto an HTTP status.
func statusForCode(code atmosphere.ErrorCode) int {
	switch code {
	case atmosphere.ErrAltitudeBelowSeaLevel,
		atmosphere.ErrAltitudeAboveCeiling,
		atmosphere.ErrInvalidAltitude,
		atmosphere.ErrInvalidRange,
		atmosphere.ErrInvalidStep,
		atmosphere.ErrInvalidTemperatureOffset,
		atmosphere.ErrDensityOutOfDomain,
		atmosphere.ErrInvalidParameter:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
