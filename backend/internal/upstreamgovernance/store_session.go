package upstreamgovernance

import "context"

var _ runtimeSessionStore = (*sqlStore)(nil)

func (s *sqlStore) SaveRuntimeSession(ctx context.Context, id, version int64, previous, encrypted string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE upstream_governance_sites SET session_cipher=$4 WHERE id=$1 AND version=$2 AND session_cipher=$3`, id, version, previous, encrypted)
	return affected(result, err, ErrConflict)
}
