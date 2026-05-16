-- +goose Up

CREATE TABLE patches (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id           UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    producing_message_id UUID REFERENCES messages(id) ON DELETE SET NULL,
    parent_patch_id      UUID REFERENCES patches(id) ON DELETE SET NULL,
    origin_type          TEXT NOT NULL CHECK (origin_type IN ('agent', 'human', 'system')),
    workspace_sha        TEXT NOT NULL,
    committed_sha        TEXT,
    hunks                JSONB NOT NULL,
    file_count           INT NOT NULL,
    hunk_count           INT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_patches_session_created ON patches(session_id, created_at DESC);
CREATE INDEX idx_patches_parent ON patches(parent_patch_id) WHERE parent_patch_id IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS patches;
