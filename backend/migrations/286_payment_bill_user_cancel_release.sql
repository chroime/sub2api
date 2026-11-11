-- User cancellation releases allocation immediately while retaining the exact
-- historical ownership endpoint for delayed statements. Administrative cancels
-- and expiry do not receive this marker.
ALTER TABLE payment_bill_amount_reservations
    ADD COLUMN user_cancelled_at TIMESTAMPTZ
    CHECK (user_cancelled_at IS NULL OR (user_cancelled_at >= created_at AND user_cancelled_at <= expires_at));

-- Backfill only positively identified user actions; never enroll legacy orders
-- without a reservation or infer intent from CANCELLED status alone.
UPDATE payment_bill_amount_reservations AS a
SET user_cancelled_at = CASE
    WHEN c.cancelled_at < a.created_at THEN a.created_at
    WHEN c.cancelled_at > a.expires_at THEN a.expires_at
    ELSE c.cancelled_at END
FROM payment_orders AS o
JOIN (
    SELECT l.order_id,l.operator,MIN(l.created_at) AS cancelled_at
    FROM payment_audit_logs AS l
    WHERE l.action='ORDER_CANCELLED'
    GROUP BY l.order_id,l.operator
) AS c ON c.order_id=CAST(o.id AS TEXT) AND c.operator='user:' || CAST(o.user_id AS TEXT)
WHERE a.order_id=o.id AND o.status='CANCELLED' AND a.user_cancelled_at IS NULL;
