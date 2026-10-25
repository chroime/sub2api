package accountquality

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type Notification struct {
	ID                     int64
	Recipient, Body, Event string
	Attempts               int
}
type Notifier interface {
	SendAccountQualityNotice(context.Context, string, string) error
}

func enqueueNoticeTx(ctx context.Context, tx *sql.Tx, s Settings, b Binding, r Run, event string, reminder ...bool) error {
	if event == "" || r.Config.ControlMode == "record_only" {
		return nil
	}
	enabled := false
	for _, v := range s.Notifications.Events {
		if v == event {
			enabled = true
		}
	}
	if !enabled {
		return nil
	}
	targets := []string{}
	if s.Notifications.EmailEnabled {
		targets = append(targets, s.Notifications.EmailRecipients...)
	}
	if s.Notifications.QQEnabled {
		for _, target := range s.Notifications.QQTargets {
			targets = append(targets, "qq:"+target)
		}
	}
	labels := map[string]string{"suspected": "首次明确不达标，等待复核", "quarantined": "已进入质量隔离", "recovered": "质量复测已恢复", "manual": "管理员调整质量管控", "budget_paused": "诊断预算暂停"}
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	at := r.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}
	body := fmt.Sprintf("【账号质量监测】\n账号：%s（#%d）\n模型：%s\n事件：%s\n当前状态：%s\n连续明确不达标：%d\n原因：%s\n检测时间：%s\n详情：/admin/upstream-governance/account-quality?account=%d&run=%s", b.AccountName, b.AccountID, b.Model, labels[event], b.State, b.Failures, b.Reason, at.In(loc).Format("2006-01-02 15:04:05"), b.AccountID, r.ID)
	seen := map[string]bool{}
	for _, recipient := range targets {
		recipient = strings.TrimSpace(recipient)
		if recipient == "" || seen[recipient] {
			continue
		}
		seen[recipient] = true
		key := fmt.Sprintf("%d:%s:%s:%s", b.ID, r.ID, event, recipient)
		var e error
		if event == "budget_paused" || (len(reminder) > 0 && reminder[0]) {
			_, e = tx.ExecContext(ctx, "INSERT INTO account_quality_notifications(dedup_key,event,recipient,body,binding_id) SELECT $1,$2,$3,$4,$5 WHERE NOT EXISTS(SELECT 1 FROM account_quality_notifications WHERE binding_id=$5 AND event=$2 AND recipient=$3 AND created_at>$6) ON CONFLICT(dedup_key) DO NOTHING", key, event, recipient, body, b.ID, time.Now().Add(-time.Duration(s.Notifications.ReminderSeconds)*time.Second))
		} else {
			_, e = tx.ExecContext(ctx, "INSERT INTO account_quality_notifications(dedup_key,event,recipient,body,binding_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT(dedup_key) DO NOTHING", key, event, recipient, body, b.ID)
		}
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) claimNotification(ctx context.Context) (Notification, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Notification{}, e
	}
	defer tx.Rollback()
	var n Notification
	e = tx.QueryRowContext(ctx, "SELECT id,recipient,body,event,attempts FROM account_quality_notifications WHERE status IN ('pending','sending') AND next_attempt_at<=NOW() ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1").Scan(&n.ID, &n.Recipient, &n.Body, &n.Event, &n.Attempts)
	if e != nil {
		return n, storeErr(e)
	}
	_, e = tx.ExecContext(ctx, "UPDATE account_quality_notifications SET status='sending',attempts=attempts+1,next_attempt_at=NOW()+INTERVAL '2 minutes' WHERE id=$1", n.ID)
	if e != nil {
		return n, e
	}
	return n, tx.Commit()
}
func (s *Store) finishNotification(ctx context.Context, n Notification, delivery error) error {
	if delivery == nil {
		_, e := s.DB.ExecContext(ctx, "UPDATE account_quality_notifications SET status='sent',last_error='' WHERE id=$1", n.ID)
		return e
	}
	delay := time.Duration(min(3600, 30*(1<<min(n.Attempts, 7)))) * time.Second
	_, e := s.DB.ExecContext(ctx, "UPDATE account_quality_notifications SET status='pending',last_error=$2,next_attempt_at=$3 WHERE id=$1", n.ID, "通知发送失败；等待重试", time.Now().Add(delay))
	return e
}
