-- Docs (D60): the team's shared drive of documents. People and agents
-- create Markdown documents, HTML pages and uploaded files; every change is
-- a new version and the old ones stay. A doc of a project is seen by that
-- project's agents and the universal ones. Trashed docs stay 30 days.
CREATE TABLE docs (
    id              text PRIMARY KEY,
    title           text NOT NULL,
    kind            text NOT NULL,             -- doc, html, file
    summary         text NOT NULL DEFAULT '',
    folder          text NOT NULL DEFAULT '',
    project_id      text NOT NULL DEFAULT '',
    pinned          boolean NOT NULL DEFAULT false,
    version         integer NOT NULL DEFAULT 1,
    content         text NOT NULL DEFAULT '',  -- the current version's text (a file's extracted text)
    file_id         text NOT NULL DEFAULT '',  -- a file doc's current bytes (files table)
    mime            text NOT NULL DEFAULT '',
    size            integer NOT NULL DEFAULT 0,
    author          text NOT NULL DEFAULT '',
    editor          text NOT NULL DEFAULT '',
    conversation_id text NOT NULL DEFAULT '',
    search          text NOT NULL DEFAULT '',  -- folded title, summary, folder and content
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL,
    deleted_at      timestamptz
);
CREATE INDEX docs_updated ON docs (updated_at DESC);

CREATE TABLE doc_versions (
    doc_id     text NOT NULL REFERENCES docs(id) ON DELETE CASCADE,
    version    integer NOT NULL,
    content    text NOT NULL DEFAULT '',
    file_id    text NOT NULL DEFAULT '',
    mime       text NOT NULL DEFAULT '',
    size       integer NOT NULL DEFAULT 0,
    title      text NOT NULL DEFAULT '',
    note       text NOT NULL DEFAULT '',
    author     text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    PRIMARY KEY (doc_id, version)
);
