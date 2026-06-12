ALTER TABLE lc_invoices
    ADD COLUMN IF NOT EXISTS document_url TEXT,
    ADD COLUMN IF NOT EXISTS document_generated_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS ubl_xml TEXT,
    ADD COLUMN IF NOT EXISTS ubl_generated_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS payment_checked_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS payment_status TEXT,
    ADD COLUMN IF NOT EXISTS payment_last_error TEXT,
    ADD COLUMN IF NOT EXISTS reminder_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_reminder_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_lc_invoices_payment_checked
    ON lc_invoices (user_id, payment_checked_at DESC)
    WHERE payment_checked_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_lc_invoices_document_generated
    ON lc_invoices (user_id, document_generated_at DESC)
    WHERE document_generated_at IS NOT NULL;
