CREATE TABLE oidc_identity (
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    student_id INTEGER REFERENCES student_user(id) ON DELETE CASCADE,
    admin_id INTEGER REFERENCES admin_user(id) ON DELETE CASCADE,
    PRIMARY KEY (issuer, subject),
    CHECK (num_nonnulls(student_id, admin_id) = 1)
);

CREATE INDEX oidc_identity_student_id_idx ON oidc_identity (student_id) WHERE student_id IS NOT NULL;
CREATE INDEX oidc_identity_admin_id_idx ON oidc_identity (admin_id) WHERE admin_id IS NOT NULL;