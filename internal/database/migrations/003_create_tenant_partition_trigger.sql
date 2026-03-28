-- Migration 003: Create tenant partition trigger and function
CREATE OR REPLACE FUNCTION create_tenant_partition()
RETURNS TRIGGER AS $$
DECLARE
    partition_name TEXT;
    partition_exists BOOLEAN;
BEGIN
    partition_name := 'messages_' || REPLACE(NEW.id::text, '-', '_');
    
    -- Check if partition already exists
    SELECT EXISTS (
        SELECT 1 FROM pg_tables 
        WHERE tablename = partition_name
    ) INTO partition_exists;
    
    IF NOT partition_exists THEN
        -- Create partition for this tenant
        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS %I PARTITION OF messages FOR VALUES IN (%L)',
            partition_name, NEW.id
        );
        
        -- Create indexes on partition
        EXECUTE format(
            'CREATE INDEX IF NOT EXISTS %I ON %I(tenant_id, created_at DESC, id DESC)',
            partition_name || '_cursor_idx', partition_name
        );
        
        EXECUTE format(
            'CREATE INDEX IF NOT EXISTS %I ON %I(status)',
            partition_name || '_status_idx', partition_name
        );
    END IF;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Drop trigger if exists to avoid errors
DROP TRIGGER IF EXISTS tr_create_tenant_partition ON tenants;

-- Create trigger
CREATE TRIGGER tr_create_tenant_partition
    AFTER INSERT ON tenants
    FOR EACH ROW
    EXECUTE FUNCTION create_tenant_partition();

-- Function to drop partition when tenant is deleted
CREATE OR REPLACE FUNCTION drop_tenant_partition()
RETURNS TRIGGER AS $$
DECLARE
    partition_name TEXT;
BEGIN
    partition_name := 'messages_' || REPLACE(OLD.id::text, '-', '_');
    
    -- Check if partition exists before dropping
    IF EXISTS (
        SELECT 1 FROM pg_tables 
        WHERE tablename = partition_name
    ) THEN
        EXECUTE format('DROP TABLE IF EXISTS %I', partition_name);
    END IF;
    
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

-- Drop trigger if exists
DROP TRIGGER IF EXISTS tr_drop_tenant_partition ON tenants;

-- Create trigger
CREATE TRIGGER tr_drop_tenant_partition
    BEFORE DELETE ON tenants
    FOR EACH ROW
    EXECUTE FUNCTION drop_tenant_partition();
