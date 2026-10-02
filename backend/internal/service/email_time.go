package service

import "time"

// Email presentation always uses Beijing time, independently of the host or
// database timezone. Converting the location preserves the event/send instant.
var emailTimeZone = time.FixedZone("UTC+8", 8*60*60)

func formatEmailTime(at time.Time) string {
	return at.In(emailTimeZone).Format("2006-01-02 15:04:05")
}
