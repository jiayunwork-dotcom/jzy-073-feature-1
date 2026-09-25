package atmosphere

import (
	"math"
	"strings"
	"testing"
)

func TestValidateTrajectoryAcceptsLegalTrajectories(t *testing.T) {
	cases := map[string][]Segment{
		"single climb": {
			{StartTime: 0, EndTime: 600, StartAltitude: 0, EndAltitude: 10000},
		},
		"climb cruise descend chain": {
			{StartTime: 0, EndTime: 600, StartAltitude: 0, EndAltitude: 10000},
			{StartTime: 600, EndTime: 3600, StartAltitude: 10000, EndAltitude: 10000},
			{StartTime: 3600, EndTime: 4200, StartAltitude: 10000, EndAltitude: 0},
		},
		"level flight at ceiling": {
			{StartTime: -100, EndTime: 0, StartAltitude: ModelTopAltitude, EndAltitude: ModelTopAltitude},
		},
		"sea level touch and junction touch": {
			{StartTime: 0, EndTime: 1, StartAltitude: 0, EndAltitude: TropopauseAltitude},
			{StartTime: 1, EndTime: 2, StartAltitude: TropopauseAltitude, EndAltitude: ModelTopAltitude},
		},
		"junction within float tolerance": {
			{StartTime: 0, EndTime: 600, StartAltitude: 0, EndAltitude: 10000},
			// 1e-10 relative noise on both junction values must not be a "gap".
			{StartTime: 600 * (1 + 1e-10), EndTime: 1200, StartAltitude: 10000 * (1 + 1e-10), EndAltitude: 5000},
		},
	}
	for name, segs := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateTrajectory(segs); err != nil {
				t.Errorf("ValidateTrajectory rejected legal trajectory: %v", err)
			}
		})
	}
}

func TestValidateTrajectoryRejectsStructuralProblems(t *testing.T) {
	if err := ValidateTrajectory(nil); err == nil || err.Code != ErrInvalidTrajectory {
		t.Errorf("empty trajectory: got %v, want %s", err, ErrInvalidTrajectory)
	}
	if err := ValidateTrajectory([]Segment{}); err == nil || err.Code != ErrInvalidTrajectory {
		t.Errorf("zero segments: got %v, want %s", err, ErrInvalidTrajectory)
	}

	tooMany := make([]Segment, MaxTrajectorySegments+1)
	for i := range tooMany {
		tooMany[i] = Segment{StartTime: float64(i), EndTime: float64(i + 1), StartAltitude: 100, EndAltitude: 100}
	}
	if err := ValidateTrajectory(tooMany); err == nil || err.Code != ErrInvalidTrajectory {
		t.Errorf("too many segments: got %v, want %s", err, ErrInvalidTrajectory)
	}

	nonPositive := []Segment{{StartTime: 100, EndTime: 100, StartAltitude: 0, EndAltitude: 100}}
	if err := ValidateTrajectory(nonPositive); err == nil || err.Code != ErrInvalidTrajectory {
		t.Errorf("zero duration: got %v, want %s", err, ErrInvalidTrajectory)
	}
	backwards := []Segment{{StartTime: 100, EndTime: 50, StartAltitude: 0, EndAltitude: 100}}
	if err := ValidateTrajectory(backwards); err == nil || err.Code != ErrInvalidTrajectory {
		t.Errorf("negative duration: got %v, want %s", err, ErrInvalidTrajectory)
	}

	nonFinite := []Segment{
		{StartTime: 0, EndTime: math.Inf(1), StartAltitude: 0, EndAltitude: 100},
		{StartTime: math.NaN(), EndTime: 10, StartAltitude: 0, EndAltitude: 100},
	}
	for _, seg := range nonFinite {
		if err := ValidateTrajectory([]Segment{seg}); err == nil || err.Code != ErrInvalidTrajectory {
			t.Errorf("non-finite time: got %v, want %s", err, ErrInvalidTrajectory)
		}
	}
}

