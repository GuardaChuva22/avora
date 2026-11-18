-- 034_home_prefs: owner profile read by every agent and per-person Home visit timestamp.
ALTER TABLE people ADD COLUMN IF NOT EXISTS about text NOT NULL DEFAULT '';
ALTER TABLE people ADD COLUMN IF NOT EXISTS last_home_at timestamptz;
