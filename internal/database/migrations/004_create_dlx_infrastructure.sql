-- Migration 004: Create Dead Letter Queue infrastructure
-- Table untuk tracking dead letter messages
CREATE TABLE IF NOT EXISTS dead_letter_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    original_message_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    payload JSONB NOT NULL,
    error_reason TEXT,
    retry_count INT NOT NULL DEFAULT 0,
    failed_at TIMESTAMPTZ DEFAULT NOW(),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    CONSTRAINT fk_dead_letter_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

-- Index untuk query DLQ per tenant
CREATE INDEX IF NOT EXISTS idx_dead_letter_tenant_id ON dead_letter_messages(tenant_id);

-- Index untuk retry count
CREATE INDEX IF NOT EXISTS idx_dead_letter_retry_count ON dead_letter_messages(retry_count);

-- Index untuk failed_at (untuk cleanup)
CREATE INDEX IF NOT EXISTS idx_dead_letter_failed_at ON dead_letter_messages(failed_at);

-- Function untuk memindahkan message ke DLQ
CREATE OR REPLACE FUNCTION move_to_dead_letter(
    p_message_id UUID,
    p_tenant_id UUID,
    p_payload JSONB,
    p_error_reason TEXT,
    p_retry_count INT
)
RETURNS VOID AS $$
BEGIN
    INSERT INTO dead_letter_messages (
        original_message_id,
        tenant_id,
        payload,
        error_reason,
        retry_count,
        failed_at
    ) VALUES (
        p_message_id,
        p_tenant_id,
        p_payload,
        p_error_reason,
        p_retry_count,
        NOW()
    );
    
    -- Update original message status
    UPDATE messages 
    SET status = 'failed', 
        updated_at = NOW()
    WHERE id = p_message_id AND tenant_id = p_tenant_id;
END;
$$ LANGUAGE plpgsql;

-- View untuk DLQ summary per tenant
CREATE OR REPLACE VIEW dead_letter_summary AS
SELECT 
    tenant_id,
    COUNT(*) as total_dead_messages,
    MAX(failed_at) as last_failure,
    SUM(CASE WHEN retry_count >= 3 THEN 1 ELSE 0 END) as permanently_failed
FROM dead_letter_messages
GROUP BY tenant_id;
