-- Migration 002: Create partitioned messages table
CREATE TABLE IF NOT EXISTS messages (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    retry_count INT NOT NULL DEFAULT 0 CHECK (retry_count >= 0 AND retry_count <= 3),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    CONSTRAINT fk_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
) PARTITION BY LIST (tenant_id);

-- Composite index for cursor pagination
CREATE INDEX IF NOT EXISTS idx_messages_cursor ON messages(tenant_id, created_at DESC, id DESC);

-- Status index for filtering
CREATE INDEX IF NOT EXISTS idx_messages_status ON messages(status);

-- Trigger for updated_at
CREATE TRIGGER tr_messages_updated_at
    BEFORE UPDATE ON messages
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
