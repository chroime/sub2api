-- New reservations end with their order's payment deadline. Keep historical
-- reuse_after values and payment windows intact for delayed reconciliation.
DO $$
DECLARE
    constraint_name TEXT;
BEGIN
    FOR constraint_name IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'payment_bill_amount_reservations'::regclass
          AND contype = 'c'
          AND pg_get_constraintdef(oid) LIKE '%reuse_after%'
    LOOP
        EXECUTE format('ALTER TABLE payment_bill_amount_reservations DROP CONSTRAINT %I', constraint_name);
    END LOOP;
END $$;

ALTER TABLE payment_bill_amount_reservations
    ADD CONSTRAINT payment_bill_amount_reservations_deadlines_check
    CHECK (expires_at > created_at AND reuse_after >= expires_at);
