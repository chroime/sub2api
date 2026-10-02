package upstreamgovernance

import "slices"

// An omitted list accepts the legacy single-group field. An explicitly empty
// list is invalid; conflicting legacy/new fields must not hide a destination.
func NormalizeLocalGroupIDs(ids []int64, legacy int64) ([]int64, error) {
	if ids == nil {
		ids = []int64{legacy}
	}
	if len(ids) == 0 || len(ids) > 100 || legacy < 0 {
		return nil, ErrInvalid
	}
	result := append([]int64(nil), ids...)
	for _, id := range result {
		if id <= 0 {
			return nil, ErrInvalid
		}
	}
	slices.Sort(result)
	result = slices.Compact(result)
	if legacy > 0 && !slices.Contains(result, legacy) {
		return nil, ErrInvalid
	}
	return result, nil
}

func sameGroupIDs(left, right []int64) bool {
	l, le := NormalizeLocalGroupIDs(left, 0)
	r, re := NormalizeLocalGroupIDs(right, 0)
	return le == nil && re == nil && slices.Equal(l, r)
}

func reviewedTargets(row PreviewRow) bool {
	if row.Selection.AccountConfig == nil || row.Selection.AccountConfig.Priority == nil || len(row.Selection.LocalGroupIDs) == 0 || len(row.Targets) != len(row.Selection.LocalGroupIDs) {
		return false
	}
	for i, id := range row.Selection.LocalGroupIDs {
		if row.Targets[i].ID != id || row.Targets[i].Fingerprint == "" {
			return false
		}
	}
	return true
}
