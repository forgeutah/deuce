-- +goose Up
ALTER TABLE agents ADD COLUMN system_prompt TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE agents ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE agents ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Add claude_session_id to session_agents for agent conversation continuity
ALTER TABLE session_agents ADD COLUMN claude_session_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE session_agents DROP COLUMN claude_session_id;
ALTER TABLE agents DROP COLUMN updated_at;
ALTER TABLE agents DROP COLUMN created_at;
ALTER TABLE agents DROP COLUMN deleted_at;
ALTER TABLE agents DROP COLUMN system_prompt;
