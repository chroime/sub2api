package upstreamgovernance

import "time"

// Intervals use PostgreSQL INTEGER fields. This is the storage representation
// limit, rather than a policy restriction on a site's chosen cadence.
const maxIntervalMinutes = 1<<31 - 1
// Keep the seconds schedule within the range that the legacy INTEGER minute
// alias and time.AddDate can represent on supported 64-bit builds.
const maxIntervalSeconds = int64(maxIntervalMinutes) * 60

func validIntervalMinutes(minutes int) bool {
	return minutes >= 1 && minutes <= maxIntervalMinutes
}

func validIntervalSeconds(seconds int64) bool {
	return seconds >= 1 && seconds <= maxIntervalSeconds
}

// addMinutes keeps large configured intervals (and their freshness multiples)
// out of time.Duration, whose nanosecond representation overflows after about
// 292 years. Add whole days in UTC so elapsed minutes stay exact through DST,
// then restore the caller's location. Supported intervals fit in a PostgreSQL
// INTEGER; even a doubled interval's day count fits in a 32-bit int.
func addMinutes(at time.Time, minutes int64) time.Time {
	const minutesPerDay = 24 * 60
	return at.UTC().AddDate(0, 0, int(minutes/minutesPerDay)).
		Add(time.Duration(minutes%minutesPerDay) * time.Minute).In(at.Location())
}

// addSeconds avoids time.Duration overflow for a BIGINT schedule while
// preserving wall-clock elapsed seconds and the caller's location.
func addSeconds(at time.Time, seconds int64) time.Time {
	const secondsPerDay = int64(24 * 60 * 60)
	days, remainder := seconds/secondsPerDay, seconds%secondsPerDay
	return at.UTC().AddDate(0, 0, int(days)).Add(time.Duration(remainder) * time.Second).In(at.Location())
}
