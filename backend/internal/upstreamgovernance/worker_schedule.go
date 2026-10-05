package upstreamgovernance

import (
	"hash/fnv"
	"strconv"
	"time"
)

// The scheduler deliberately keeps retry state in memory while persisting the
// resulting deadline in the site row. A process restart therefore never loses a
// reserved next run, while a successful observation immediately clears the
// transient backoff state.
const (
	maxCollectionFailures       = 5
	maxCollectionBackoff        = 15 * time.Minute
	maxCollectionBackoffSecs    = int64(maxCollectionBackoff / time.Second)
	maxScheduleJitterMillis     = int64(5_000)
	minimumScheduleJitterMillis = int64(1)
)

type scheduleKind uint8

const (
	scheduleCollection scheduleKind = iota
	scheduleFastObservation
)

// scheduledNextAt returns a deadline at least intervalSeconds in the future.
// A small deterministic per-site jitter spreads otherwise synchronized sites;
// deriving it from the site ID makes the spread stable across process restarts.
// Failure attempts use exponential backoff, capped at fifteen minutes while
// never shortening a caller's configured cadence.
func scheduledNextAt(now time.Time, intervalSeconds int64, failureCount int, siteID int64, kind scheduleKind) time.Time {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if intervalSeconds < 1 {
		intervalSeconds = 1
	}
	if failureCount < 0 {
		failureCount = 0
	}
	if failureCount > maxCollectionFailures {
		failureCount = maxCollectionFailures
	}
	delaySeconds := intervalSeconds
	if failureCount > 0 {
		multiplier := int64(1)
		for i := 0; i < failureCount; i++ {
			if multiplier >= maxCollectionBackoffSecs {
				break
			}
			multiplier *= 2
		}
		if intervalSeconds < maxCollectionBackoffSecs {
			// Saturate before multiplying, rather than falling back to the
			// configured cadence as soon as the exponential crosses the cap.
			if intervalSeconds > maxCollectionBackoffSecs/multiplier {
				delaySeconds = maxCollectionBackoffSecs
			} else {
				delaySeconds = intervalSeconds * multiplier
			}
		}
	}

	jitter := int64(0)
	// Full catalog cadence is an administrator-visible contract and remains
	// exact on successful runs. Only the optional second-based observer needs
	// persisted jitter to prevent a fleet of sites from waking on one second.
	if kind == scheduleFastObservation {
		jitter = scheduleJitterMillis(intervalSeconds, siteID, failureCount, kind)
	}
	return addSeconds(now, delaySeconds).Add(time.Duration(jitter) * time.Millisecond)
}

func scheduleJitterMillis(intervalSeconds, siteID int64, failureCount int, kind scheduleKind) int64 {
	// Keep jitter below ten percent of the configured cadence and at most five
	// seconds. The minimum is one millisecond for sub-minute cadences.
	max := maxScheduleJitterMillis
	if intervalSeconds <= maxScheduleJitterMillis/100 {
		max = intervalSeconds * 100
	}
	if max < minimumScheduleJitterMillis {
		max = minimumScheduleJitterMillis
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(strconv.FormatInt(siteID, 10)))
	_, _ = h.Write([]byte{0, byte(kind), byte(failureCount)})
	return int64(h.Sum64() % uint64(max))
}
