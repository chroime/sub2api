-- This fork already exposes and enforces allow_image_generation for Gemini
-- family groups. An existing false value can be an explicit administrator
-- decision, so upstream's blanket enablement would overwrite local policy.
-- Preserve every existing permission; new-group defaults are handled by the
-- application. Keep the upstream migration filename as a recorded no-op.
SELECT 1;
