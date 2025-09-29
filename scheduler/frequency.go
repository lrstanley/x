// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.
//
// A good portion of this code is based on github.com/robfig/cron. Copyright (C)
// 2012 Rob Figueiredo, All Rights Reserved (MIT License), which the license can
// be found at: https://github.com/robfig/cron/blob/master/LICENSE

package scheduler

import (
	"fmt"
	"time"
)

// FrequencySchedule represents a simple recurring duty cycle, e.g. "Every
// 5 minutes". It does not support jobs more frequent than once a second.
type FrequencySchedule struct {
	Delay time.Duration
}

func (s FrequencySchedule) String() string {
	return fmt.Sprintf("@every %s", s.Delay.Round(time.Second))
}

// Every returns a crontab Schedule that activates once every duration. Delays
// of less than a second are not supported (will round up to 1 second). Any fields
// less than a Second are truncated.
func Every(dur time.Duration) FrequencySchedule {
	if dur < time.Second {
		dur = time.Second
	}
	return FrequencySchedule{
		Delay: dur - time.Duration(dur.Nanoseconds())%time.Second,
	}
}

// Next returns the next time this should be run. This rounds so that the next
// activation time will be on the second.
func (s FrequencySchedule) Next(t time.Time) time.Time {
	return t.Add(s.Delay - time.Duration(t.Nanosecond())*time.Nanosecond)
}
