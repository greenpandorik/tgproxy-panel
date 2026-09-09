-- name: CreateProfile :one
INSERT INTO profiles (node_id, access_key_id, name, secret_enc, backend, carrier_mode, limits)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *;

-- name: ListNodeProfiles :many
SELECT * FROM profiles WHERE node_id = $1 ORDER BY created_at;

-- name: GetNodeProfileByName :one
SELECT * FROM profiles WHERE node_id = $1 AND name = $2;

-- ListNodeProfilesWithKey is ListNodeProfiles plus the columns of the bound access key that
-- the telemt engine has to push to the node - the per-user limits, the expiry, and the status
-- (a revoked key must be served as a disabled user even in the window before its profile row
-- is deleted) - and the key's label, which the node's Profiles tab prints so a row can be
-- read as a person rather than as a uuid. The default profile has no key, so the joined
-- columns are NULL there and the caller reads them as "no limits, never expires, enabled".
-- name: ListNodeProfilesWithKey :many
SELECT p.*, k.telemt_limits AS key_telemt_limits, k.expires_at AS key_expires_at,
       k.status AS key_status, k.label AS key_label
FROM profiles p LEFT JOIN access_keys k ON k.id = p.access_key_id
WHERE p.node_id = $1 ORDER BY p.created_at;

-- name: CountNodeProfiles :one
SELECT count(*) FROM profiles WHERE node_id = $1;

-- name: ListProfilesByKey :many
SELECT * FROM profiles WHERE access_key_id = $1;

-- name: UpdateProfileSecret :exec
UPDATE profiles SET secret_enc = $2, sync_state = 'pending' WHERE id = $1;

-- name: UpdateProfileSettings :exec
UPDATE profiles SET carrier_mode = $2, limits = $3, sync_state = 'pending' WHERE id = $1;

-- name: SetNodeProfilesSync :exec
UPDATE profiles SET sync_state = $2 WHERE node_id = $1;

-- SetProfilesSyncByIDs marks only the profiles an apply actually pushed. Profiles created
-- after the apply's snapshot keep sync_state = 'pending' so ActivatePendingKeysForNode
-- correctly leaves their keys pending.
-- name: SetProfilesSyncByIDs :exec
UPDATE profiles SET sync_state = sqlc.arg('sync_state')
WHERE id = ANY(sqlc.arg('ids')::uuid[]);

-- name: DeleteProfilesByKey :exec
DELETE FROM profiles WHERE access_key_id = $1;

-- name: DeleteProfile :exec
DELETE FROM profiles WHERE id = $1;
