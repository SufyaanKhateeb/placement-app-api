BEGIN;
DROP TABLE job_deliveries;
DROP TABLE job_opportunities;
DELETE FROM access_role_permissions WHERE permission_key = 'jobs.publish';
DELETE FROM access_permissions WHERE key = 'jobs.publish';
COMMIT;
