-- +goose Up

-- Retire the legacy 'suspended' value before adding the CHECK constraint:
-- existing deployments may carry rows holding 'suspended' from earlier seed
-- data or operator scripts. The reconciler maps a stopped-but-still-present
-- container to 'stopped', which is the natural successor.
UPDATE sessions SET workspace_status = 'stopped'
    WHERE workspace_status = 'suspended';

-- Defense-in-depth: workspace_status was previously unconstrained TEXT, which
-- let typos and stale values flow through. The reconciler and the four
-- lifecycle endpoints write a closed set of eight values; the CHECK pins it.
ALTER TABLE sessions
    ADD CONSTRAINT workspace_status_check
    CHECK (workspace_status IN (
        'starting',
        'ready',
        'stopping',
        'stopped',
        'rebuilding',
        'deleting',
        'missing',
        'failed'
    ));

-- +goose Down

-- Down only drops the constraint. The 'suspended' → 'stopped' UPDATE is not
-- reversed: the original 'suspended' rows are indistinguishable from rows
-- that legitimately reached 'stopped' after this migration ran, and the
-- repo treats migrations as forward-only by convention (see
-- server/internal/db/migrate.go). Operators who need the old value back can
-- update specific rows manually before rolling forward again.
ALTER TABLE sessions
    DROP CONSTRAINT workspace_status_check;
