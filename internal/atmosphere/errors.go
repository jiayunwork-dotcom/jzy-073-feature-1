package atmosphere

import "fmt"

// ErrorCode identifies why a request was rejected.
type ErrorCode string

const (
	// ErrInvalidAltitude means the altitude was not a finite number.
	ErrInvalidAltitude ErrorCode = "INVALID_ALTITUDE"
	// ErrAltitudeBelowSeaLevel means the altitude was negative.
	ErrAltitudeBelowSeaLevel ErrorCode = "ALTITUDE_BELOW_SEA_LEVEL"
	// ErrAltitudeAboveCeiling means the altitude exceeded 20 km.
	ErrAltitudeAboveCeiling ErrorCode = "ALTITUDE_ABOVE_CEILING"
	// ErrInvalidRange means the profile interval endpoints were missing,
	// unordered or otherwise invalid.
	ErrInvalidRange ErrorCode = "INVALID_RANGE"
	// ErrInvalidStep means the profile step was non-positive or too small.
	ErrInvalidStep ErrorCode = "INVALID_STEP"
	// ErrInvalidTemperatureOffset means the offset was non-finite or made the
	// absolute temperature non-positive.
	ErrInvalidTemperatureOffset ErrorCode = "INVALID_TEMPERATURE_OFFSET"
	// ErrDensityOutOfDomain means the given density has no equivalent
	// altitude inside the implemented 0..20 km standard model.
	ErrDensityOutOfDomain ErrorCode = "DENSITY_OUT_OF_DOMAIN"
	// ErrInvalidParameter means an HTTP query parameter was malformed.
	ErrInvalidParameter ErrorCode = "INVALID_PARAMETER"
	// ErrInvalidTrajectory means a flight trajectory was structurally
	// illegal: no segments, non-finite times, non-positive segment
	// duration or too many segments.
	ErrInvalidTrajectory ErrorCode = "INVALID_TRAJECTORY"
	// ErrTrajectoryGap means two adjacent segments do not meet: the next
	// segment's start time or start altitude differs from the previous
	// segment's end, so the path is not one continuous trajectory.
	ErrTrajectoryGap ErrorCode = "TRAJECTORY_GAP"
)

// ModelError is the structured error returned for every rejected request.
// The HTTP layer serializes Code and Message verbatim.
type ModelError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func (e *ModelError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func errInvalidAltitude(h float64) *ModelError {
	return &ModelError{
		Code:    ErrInvalidAltitude,
		Message: fmt.Sprintf("altitude must be a finite number, got %v", h),
	}
}

func errBelowSeaLevel(h float64) *ModelError {
	return &ModelError{
		Code: ErrAltitudeBelowSeaLevel,
		Message: fmt.Sprintf("altitude %.3f m is below mean sea level; the model domain is 0..%.0f m",
			h, ModelTopAltitude),
	}
}

func errAboveCeiling(h float64) *ModelError {
	return &ModelError{
		Code: ErrAltitudeAboveCeiling,
		Message: fmt.Sprintf("altitude %.3f m exceeds the %.0f m implementation ceiling; higher atmospheric layers are not modelled or extrapolated",
			h, ModelTopAltitude),
	}
}

func errInvalidTemperatureOffset(deltaT, t float64) *ModelError {
	return &ModelError{
		Code: ErrInvalidTemperatureOffset,
		Message: fmt.Sprintf("temperature offset %.3f K yields non-positive absolute temperature %.3f K",
			deltaT, t),
	}
}

func errDensityOutOfDomain(rho float64) *ModelError {
	return &ModelError{
		Code: ErrDensityOutOfDomain,
		Message: fmt.Sprintf("density %.6g kg/m^3 has no equivalent altitude inside the standard model domain [%.6g, %.6g] kg/m^3 (0..%.0f m)",
			rho, modelTopDensity, SeaLevelDensity, ModelTopAltitude),
	}
}

// errTrajectoryGap reports a broken junction between segments[i-1] and
// segments[i]; "quantity" names what jumps ("start_time_s"/"start_altitude_m").
func errTrajectoryGap(i int, quantity string, prevEnd, nextStart float64) *ModelError {
	return &ModelError{
		Code: ErrTrajectoryGap,
		Message: fmt.Sprintf("segments[%d] does not continue segments[%d]: %s jumps from %v to %v at the junction",
			i, i-1, quantity, prevEnd, nextStart),
	}
}
