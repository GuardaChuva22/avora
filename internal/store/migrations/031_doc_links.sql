-- Wiki links between docs ([[Título]] and [[Título|texto]]). to_id holds
-- the target doc id when resolved in the same folder/space, or '' until a
-- page with that title is created.
CREATE TABLE doc_links (
    from_id  text NOT NULL REFERENCES docs(id) ON DELETE CASCADE,
    to_title text NOT NULL,
    to_id    text NOT NULL DEFAULT '',
    PRIMARY KEY (from_id, to_title)
);
CREATE INDEX doc_links_to_id ON doc_links (to_id) WHERE to_id <> '';
CREATE INDEX doc_links_to_title ON doc_links (lower(to_title));
