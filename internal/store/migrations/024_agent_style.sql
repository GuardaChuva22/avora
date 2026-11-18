-- How an agent talks (D50): voice, how often it joins in #time, emojis
-- and length, as one JSON object.
ALTER TABLE agents ADD COLUMN style jsonb NOT NULL DEFAULT '{}';
