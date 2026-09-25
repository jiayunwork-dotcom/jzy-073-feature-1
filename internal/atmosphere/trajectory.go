package atmosphere

import (
	"fmt"
	"math"
)

// This file describes a flight trajectory and what makes one legal.
//
// A trajectory is a time-ordered chain of segments. Within one segment the
// geometric altitude varies linearly with time — a constant climb or
// descent rate, or level flight when both endpoint altitudes are equal.
// Adjacent segments must meet: the next segment starts exactly (within a
// small floating-point tolerance) where and when the previous one ended.
// Every altitude on the path must stay inside the model domain; the service
// never extrapolates the atmosphere to make an integral computable.

// Segment is one leg of a flight trajectory. Between StartTime and EndTime
// (seconds, any consistent epoch) the geometric altitude varies linearly
// from StartAltitude to EndAltitude (metres).
type Segment struct {
	StartTime     float64
	EndTime       float64
	StartAltitude float64
	EndAltitude   float64
}

// Duration is the segment's length in seconds; it is always positive for a
// validated segment.
func (s Segment) Duration() float64 {
	return s.EndTime - s.StartTime
}

// ClimbRate is the signed vertical speed in m/s: positive when climbing,
// negative when descending, exactly zero in level flight.
func (s Segment) ClimbRate() float64 {
	return (s.EndAltitude - s.StartAltitude) / s.Duration()
}

// gapRelTol is the relative tolerance for the junction continuity check.
// Callers build segments from JSON numbers and a strict bit-equality demand
// would reject genuinely continuous paths over representation noise; the
// tolerance is far too small to ever absorb a real break.
const gapRelTol = 1e-9

// MaxTrajectorySegments bounds the number of segments a single accumulation
// request may carry, mirroring MaxProfilePoints on the profile endpoint.
const MaxTrajectorySegments = 100_000

// ValidateTrajectory checks that segments form one continuous, legal flight
// path: at least one segment, positive duration per segment, all altitudes
// inside the model domain, and adjacent segments meeting in both time and
// altitude. It returns nil for a legal trajectory, otherwise the first
// problem found, naming the offending segment.
func ValidateTrajectory(segments []Segment) *ModelError {
	if len(segments) == 0 {
		return &ModelError{
			Code:    ErrInvalidTrajectory,
			Message: "trajectory must contain at least one segment",
		}
	}
	if len(segments) > MaxTrajectorySegments {
		return &ModelError{
			Code: ErrInvalidTrajectory,
			Message: fmt.Sprintf("trajectory has %d segments; the limit is %d",
				len(segments), MaxTrajectorySegments),
		}
	}
	for i := range segments {
		if err := validateSegment(i, segments[i]); err != nil {
			return err
		}
		if i > 0 {
			if err := checkJunction(i, segments[i-1], segments[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateSegment checks one segment in isolation: finite times, strictly
// positive duration, and both endpoint altitudes inside the model domain.
// Because altitude is linear within a segment, checking the endpoints bounds
// the whole segment — no interior point can leave the domain.
func validateSegment(i int, s Segment) *ModelError {
	if math.IsNaN(s.StartTime) || math.IsInf(s.StartTime, 0) ||
		math.IsNaN(s.EndTime) || math.IsInf(s.EndTime, 0) {
		return &ModelError{
			Code:    ErrInvalidTrajectory,
			Message: fmt.Sprintf("segments[%d]: start and end times must be finite numbers", i),
		}
	}
	if !(s.EndTime > s.StartTime) {
		return &ModelError{
			Code: ErrInvalidTrajectory,
			Message: fmt.Sprintf("segments[%d]: segment duration must be positive (start_time_s %v, end_time_s %v)",
				i, s.StartTime, s.EndTime),
		}
	}
	if err := checkSegmentAltitude(i, s.StartAltitude); err != nil {
		return err
	}
	return checkSegmentAltitude(i, s.EndAltitude)
}

// checkSegmentAltitude applies the model's altitude domain rules to one
// endpoint, reusing the model's error codes with the segment index added.
func checkSegmentAltitude(i int, h float64) *ModelError {
	switch {
	case math.IsNaN(h) || math.IsInf(h, 0):
		return &ModelError{
			Code:    ErrInvalidAltitude,
			Message: fmt.Sprintf("segments[%d]: altitude must be a finite number, got %v", i, h),
		}
	case h < 0:
		return &ModelError{
			Code: ErrAltitudeBelowSeaLevel,
			Message: fmt.Sprintf("segments[%d]: altitude %.3f m is below mean sea level; the model domain is 0..%.0f m",
				i, h, ModelTopAltitude),
		}
	case h > ModelTopAltitude:
		return &ModelError{
			Code: ErrAltitudeAboveCeiling,
			Message: fmt.Sprintf("segments[%d]: altitude %.3f m exceeds the %.0f m implementation ceiling; higher atmospheric layers are not modelled or extrapolated",
				i, h, ModelTopAltitude),
		}
	}
	return nil
}

// checkJunction verifies that segment i continues segment i-1 without a
// jump in time or altitude.
func checkJunction(i int, prev, next Segment) *ModelError {
	if !continuous(prev.EndTime, next.StartTime) {
		return errTrajectoryGap(i, "start_time_s", prev.EndTime, next.StartTime)
	}
	if !continuous(prev.EndAltitude, next.StartAltitude) {
		return errTrajectoryGap(i, "start_altitude_m", prev.EndAltitude, next.StartAltitude)
	}
	return nil
}

// continuous reports whether two junction values agree to gapRelTol.
func continuous(a, b float64) bool {
	return math.Abs(a-b) <= gapRelTol*math.Max(1.0, math.Max(math.Abs(a), math.Abs(b)))
}
