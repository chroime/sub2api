package upstreamgovernance

import "time"

// fastObservationChangeNotices turns a changed complete fast catalog into the
// same credential-free, per-group notices used by full collection. Sharing the
// event digest lets a later full catalog collection coalesce the same change in
// the durable queue instead of sending a duplicate message.
func fastObservationChangeNotices(site Site, previous *CatalogObservation, observation GroupObservation, revision int64, observedAt time.Time) []ChangeNotice {
	if previous == nil || previous.Revision <= 0 || revision <= 1 || !previous.GroupsComplete || !observation.GroupsComplete {
		return nil
	}
	events := DiffCatalog(site.ID, Catalog{GroupsComplete: true, Groups: previous.Groups}, Catalog{GroupsComplete: true, Groups: observation.Groups})
	return renderCatalogChangeNotices(site, events, Catalog{GroupsComplete: true, Groups: observation.Groups}, observedAt, true)
}
