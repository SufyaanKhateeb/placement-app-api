BEGIN;
ALTER TABLE forms DROP CONSTRAINT forms_published_version_fkey;
DROP TABLE form_versions;
DROP TABLE forms;
COMMIT;