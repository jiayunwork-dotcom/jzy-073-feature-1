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
	// ErrInvalidTrajectory means a flight-trajectory request was malformed:
	// missing/duplicate fields, non-positive durations, broken continuity
	// between adjacent legs, too many legs, etc.
	ErrInvalidTrajectory ErrorCode = "INVALID_TRAJECTORY"
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

// errTrajectoryf builds a trajectory-level validation error. Leg indices are
// 1-based in messages (the caller's ordering), so callers pass i+1.
func errTrajectoryf(format string, args ...any) *ModelError {
	return &ModelError{Code: ErrInvalidTrajectory, Message: fmt.Sprintf(format, args...)}
}

// errTrajectoryAltitude annotates an out-of-domain altitude ModelError with
// the leg (and endpoint) it came from; the original domain code is kept.
func errTrajectoryAltitude(legIndex int, endpoint string, wrapped *ModelError) *ModelError {
	return &ModelError{
		Code:    wrapped.Code,
		Message: fmt.Sprintf("leg %d %s altitude rejected: %s", legIndex, endpoint, wrapped.Message),
	}
}
