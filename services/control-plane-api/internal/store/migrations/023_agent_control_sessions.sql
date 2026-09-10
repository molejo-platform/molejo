-- +goose Up
ALTER TABLE agent_installations
    ADD COLUMN control_session_id TEXT,
    ADD COLUMN control_session_sequence BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT agent_installations_control_session_valid CHECK (
        (control_session_id IS NULL AND control_session_sequence = 0)
        OR (control_session_id IS NOT NULL AND char_length(control_session_id) BETWEEN 1 AND 80 AND control_session_sequence >= 0)
    );

-- +goose Down
ALTER TABLE agent_installations
    DROP CONSTRAINT IF EXISTS agent_installations_control_session_valid,
    DROP COLUMN IF EXISTS control_session_sequence,
    DROP COLUMN IF EXISTS control_session_id;
