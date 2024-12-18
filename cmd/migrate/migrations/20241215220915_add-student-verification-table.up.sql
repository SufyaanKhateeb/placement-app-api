CREATE TYPE student_verification_status AS ENUM ('pending', 'approved', 'rejected');
CREATE TABLE IF NOT EXISTS student_verification_request (
    id SERIAL NOT NULL,
    studentId INT NOT NULL, -- Links to the students table
    studentData JSONB NOT NULL, -- Stores form data submitted by the student
    status student_verification_status NOT NULL DEFAULT 'pending', -- Tracks request status
    adminId INT, -- Tracks the admin who handled the request
    createdAt TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updatedAt TIMESTAMP,

    PRIMARY KEY (id),
    FOREIGN KEY (studentId) REFERENCES student_user (id) ON DELETE CASCADE,
    FOREIGN KEY (adminId) REFERENCES admin_user (id) ON DELETE SET NULL
);