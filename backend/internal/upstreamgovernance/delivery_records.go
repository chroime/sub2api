package upstreamgovernance

import (
	"context"
	"time"
)

// Delivery channels listed by DeliveryRecords.
const (
	DeliveryChannelEmail   = "email"   // upstream change mails to administrators
	DeliveryChannelAstrBot = "astrbot" // local group change messages to QQ groups
)

// DeliveryRecord is one outbound notification as the settings page lists it.
// Mail rows carry the site and recipient; AstrBot rows carry the local group.
type DeliveryRecord struct {
	ID        int64      `json:"id"`
	Channel   string     `json:"channel"`
	SiteID    int64      `json:"site_id,omitempty"`
	SiteName  string     `json:"site_name,omitempty"`
	GroupID   int64      `json:"group_id,omitempty"`
	GroupName string     `json:"group_name,omitempty"`
	Kind      string     `json:"kind"`
	Subject   string     `json:"subject,omitempty"`
	Recipient string     `json:"recipient,omitempty"`
	Status    string     `json:"status"`
	Attempts  int        `json:"attempts"`
	LastError string     `json:"last_error,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	SentAt    *time.Time `json:"sent_at,omitempty"`
}

// DeliveryRecordReader lists outbound notification records, newest first.
type DeliveryRecordReader interface {
	ListDeliveryRecords(ctx context.Context, channel string, page, size int) ([]DeliveryRecord, int64, error)
}

func (s *Service) DeliveryRecords(ctx context.Context, channel string, page, size int) ([]DeliveryRecord, int64, error) {
	if channel != DeliveryChannelEmail && channel != DeliveryChannelAstrBot {
		return nil, 0, ErrInvalid
	}
	reader, ok := s.store.(DeliveryRecordReader)
	if !ok {
		return nil, 0, ErrUnsupported
	}
	return reader.ListDeliveryRecords(ctx, channel, page, size)
}

func (s *sqlStore) ListDeliveryRecords(ctx context.Context, channel string, page, size int) ([]DeliveryRecord, int64, error) {
	countQuery := `SELECT COUNT(*) FROM upstream_governance_change_notifications`
	listQuery := `SELECT n.id, n.site_id, COALESCE(s.name, ''), 0::bigint, '', n.kind, n.subject, n.recipient, n.status, n.attempts, n.last_error, n.created_at, n.sent_at
FROM upstream_governance_change_notifications n LEFT JOIN upstream_governance_sites s ON s.id = n.site_id
ORDER BY n.id DESC LIMIT $1 OFFSET $2`
	if channel == DeliveryChannelAstrBot {
		countQuery = `SELECT COUNT(*) FROM upstream_governance_local_group_changes`
		listQuery = `SELECT c.id, 0::bigint, '', c.group_id, COALESCE(g.name, ''), c.kind, '', '', c.status, c.attempts, c.last_error, c.created_at, c.processed_at
FROM upstream_governance_local_group_changes c LEFT JOIN groups g ON g.id = c.group_id
ORDER BY c.id DESC LIMIT $1 OFFSET $2`
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, countQuery).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, listQuery, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	records := []DeliveryRecord{}
	for rows.Next() {
		record := DeliveryRecord{Channel: channel}
		if err = rows.Scan(&record.ID, &record.SiteID, &record.SiteName, &record.GroupID, &record.GroupName, &record.Kind, &record.Subject,
			&record.Recipient, &record.Status, &record.Attempts, &record.LastError, &record.CreatedAt, &record.SentAt); err != nil {
			return nil, 0, err
		}
		records = append(records, record)
	}
	return records, total, rows.Err()
}
