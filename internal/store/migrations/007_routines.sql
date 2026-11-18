-- Responsabilidades (M1) and rotinas (M5). Responsibilities are part of the
-- versioned agent; a routine is a scheduled prompt one agent runs, delivered
-- in #time or in its own conversation.
ALTER TABLE agents ADD COLUMN responsibilities jsonb NOT NULL DEFAULT '[]';

CREATE TABLE routines (
    id                text PRIMARY KEY,
    agent_id          text NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    responsibility_id text NOT NULL DEFAULT '',
    title             text NOT NULL,
    prompt            text NOT NULL,
    schedule          jsonb NOT NULL,
    deliver           text NOT NULL CHECK (deliver IN ('time', 'conversation')),
    conversation_id   text NOT NULL DEFAULT '',
    enabled           boolean NOT NULL DEFAULT true,
    next_run          timestamptz,
    last_run          timestamptz,
    last_status       text NOT NULL DEFAULT '',
    last_preview      text NOT NULL DEFAULT '',
    failures          integer NOT NULL DEFAULT 0,
    created_at        timestamptz NOT NULL,
    updated_at        timestamptz NOT NULL
);

CREATE INDEX routines_agent_idx ON routines (agent_id, created_at);

-- Starting routines for installs whose team already exists (new installs get
-- them from the server's seedRoutines). The text matches seedRoutines.
INSERT INTO routines (id, agent_id, title, prompt, schedule, deliver, enabled, created_at, updated_at)
SELECT v.id, 'vega', v.title, v.prompt, v.schedule::jsonb, 'time', true, now(), now()
FROM (VALUES
    ('briefing-manha', 'Briefing da manhã', '{"at":"08:00","days":[1,2,3,4,5]}',
     E'Bom dia! Faça o briefing da manhã para o seu dono, em até 8 linhas: o que o time entregou nas últimas 24 h, o que falhou ou travou, as rotinas e responsabilidades que pedem atenção e uma sugestão de foco para hoje.\nSó chame alguém com @ se houver algo concreto para essa pessoa fazer agora.'),
    ('revisao-semanal', 'Revisão semanal', '{"at":"09:00","days":[1]}',
     E'Faça a revisão semanal das responsabilidades do time: para cada funcionário com responsabilidades, diga se está no caminho (ok, atenção ou fora) com base nas checagens da semana, o que travou e uma mudança concreta para a próxima semana.\nTermine com as 3 prioridades da semana. Só chame alguém com @ se houver algo concreto para fazer agora.')
) AS v (id, title, schedule, prompt)
WHERE EXISTS (SELECT 1 FROM agents WHERE id = 'vega')
ON CONFLICT (id) DO NOTHING;
