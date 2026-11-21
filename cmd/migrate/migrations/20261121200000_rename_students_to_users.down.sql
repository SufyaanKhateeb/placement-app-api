BEGIN;

ALTER INDEX public.oidc_identity_user_id_idx RENAME TO oidc_identity_student_id_idx;
ALTER TABLE public.oidc_identity RENAME CONSTRAINT oidc_identity_user_id_fkey TO oidc_identity_student_id_fkey;
ALTER TABLE public.oidc_identity RENAME COLUMN user_id TO student_id;

ALTER SEQUENCE public.user_verification_request_id_seq RENAME TO student_verification_request_id_seq;
ALTER TABLE public.user_verification_request RENAME CONSTRAINT user_verification_request_adminid_fkey TO student_verification_request_adminid_fkey;
ALTER TABLE public.user_verification_request RENAME CONSTRAINT user_verification_request_user_id_fkey TO student_verification_request_studentid_fkey;
ALTER TABLE public.user_verification_request RENAME CONSTRAINT user_verification_request_pkey TO student_verification_request_pkey;
ALTER TABLE public.user_verification_request RENAME COLUMN user_data TO studentdata;
ALTER TABLE public.user_verification_request RENAME COLUMN user_id TO studentid;
ALTER TABLE public.user_verification_request RENAME TO student_verification_request;
ALTER TYPE public.user_verification_status RENAME TO student_verification_status;

ALTER SEQUENCE public.app_user_id_seq RENAME TO student_user_id_seq;
ALTER TABLE public.app_user RENAME CONSTRAINT app_user_email_key TO student_user_email_key;
ALTER TABLE public.app_user RENAME CONSTRAINT app_user_pkey TO student_user_pkey;
ALTER TABLE public.app_user RENAME TO student_user;

COMMIT;