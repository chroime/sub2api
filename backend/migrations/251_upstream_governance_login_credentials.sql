-- Administrator-editable dashboard login details are encrypted separately from
-- active sessions. Ordinary site metadata must never expose this ciphertext.
ALTER TABLE upstream_governance_sites ADD COLUMN login_cipher TEXT NOT NULL DEFAULT '';
