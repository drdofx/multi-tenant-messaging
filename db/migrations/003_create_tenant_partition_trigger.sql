-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION create_message_partition()
RETURNS TRIGGER AS $$
DECLARE
    partition_name TEXT;
    tenant_id_val UUID;
BEGIN
    tenant_id_val := NEW.tenant_id;
    partition_name := 'messages_' || REPLACE(tenant_id_val::TEXT, '-', '_');
    
    IF NOT EXISTS (
        SELECT 1 FROM pg_class WHERE relname = partition_name
    ) THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF messages FOR VALUES IN (%L)',
            partition_name,
            tenant_id_val
        );
    END IF;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'trigger_create_message_partition'
    ) THEN
        CREATE TRIGGER trigger_create_message_partition
        BEFORE INSERT ON messages
        FOR EACH ROW
        EXECUTE FUNCTION create_message_partition();
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS trigger_create_message_partition ON messages;
DROP FUNCTION IF EXISTS create_message_partition();
-- +goose StatementEnd
