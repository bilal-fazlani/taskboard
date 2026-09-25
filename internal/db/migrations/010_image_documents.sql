-- Images as documents: png, jpeg, gif and webp join markdown and html. An
-- image's details live on its documents row like any document's, with its
-- size in bytes and its width and height in pixels, which text documents
-- leave NULL (their size is their content's length). Its content stays
-- empty: the file and its thumbnail live in document_images, so listing,
-- counting and searching documents never reads an image's bytes.
--
-- The format CHECK can only change by rebuilding the table. Dropping
-- documents would cascade into document_search, so the search text is set
-- aside first and put back after. The rebuilt table puts content last, so
-- reading any other column never walks a long text's overflow pages.
-- Rows whose document is gone (possible only if foreign keys were ever off)
-- are left behind, since the rebuilt table's foreign key would refuse them.
CREATE TABLE document_search_saved AS SELECT document_id, revision, text FROM document_search
    WHERE document_id IN (SELECT id FROM documents);
DROP TABLE document_search;

CREATE TABLE documents_new (
    id         TEXT PRIMARY KEY,
    ticket_id  TEXT REFERENCES tickets(id) ON DELETE CASCADE,
    epic_id    TEXT REFERENCES epics(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    format     TEXT NOT NULL CHECK (format IN ('markdown', 'html', 'png', 'jpeg', 'gif', 'webp')),
    revision   INTEGER NOT NULL DEFAULT 1,
    size       INTEGER,
    width      INTEGER,
    height     INTEGER,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    CHECK ((ticket_id IS NULL) != (epic_id IS NULL)),
    CHECK (CASE WHEN format IN ('png', 'jpeg', 'gif', 'webp')
        THEN size IS NOT NULL AND width > 0 AND height > 0 AND content = ''
        ELSE size IS NULL AND width IS NULL AND height IS NULL END)
);

INSERT INTO documents_new (id, ticket_id, epic_id, name, format, revision, created_at, updated_at, content)
    SELECT id, ticket_id, epic_id, name, format, revision, created_at, updated_at, content FROM documents;
DROP TABLE documents;
ALTER TABLE documents_new RENAME TO documents;

CREATE INDEX idx_documents_ticket_id ON documents(ticket_id);
CREATE INDEX idx_documents_epic_id ON documents(epic_id);
CREATE UNIQUE INDEX idx_documents_ticket_name
    ON documents(ticket_id, name COLLATE NOCASE) WHERE ticket_id IS NOT NULL;
CREATE UNIQUE INDEX idx_documents_epic_name
    ON documents(epic_id, name COLLATE NOCASE) WHERE epic_id IS NOT NULL;
-- The open-time search check (fillDocumentSearch) compares revisions and
-- skips images from this index alone.
CREATE INDEX idx_documents_revision ON documents(id, revision, format);

CREATE TABLE document_search (
    document_id TEXT PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
    revision    INTEGER NOT NULL,
    text        TEXT NOT NULL
);
INSERT INTO document_search (document_id, revision, text)
    SELECT document_id, revision, text FROM document_search_saved;
DROP TABLE document_search_saved;

-- An image's file, with its metadata removed, and its thumbnail. The
-- thumbnail comes first so reading it never walks the file's overflow pages.
CREATE TABLE document_images (
    document_id    TEXT PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
    thumbnail_type TEXT NOT NULL CHECK (thumbnail_type IN ('image/png', 'image/jpeg')),
    thumbnail      BLOB NOT NULL,
    data           BLOB NOT NULL
);
