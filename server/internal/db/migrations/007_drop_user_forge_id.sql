-- +goose Up

-- Drop seed users so a real proxy-auth user with the same email does not
-- silently inherit a seed account's UUID, sessions, and team memberships.
-- The seed dataset (002_seed_data.sql) shipped before proxy auth and used
-- real-shaped emails (clint@forge.dev, alex@acme.co, ...) — with email as
-- the new lookup key, those rows are a footgun. Cascades drop the related
-- team_members and session_members rows; sessions/messages/agents stay.
DELETE FROM users
    WHERE id IN (
        '10000000-0000-0000-0000-000000000001',
        '10000000-0000-0000-0000-000000000002',
        '10000000-0000-0000-0000-000000000003',
        '10000000-0000-0000-0000-000000000004',
        '10000000-0000-0000-0000-000000000005'
    );

ALTER TABLE users
    DROP COLUMN forge_first_seen_at,
    DROP COLUMN forge_user_id;

-- +goose Down
-- Restores the schema shape from migration 006 and re-inserts the seed
-- users from 002 so a Down→Up cycle returns to a known dev state.
ALTER TABLE users
    ADD COLUMN forge_user_id BIGINT UNIQUE,
    ADD COLUMN forge_first_seen_at TIMESTAMPTZ;

INSERT INTO users (id, name, email, avatar, status) VALUES
    ('10000000-0000-0000-0000-000000000001', 'Clint Berry',    'clint@forge.dev',  'https://api.dicebear.com/9.x/avataaars/svg?seed=Clint',  'online'),
    ('10000000-0000-0000-0000-000000000002', 'Sarah Chen',     'sarah@forge.dev',  'https://api.dicebear.com/9.x/avataaars/svg?seed=Sarah',  'online'),
    ('10000000-0000-0000-0000-000000000003', 'Mike Rodriguez', 'mike@forge.dev',   'https://api.dicebear.com/9.x/avataaars/svg?seed=Mike',   'offline'),
    ('10000000-0000-0000-0000-000000000004', 'Alex Park',      'alex@acme.co',     'https://api.dicebear.com/9.x/avataaars/svg?seed=Alex',   'online'),
    ('10000000-0000-0000-0000-000000000005', 'Jordan Lee',     'jordan@acme.co',   'https://api.dicebear.com/9.x/avataaars/svg?seed=Jordan', 'offline');

INSERT INTO team_members (team_id, user_id) VALUES
    ('20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001'),
    ('20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002'),
    ('20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000003'),
    ('20000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000004'),
    ('20000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000005');

INSERT INTO session_members (session_id, user_id) VALUES
    ('40000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001'),
    ('40000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002'),
    ('40000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001'),
    ('40000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000003'),
    ('40000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001'),
    ('40000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000002'),
    ('40000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000003'),
    ('40000000-0000-0000-0000-000000000005', '10000000-0000-0000-0000-000000000001'),
    ('40000000-0000-0000-0000-000000000005', '10000000-0000-0000-0000-000000000002'),
    ('40000000-0000-0000-0000-000000000006', '10000000-0000-0000-0000-000000000004'),
    ('40000000-0000-0000-0000-000000000006', '10000000-0000-0000-0000-000000000005');
