package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"isa-service/internal/atmosphere"
)

// pointHandler implements GET /api/v1/atmosphere/point?altitude=<m>[&temperature_offset=<K>].
// It returns the full state at one altitude.
func pointHandler(c *gin.Context) {
	h, err := parseAltitude(c)
	if err != nil {
		writeModelError(c, err)
		return
	}
	offset, err := parseTemperatureOffset(c)
	if err != nil {
		writeModelError(c, err)
		return
	}

	state, modelErr := atmosphere.Compute(h, offset)
	if modelErr != nil {
		writeModelError(c, modelErr)
		return
	}
	c.JSON(http.StatusOK, pointResponse{State: state})
}

// profileHandler implements
// GET /api/v1/atmosphere/profile?start=<m>&end=<m>&step=<m>[&temperature_offset=<K>].
// It returns the complete state at every grid point, including both endpoints.
func profileHandler(c *gin.Context) {
	start, err := parseRequiredFloat(c, "start")
	if err != nil {
		writeModelError(c, err)
		return
	}
	end, err := parseRequiredFloat(c, "end")
	if err != nil {
		writeModelError(c, err)
		return
	}
	step, err := parseRequiredFloat(c, "step")
	if err != nil {
		writeModelError(c, err)
		return
	}
	offset, err := parseTemperatureOffset(c)
	if err != nil {
		writeModelError(c, err)
		return
	}

	states, modelErr := atmosphere.Profile(start, end, step, offset)
	if modelErr != nil {
		writeModelError(c, modelErr)
		return
	}
	c.JSON(http.StatusOK, profileResponse{
		StartM:             start,
		EndM:               end,
		StepM:              step,
		TemperatureOffsetK: offset,
		Count:              len(states),
		Points:             states,
	})
}

func parseAltitude(c *gin.Context) (float64, *atmosphere.ModelError) {
	return parseRequiredFloat(c, "altitude")
}

func parseRequiredFloat(c *gin.Context, name string) (float64, *atmosphere.ModelError) {
	raw := c.Query(name)
	if raw == "" {
		return 0, &atmosphere.ModelError{
			Code:    atmosphere.ErrInvalidParameter,
			Message: "missing required query parameter \"" + name + "\" (metres)",
		}
	}
	v, parseErr := strconv.ParseFloat(raw, 64)
	if parseErr != nil {
		return 0, &atmosphere.ModelError{
			Code:    atmosphere.ErrInvalidParameter,
			Message: "query parameter \"" + name + "\" must be a number, got \"" + raw + "\"",
		}
	}
	return v, nil
}

// parseTemperatureOffset reads the optional temperature offset in kelvin;
// it defaults to zero (pure ISA).
func parseTemperatureOffset(c *gin.Context) (float64, *atmosphere.ModelError) {
	raw := c.Query("temperature_offset")
	if raw == "" {
		return 0, nil
	}
	v, parseErr := strconv.ParseFloat(raw, 64)
	if parseErr != nil {
		return 0, &atmosphere.ModelError{
			Code:    atmosphere.ErrInvalidParameter,
			Message: "query parameter \"temperature_offset\" must be a number of kelvin, got \"" + raw + "\"",
		}
	}
	return v, nil
}
