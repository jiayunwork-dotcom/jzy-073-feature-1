// Package httpapi exposes the ISA atmosphere model over HTTP with Gin.
package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"isa-service/internal/atmosphere"
)

// Paths of the service endpoints.
const (
	PointPath                = "/api/v1/atmosphere/point"
	ProfilePath              = "/api/v1/atmosphere/profile"
	TrajectoryAccumulatePath = "/api/v1/atmosphere/trajectory/accumulate"
)

// NewRouter builds the Gin engine with the service endpoints.
// The service deliberately exposes exactly these routes; everything
// else falls through to the structured 404 handler.
func NewRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET(PointPath, pointHandler)
	r.GET(ProfilePath, profileHandler)
	r.POST(TrajectoryAccumulatePath, trajectoryAccumulateHandler)

	r.NoRoute(func(c *gin.Context) {
		writeError(c, http.StatusNotFound, &atmosphere.ModelError{
			Code:    "NOT_FOUND",
			Message: "no such route; use GET " + PointPath + ", GET " + ProfilePath + " or POST " + TrajectoryAccumulatePath,
		})
	})
	return r
}
