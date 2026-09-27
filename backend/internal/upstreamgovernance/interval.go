package upstreamgovernance

import "time"

// Intervals use PostgreSQL INTEGER fields. This is the storage representation
// limit, rather than a policy restriction on a site's chosen cadence.
const maxIntervalMinutes = 1<<31 - 1

func validIntervalMinutes(minutes int) bool {
	return minutes >= 1 && minutes <= maxIntervalMinutes
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
