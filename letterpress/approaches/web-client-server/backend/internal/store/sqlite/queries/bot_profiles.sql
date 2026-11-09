-- name: GetBotProfile :one
SELECT player_id, configuration_version, style,
       english_knowledge_level, skill_level, enabled, created_at
FROM bot_profiles
WHERE player_id = ? AND configuration_version = ?;

-- name: GetLatestEnabledBotProfile :one
SELECT p.id, p.display_name, p.normalized_name, p.player_type, p.created_at,
       b.configuration_version, b.style, b.english_knowledge_level,
       b.skill_level, b.enabled, b.created_at AS profile_created_at
FROM registered_players AS p
JOIN bot_profiles AS b ON b.player_id = p.id
WHERE p.id = ? AND p.player_type = 'bot' AND b.enabled = 1
ORDER BY b.configuration_version DESC
LIMIT 1;

-- name: ListLatestEnabledBotProfiles :many
SELECT p.id, p.display_name, p.normalized_name, p.player_type, p.created_at,
       b.configuration_version, b.style, b.english_knowledge_level,
       b.skill_level, b.enabled, b.created_at AS profile_created_at
FROM registered_players AS p
JOIN bot_profiles AS b ON b.player_id = p.id
WHERE p.player_type = 'bot' AND b.enabled = 1
  AND b.configuration_version = (
      SELECT MAX(latest.configuration_version)
      FROM bot_profiles AS latest
      WHERE latest.player_id = p.id AND latest.enabled = 1
  )
ORDER BY p.id;

-- name: CreateBotProfileVersion :exec
INSERT INTO bot_profiles (
    player_id, configuration_version, style,
    english_knowledge_level, skill_level, enabled, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: CreateBotRuntimeCredential :exec
INSERT INTO bot_runtime_credentials (token_hash, player_id, created_at)
VALUES (?, ?, ?);

-- name: GetBotByRuntimeCredentialHash :one
SELECT p.id, p.display_name, p.normalized_name, p.player_type, p.created_at
FROM bot_runtime_credentials AS c
JOIN registered_players AS p ON p.id = c.player_id
WHERE c.token_hash = ? AND p.player_type = 'bot';
