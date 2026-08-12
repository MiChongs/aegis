-- +migrate Down
DROP INDEX IF EXISTS idx_legal_documents_type_published;
DROP TABLE IF EXISTS legal_documents;
