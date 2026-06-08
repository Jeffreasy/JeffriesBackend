-- 019_laventecare_invoice_quote_link.up.sql
-- Link LaventeCare invoices back to the accepted quote they originated from.

ALTER TABLE lc_invoices
    ADD COLUMN IF NOT EXISTS quote_id UUID REFERENCES lc_quotes(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_lc_invoices_quote
    ON lc_invoices (quote_id, created_at DESC)
    WHERE quote_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_lc_invoices_quote_unique_active
    ON lc_invoices (user_id, quote_id)
    WHERE quote_id IS NOT NULL AND status <> 'geannuleerd';
