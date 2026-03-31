// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.
//
// A good portion of this code is based on github.com/robfig/cron. Copyright (C)
// 2012 Rob Figueiredo, All Rights Reserved (MIT License), which the license can
// be found at: https://github.com/robfig/cron/blob/master/LICENSE

package scheduler

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Schedule describes a job's duty cycle.
type Schedule interface {
	// Next returns the next activation time, later than the given time. Next is
	// invoked initially, and then each time the job is run.
	Next(time.Time) time.Time

	// String returns the string representation of the schedule.
	String() string
}

// Parse returns a new crontab schedule representing the given spec. It requires
// 5 entries representing: minute, hour, day of month, month and day of week, or
// descriptors, e.g. "@midnight", "@every 1h30m".
func Parse(spec string) (Schedule, error) {
	spec = strings.TrimSpace(spec)

	if spec == "" {
		return nil, errors.New("empty spec string")
	}

	tz := time.Local
	if strings.HasPrefix(spec, "CRON_TZ=") {
		var err error
		i := strings.Index(spec, " ")
		eq := strings.Index(spec, "=")
		if tz, err = time.LoadLocation(spec[eq+1 : i]); err != nil {
			return nil, fmt.Errorf("provided bad location %s: %w", spec[eq+1:i], err)
		}
		spec = strings.TrimSpace(spec[i:])
	}

	if strings.HasPrefix(spec, "@") {
		return parseDescriptor(spec, tz)
	}

	fields := strings.Fields(spec)

	if len(fields) != 5 {
		return nil, fmt.Errorf("expected exactly 5 fields, found %d: %s", len(fields), fields)
	}

	schedule := &SpecSchedule{
		Source:   spec,
		Location: tz,
	}

	var err error
	schedule.Minute, err = getField(fields[0], minutes)
	if err != nil {
		return nil, err
	}
	schedule.Hour, err = getField(fields[1], hours)
	if err != nil {
		return nil, err
	}
	schedule.DayOfMonth, err = getField(fields[2], dom)
	if err != nil {
		return nil, err
	}
	schedule.Month, err = getField(fields[3], months)
	if err != nil {
		return nil, err
	}
	schedule.DayOfWeek, err = getField(fields[4], dow)
	if err != nil {
		return nil, err
	}
	return schedule, nil
}

// getField returns an Int with the bits set representing all of the times that
// the field represents or error parsing field value. A "field" is a comma-separated
// list of "ranges".
func getField(field string, r bounds) (uint64, error) {
	var bits uint64
	ranges := strings.FieldsFunc(field, func(r rune) bool { return r == ',' })
	for _, expr := range ranges {
		bit, err := getRange(expr, r)
		if err != nil {
			return bits, err
		}
		bits |= bit
	}
	return bits, nil
}

// getRange returns the bits indicated by the given expression (or error parsing
// range):
//
//	number | number "-" number [ "/" number ]
func getRange(expr string, r bounds) (uint64, error) {
	var (
		start, end, step uint
		rangeAndStep     = strings.Split(expr, "/")
		lowAndHigh       = strings.Split(rangeAndStep[0], "-")
		singleDigit      = len(lowAndHigh) == 1
		err              error
	)

	var extra uint64
	if lowAndHigh[0] == "*" || lowAndHigh[0] == "?" {
		start = r.min
		end = r.max
		extra = starBit
	} else {
		start, err = parseIntOrName(lowAndHigh[0], r.names)
		if err != nil {
			return 0, err
		}
		switch len(lowAndHigh) {
		case 1:
			end = start
		case 2:
			end, err = parseIntOrName(lowAndHigh[1], r.names)
			if err != nil {
				return 0, err
			}
		default:
			return 0, fmt.Errorf("too many hyphens: %s", expr)
		}
	}

	switch len(rangeAndStep) {
	case 1:
		step = 1
	case 2:
		step, err = mustParseInt(rangeAndStep[1])
		if err != nil {
			return 0, err
		}

		// Special handling: "N/step" means "N-max/step".
		if singleDigit {
			end = r.max
		}
		if step > 1 {
			extra = 0
		}
	default:
		return 0, fmt.Errorf("too many slashes: %s", expr)
	}

	if start < r.min {
		return 0, fmt.Errorf("beginning of range (%d) below minimum (%d): %s", start, r.min, expr)
	}
	if end > r.max {
		return 0, fmt.Errorf("end of range (%d) above maximum (%d): %s", end, r.max, expr)
	}
	if start > end {
		return 0, fmt.Errorf("beginning of range (%d) beyond end of range (%d): %s", start, end, expr)
	}
	if step == 0 {
		return 0, fmt.Errorf("step of range should be a positive number: %s", expr)
	}

	return getBits(start, end, step) | extra, nil
}

