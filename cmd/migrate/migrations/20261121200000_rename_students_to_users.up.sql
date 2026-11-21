BEGIN;

ALTER TABLE public.student_user RENAME TO app_user;
ALTER TABLE public.app_user RENAME CONSTRAINT student_user_pkey TO app_user_pkey;
ALTER TABLE public.app_user RENAME CONSTRAINT student_user_email_key TO app_user_email_key;
ALTER SEQUENCE public.student_user_id_seq RENAME TO app_user_id_seq;

ALTER TYPE public.student_verification_status RENAME TO user_verification_status;
ALTER TABLE public.student_verification_request RENAME TO user_verification_request;
ALTER TABLE public.user_verification_request RENAME COLUMN studentid TO user_id;
ALTER TABLE public.user_verification_request RENAME COLUMN studentdata TO user_data;
ALTER TABLE public.user_verification_request RENAME CONSTRAINT student_verification_request_pkey TO user_verification_request_pkey;
ALTER TABLE public.user_verification_request RENAME CONSTRAINT student_verification_request_studentid_fkey TO user_verification_request_user_id_fkey;
ALTER TABLE public.user_verification_request RENAME CONSTRAINT student_verification_request_adminid_fkey TO user_verification_request_adminid_fkey;
ALTER SEQUENCE public.student_verification_request_id_seq RENAME TO user_verification_request_id_seq;

ALTER TABLE public.oidc_identity RENAME COLUMN student_id TO user_id;
ALTER TABLE public.oidc_identity RENAME CONSTRAINT oidc_identity_student_id_fkey TO oidc_identity_user_id_fkey;
ALTER INDEX public.oidc_identity_student_id_idx RENAME TO oidc_identity_user_id_idx;

COMMIT;