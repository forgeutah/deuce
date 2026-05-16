-- +goose Up

-- Agent presets
INSERT INTO agents (id, name, role, color, color_muted, provider, model, description) VALUES
    ('00000000-0000-0000-0000-000000000001', 'Coder',    'coder',    '#58a6ff', '#0c2d6b', 'Anthropic', 'Claude Sonnet 4', 'Writes and modifies code'),
    ('00000000-0000-0000-0000-000000000002', 'Reviewer',  'reviewer', '#BE8FFF', '#3c1e70', 'Anthropic', 'Claude Sonnet 4', 'Reviews code changes'),
    ('00000000-0000-0000-0000-000000000003', 'Planner',   'planner',  '#3fb950', '#033a16', 'OpenAI',    'GPT-4o',          'Creates implementation plans'),
    ('00000000-0000-0000-0000-000000000004', 'Tester',    'tester',   '#d29922', '#4b2900', 'Anthropic', 'Claude Sonnet 4', 'Writes and runs tests'),
    ('00000000-0000-0000-0000-000000000005', 'Designer',  'designer', '#f778ba', '#5e103e', 'OpenAI',    'GPT-4o',          'UI/UX suggestions');

-- Users
INSERT INTO users (id, name, email, avatar, status) VALUES
    ('10000000-0000-0000-0000-000000000001', 'Clint Berry',    'clint@forge.dev',  'https://api.dicebear.com/9.x/avataaars/svg?seed=Clint',  'online'),
    ('10000000-0000-0000-0000-000000000002', 'Sarah Chen',     'sarah@forge.dev',  'https://api.dicebear.com/9.x/avataaars/svg?seed=Sarah',  'online'),
    ('10000000-0000-0000-0000-000000000003', 'Mike Rodriguez', 'mike@forge.dev',   'https://api.dicebear.com/9.x/avataaars/svg?seed=Mike',   'offline'),
    ('10000000-0000-0000-0000-000000000004', 'Alex Park',      'alex@acme.co',     'https://api.dicebear.com/9.x/avataaars/svg?seed=Alex',   'online'),
    ('10000000-0000-0000-0000-000000000005', 'Jordan Lee',     'jordan@acme.co',   'https://api.dicebear.com/9.x/avataaars/svg?seed=Jordan', 'offline');

-- Teams
INSERT INTO teams (id, name, slug) VALUES
    ('20000000-0000-0000-0000-000000000001', 'Forge Utah', 'forge-utah'),
    ('20000000-0000-0000-0000-000000000002', 'Acme Corp',  'acme-corp');

-- Team members
INSERT INTO team_members (team_id, user_id) VALUES
    ('20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001'),
    ('20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002'),
    ('20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000003'),
    ('20000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000004'),
    ('20000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000005');

-- Projects
INSERT INTO projects (id, name, repo_url, team_id) VALUES
    ('30000000-0000-0000-0000-000000000001', 'forge-api',        'https://github.com/forgeutah/forge-api',  '20000000-0000-0000-0000-000000000001'),
    ('30000000-0000-0000-0000-000000000002', 'forge-web',        'https://github.com/forgeutah/forge-web',  '20000000-0000-0000-0000-000000000001'),
    ('30000000-0000-0000-0000-000000000003', 'acme-dashboard',   'https://github.com/acmecorp/dashboard',   '20000000-0000-0000-0000-000000000002');

-- Sessions
INSERT INTO sessions (id, name, project_id, status, workspace_status, plan_content, last_activity_at) VALUES
    ('40000000-0000-0000-0000-000000000001', 'auth-module',        '30000000-0000-0000-0000-000000000001', 'active',   'ready',     E'# Auth Module Plan\n\n## Goals\n- [ ] Implement JWT token validation\n- [ ] Add token expiration checks\n- [x] Set up auth middleware\n- [x] Create user model\n\n## Technical Notes\n- Using `golang-jwt/jwt/v5` for JWT parsing\n- Token expiry window: 24 hours\n- Refresh tokens stored in Redis', now() - interval '5 minutes'),
    ('40000000-0000-0000-0000-000000000002', 'api-rate-limiting',  '30000000-0000-0000-0000-000000000001', 'active',   'ready',     E'# Rate Limiting Plan\n\n## Approach\n- Token bucket algorithm\n- Per-user rate limits via Redis', now() - interval '2 hours'),
    ('40000000-0000-0000-0000-000000000003', 'homepage-redesign',  '30000000-0000-0000-0000-000000000002', 'active',   'ready',     '', now() - interval '30 minutes'),
    ('40000000-0000-0000-0000-000000000004', 'ci-pipeline',        '30000000-0000-0000-0000-000000000001', 'paused',   'suspended', '', now() - interval '2 days'),
    ('40000000-0000-0000-0000-000000000005', 'onboarding-flow',    '30000000-0000-0000-0000-000000000002', 'archived', 'suspended', '', now() - interval '7 days'),
    ('40000000-0000-0000-0000-000000000006', 'dashboard-charts',   '30000000-0000-0000-0000-000000000003', 'active',   'starting',  '', now() - interval '15 minutes');

-- Session members
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

