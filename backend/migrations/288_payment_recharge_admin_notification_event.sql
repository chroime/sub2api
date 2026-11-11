-- Record administrator recharge-success notices alongside the existing email
-- audit without rewriting historical classifications or delivery state.
ALTER TABLE user_balance_notification_records
    DROP CONSTRAINT user_balance_notification_records_event_type_check;

ALTER TABLE user_balance_notification_records
    ADD CONSTRAINT user_balance_notification_records_event_type_check
    CHECK (event_type IN ('balance.low', 'system.health_snapshot', 'payment.recharge_success_admin'));
