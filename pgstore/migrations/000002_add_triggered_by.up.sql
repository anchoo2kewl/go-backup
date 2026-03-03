ALTER TABLE backup_history
    ADD COLUMN IF NOT EXISTS triggered_by VARCHAR(20) NOT NULL DEFAULT 'manual';