-- Session agents
INSERT INTO session_agents (session_id, agent_id) VALUES
    ('40000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001'),
    ('40000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002'),
    ('40000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000004'),
    ('40000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001'),
    ('40000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000003'),
    ('40000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001'),
    ('40000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000005'),
    ('40000000-0000-0000-0000-000000000004', '00000000-0000-0000-0000-000000000001'),
    ('40000000-0000-0000-0000-000000000005', '00000000-0000-0000-0000-000000000001'),
    ('40000000-0000-0000-0000-000000000005', '00000000-0000-0000-0000-000000000003'),
    ('40000000-0000-0000-0000-000000000006', '00000000-0000-0000-0000-000000000001'),
    ('40000000-0000-0000-0000-000000000006', '00000000-0000-0000-0000-000000000002'),
    ('40000000-0000-0000-0000-000000000006', '00000000-0000-0000-0000-000000000004');

-- Sample messages for auth-module session
-- Agent messages get explicit IDs (60... prefix) so seed patches in 008 can
-- anchor producing_message_id to them deterministically.
INSERT INTO messages (id, session_id, author_id, author_type, content, created_at) VALUES
    ('60000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', 'human', 'Let''s start working on the auth module. We need JWT validation with token expiration checking.', now() - interval '4 hours'),
    ('60000000-0000-0000-0000-000000000002', '40000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002', 'human', 'Agreed. I''ve updated the plan with acceptance criteria. @Planner can you break this down?', now() - interval '3 hours 30 minutes'),
    ('60000000-0000-0000-0000-000000000003', '40000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'agent', 'I''ve set up the auth middleware and user model. The base structure is ready for JWT integration.', now() - interval '3 hours'),
    ('60000000-0000-0000-0000-000000000004', '40000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', 'human', '@Coder now add token expiration checking to the Validate function', now() - interval '2 hours'),
    ('60000000-0000-0000-0000-000000000005', '40000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'agent', 'I''ve updated the Validate function with token expiration checking. The implementation parses JWT claims and checks the expiry timestamp.', now() - interval '1 hour 30 minutes'),
    ('60000000-0000-0000-0000-000000000006', '40000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', 'human', '@Tester can you write tests for the new expiration logic?', now() - interval '1 hour'),
    ('60000000-0000-0000-0000-000000000007', '40000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000004', 'agent', 'Tests are written and passing. I''ve covered valid tokens, expired tokens, invalid format, and empty input.', now() - interval '45 minutes'),
    ('60000000-0000-0000-0000-000000000008', '40000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002', 'human', '@Reviewer can you review all the auth changes?', now() - interval '20 minutes'),
    ('60000000-0000-0000-0000-000000000009', '40000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002', 'agent', E'Code looks good overall. Two suggestions:\n\n1. Consider using `errors.Is()` for error comparison in middleware\n2. Add context to the JWT parsing errors for better debugging\n\nThe token expiration logic is correctly implemented. Tests cover the main cases well.', now() - interval '10 minutes');

-- Sample messages for api-rate-limiting session
INSERT INTO messages (session_id, author_id, author_type, content, created_at) VALUES
    ('40000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', 'human', 'We need rate limiting on the API endpoints. @Planner can you outline an approach?', now() - interval '6 hours'),
    ('40000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000003', 'agent', E'Here''s my recommended approach for rate limiting:\n\n**Algorithm:** Token bucket (allows bursts while maintaining average rate)\n\n**Storage:** Redis for distributed rate limit state\n\n**Configuration:**\n- Default: 100 requests/minute per user\n- Auth endpoints: 10 requests/minute (stricter)\n- Public endpoints: 30 requests/minute per IP', now() - interval '5 hours 30 minutes'),
    ('40000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000003', 'human', 'Looks good. Make sure we add X-RateLimit headers in the response.', now() - interval '5 hours');

-- Activity items for auth-module session
INSERT INTO activity_items (session_id, type, description, agent_id, metadata, created_at) VALUES
    ('40000000-0000-0000-0000-000000000001', 'agent-action', 'Reviewer completed code review',        '00000000-0000-0000-0000-000000000002', NULL, now() - interval '10 minutes'),
    ('40000000-0000-0000-0000-000000000001', 'test-run',     '4/4 tests passing',                      '00000000-0000-0000-0000-000000000004', NULL, now() - interval '45 minutes'),
    ('40000000-0000-0000-0000-000000000001', 'file-change',  'validate.go',                             '00000000-0000-0000-0000-000000000001', '{"additions": "12", "deletions": "3"}', now() - interval '1 hour 30 minutes'),
    ('40000000-0000-0000-0000-000000000001', 'file-change',  'middleware.go',                            '00000000-0000-0000-0000-000000000001', '{"additions": "45", "deletions": "0"}', now() - interval '3 hours'),
    ('40000000-0000-0000-0000-000000000001', 'commit',       'a1b2c3d Add token expiration check',       NULL, NULL, now() - interval '1 hour');

-- +goose Down
DELETE FROM activity_items;
DELETE FROM messages;
DELETE FROM session_agents;
DELETE FROM session_members;
DELETE FROM sessions;
DELETE FROM projects;
DELETE FROM team_members;
DELETE FROM teams;
DELETE FROM users;
DELETE FROM agents;
