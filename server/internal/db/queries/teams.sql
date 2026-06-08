-- name: ListTeamsForUser :many
SELECT t.* FROM teams t
JOIN team_members tm ON t.id = tm.team_id
WHERE tm.user_id = $1
ORDER BY t.name;

-- name: ListTeamMembers :many
SELECT u.* FROM users u
JOIN team_members tm ON u.id = tm.user_id
WHERE tm.team_id = $1
ORDER BY u.name;

-- name: GetDefaultTeam :one
SELECT * FROM teams WHERE is_default LIMIT 1;

-- name: AddTeamMember :exec
INSERT INTO team_members (team_id, user_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ListAllTeamsWithMembership :many
-- Browse list for the team-management UI: every team plus its member count
-- and whether the calling user already belongs to it.
SELECT
    t.*,
    (SELECT COUNT(*) FROM team_members tm WHERE tm.team_id = t.id)::int AS member_count,
    EXISTS (
        SELECT 1 FROM team_members me
        WHERE me.team_id = t.id AND me.user_id = @user_id
    ) AS is_member
FROM teams t
ORDER BY t.name;

-- name: GetTeam :one
SELECT * FROM teams WHERE id = $1;

-- name: CreateTeam :one
INSERT INTO teams (name, slug) VALUES ($1, $2) RETURNING *;

-- name: RemoveTeamMember :exec
DELETE FROM team_members WHERE team_id = $1 AND user_id = $2;

-- name: IsTeamMember :one
SELECT EXISTS (
    SELECT 1 FROM team_members WHERE team_id = $1 AND user_id = $2
) AS is_member;
