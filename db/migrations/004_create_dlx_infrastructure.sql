-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS dead_letter_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    original_message_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    payload JSONB NOT NULL,
    error_reason TEXT,
    retry_count INT DEFAULT 0,
    failed_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_dead_letter_tenant ON dead_letter_messages(tenant_id);
CREATE INDEX IF NOT EXISTS idx_dead_letter_original ON dead_letter_messages(original_message_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_dead_letter_original;
DROP INDEX IF EXISTS idx_dead_letter_tenant;
DROP TABLE IF EXISTS dead_letter_messages;
-- +goose StatementEnd
