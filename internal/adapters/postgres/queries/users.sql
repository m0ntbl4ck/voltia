-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- Storing the same email again refreshes the name and the password hash, so a
-- password changed in the environment takes effect on the next start.
-- name: UpsertUser :one
INSERT INTO users (email, password_hash, name)
VALUES ($1, $2, $3)
ON CONFLICT (email) DO UPDATE SET
    password_hash = EXCLUDED.password_hash,
    name          = EXCLUDED.name
RETURNING *;