// parseIntOrName returns the (possibly-named) integer contained in expr.
func parseIntOrName(expr string, names map[string]uint) (uint, error) {
	if names != nil {
		if namedInt, ok := names[strings.ToLower(expr)]; ok {
			return namedInt, nil
		}
	}
	return mustParseInt(expr)
}

// mustParseInt parses the given expression as an int or returns an error.
func mustParseInt(expr string) (uint, error) {
	num, err := strconv.Atoi(expr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse int from %s: %w", expr, err)
	}
	if num < 0 {
		return 0, fmt.Errorf("negative number (%d) not allowed: %s", num, expr)
	}

	return uint(num), nil
}

// getBits sets all bits in the range [min, max], modulo the given step size.
func getBits(bmin, bmax, step uint) uint64 {
	var bits uint64

	// If step is 1, use shifts.
	if step == 1 {
		return ^(math.MaxUint64 << (bmax + 1)) & (math.MaxUint64 << bmin)
	}

	// Else, use a simple loop.
	for i := bmin; i <= bmax; i += step {
		bits |= 1 << i
	}
	return bits
}

// all returns all bits within the given bounds (plus the star bit).
func all(r bounds) uint64 {
	return getBits(r.min, r.max, 1) | starBit
}

// parseDescriptor returns a predefined schedule for the expression, or error if
// none matches.
func parseDescriptor(descriptor string, loc *time.Location) (Schedule, error) {
	switch descriptor {
	case "@yearly", "@annually":
		return &SpecSchedule{
			Source:     descriptor,
			Minute:     1 << minutes.min,
			Hour:       1 << hours.min,
			DayOfMonth: 1 << dom.min,
			Month:      1 << months.min,
			DayOfWeek:  all(dow),
			Location:   loc,
		}, nil

	case "@monthly":
		return &SpecSchedule{
			Source:     descriptor,
			Minute:     1 << minutes.min,
			Hour:       1 << hours.min,
			DayOfMonth: 1 << dom.min,
			Month:      all(months),
			DayOfWeek:  all(dow),
			Location:   loc,
		}, nil

	case "@weekly":
		return &SpecSchedule{
			Source:     descriptor,
			Minute:     1 << minutes.min,
			Hour:       1 << hours.min,
			DayOfMonth: all(dom),
			Month:      all(months),
			DayOfWeek:  1 << dow.min,
			Location:   loc,
		}, nil

	case "@daily", "@midnight":
		return &SpecSchedule{
			Source:     descriptor,
			Minute:     1 << minutes.min,
			Hour:       1 << hours.min,
			DayOfMonth: all(dom),
			Month:      all(months),
			DayOfWeek:  all(dow),
			Location:   loc,
		}, nil

	case "@hourly":
		return &SpecSchedule{
			Source:     descriptor,
			Minute:     1 << minutes.min,
			Hour:       all(hours),
			DayOfMonth: all(dom),
			Month:      all(months),
			DayOfWeek:  all(dow),
			Location:   loc,
		}, nil
	}

	const every = "@every "
	if strings.HasPrefix(descriptor, every) {
		duration, err := time.ParseDuration(descriptor[len(every):])
		if err != nil {
			return nil, fmt.Errorf("failed to parse duration %s: %w", descriptor, err)
		}
		return Every(duration), nil
	}

	return nil, fmt.Errorf("unrecognized descriptor: %s", descriptor)
}
