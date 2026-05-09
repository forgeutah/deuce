-- name: ListProjects :many
SELECT * FROM projects ORDER BY name;

-- name: ListProjectsByTeam :many
SELECT * FROM projects WHERE team_id = $1 ORDER BY name;
