-- The readable text of each document (internal/doctext), which the search
-- matches instead of the raw content, so a search never parses documents.
-- The store writes it on create and on every content save. revision is the
-- document revision the text was worked out from: a row that is missing or
-- behind its document (one written before this table existed, or by an
-- older build) is filled in when the database is opened. It lives in its
-- own table so neither the search nor that check reads past the content.
CREATE TABLE IF NOT EXISTS document_search (
    document_id TEXT PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
    revision    INTEGER NOT NULL,
    text        TEXT NOT NULL
);

-- Lets the open-time check compare revisions from the index alone, without
-- reading each document's content.
CREATE INDEX IF NOT EXISTS idx_documents_revision ON documents(id, revision);
