-- agent.run jobs carry their spec (engine, prompt, mode, workdir, time limit).
ALTER TABLE jobs ADD COLUMN agent jsonb;
