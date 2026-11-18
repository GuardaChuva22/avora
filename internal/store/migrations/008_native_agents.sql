-- Every agent thinks with the native model; Claude Code and Codex became
-- tools the agents delegate to. Old values stay in agent_versions.
UPDATE agents SET engine = 'native', model = '', mode = 'read'
WHERE engine <> 'native' OR model <> '' OR mode <> 'read';
