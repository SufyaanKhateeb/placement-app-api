ALTER TABLE form_submissions
    DROP CONSTRAINT form_submissions_requested_changes_check,
    DROP COLUMN requested_changes;