// Rule: a trajectory that does not connect is illegal, and the error must
// say which junction broke — the segment index appears in the message.
func TestValidateTrajectoryRejectsGaps(t *testing.T) {
	climb := Segment{StartTime: 0, EndTime: 600, StartAltitude: 0, EndAltitude: 10000}

	altitudeGap := []Segment{climb, {StartTime: 600, EndTime: 1200, StartAltitude: 9500, EndAltitude: 5000}}
	err := ValidateTrajectory(altitudeGap)
	if err == nil || err.Code != ErrTrajectoryGap {
		t.Fatalf("altitude gap: got %v, want %s", err, ErrTrajectoryGap)
	}
	if !strings.Contains(err.Message, "segments[1]") || !strings.Contains(err.Message, "start_altitude_m") {
		t.Errorf("altitude gap message must name the segment and quantity: %q", err.Message)
	}

	timeGap := []Segment{climb, {StartTime: 601, EndTime: 1200, StartAltitude: 10000, EndAltitude: 5000}}
	err = ValidateTrajectory(timeGap)
	if err == nil || err.Code != ErrTrajectoryGap {
		t.Fatalf("time gap: got %v, want %s", err, ErrTrajectoryGap)
	}
	if !strings.Contains(err.Message, "segments[1]") || !strings.Contains(err.Message, "start_time_s") {
		t.Errorf("time gap message must name the segment and quantity: %q", err.Message)
	}

	// The break is reported at the junction where it happens, not the first
	// segment of the chain.
	threeLegs := []Segment{
		climb,
		{StartTime: 600, EndTime: 1200, StartAltitude: 10000, EndAltitude: 11000},
		{StartTime: 1200, EndTime: 1800, StartAltitude: 11001, EndAltitude: 5000},
	}
	err = ValidateTrajectory(threeLegs)
	if err == nil || err.Code != ErrTrajectoryGap || !strings.Contains(err.Message, "segments[2]") {
		t.Errorf("late break: got %v, want TRAJECTORY_GAP naming segments[2]", err)
	}
}

// Rule: any altitude outside the model domain — even a single endpoint deep
// inside a long trajectory — rejects the whole request. The service never
// extrapolates the atmosphere to finish an integral.
func TestValidateTrajectoryRejectsOutOfDomainAltitudes(t *testing.T) {
	cases := map[string]struct {
		segs []Segment
		want ErrorCode
	}{
		"below sea level": {
			[]Segment{{StartTime: 0, EndTime: 10, StartAltitude: -0.5, EndAltitude: 100}},
			ErrAltitudeBelowSeaLevel,
		},
		"above ceiling": {
			[]Segment{{StartTime: 0, EndTime: 10, StartAltitude: 100, EndAltitude: ModelTopAltitude + 0.5}},
			ErrAltitudeAboveCeiling,
		},
		"out of domain in later segment": {
			[]Segment{
				{StartTime: 0, EndTime: 10, StartAltitude: 0, EndAltitude: 5000},
				{StartTime: 10, EndTime: 20, StartAltitude: 5000, EndAltitude: 25000},
			},
			ErrAltitudeAboveCeiling,
		},
		"non-finite altitude": {
			[]Segment{{StartTime: 0, EndTime: 10, StartAltitude: 0, EndAltitude: math.Inf(1)}},
			ErrInvalidAltitude,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidateTrajectory(tc.segs)
			if err == nil || err.Code != tc.want {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
			if !strings.Contains(err.Message, "segments[") {
				t.Errorf("message must name the offending segment: %q", err.Message)
			}
		})
	}
}

// AccumulateTrajectory must refuse to produce numbers for an illegal
// trajectory — validation is not the caller's problem.
func TestAccumulateTrajectoryValidatesFirst(t *testing.T) {
	broken := []Segment{
		{StartTime: 0, EndTime: 600, StartAltitude: 0, EndAltitude: 10000},
		{StartTime: 600, EndTime: 1200, StartAltitude: 9000, EndAltitude: 5000},
	}
	if _, err := AccumulateTrajectory(broken); err == nil || err.Code != ErrTrajectoryGap {
		t.Errorf("AccumulateTrajectory accepted broken trajectory: %v", err)
	}
}
