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
