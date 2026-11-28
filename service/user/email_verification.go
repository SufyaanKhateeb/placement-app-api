package user

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
	"github.com/jackc/pgx/v5"
)

const (
	emailCodeLifetime      = 10 * time.Minute
	emailChallengeLifetime = 20 * time.Minute
)

var (
	errEmailVerificationLimit = errors.New("too many requests for this email. Please wait and try again")
	errEmailChallengeExpired  = errors.New("this verification has expired. Start again to get a new code")
	errEmailCodeInvalid       = errors.New("that code is incorrect")
	errEmailUnavailable       = errors.New("an account already uses this email")
)

type emailChallenge struct {
	email, recipient                  string
	userID                            *int
	firstName, lastName, passwordHash string
	tokenHash, codeDigest             []byte
	expires, deadline                 time.Time
}

func codeDigest(token []byte, code string) []byte {
	mac := hmac.New(sha256.New, token)
	mac.Write([]byte(code))
	return mac.Sum(nil)
}

func newEmailCode() (string, error) {
	number, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", number.Int64()), nil
}

func (s *Store) passwordEmailVerified(ctx context.Context, id int, email string) (bool, error) {
	var verified bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM verified_user_emails WHERE user_id = app_user.id AND email = lower(app_user.email))
		    OR EXISTS (SELECT 1 FROM admin_memberships WHERE user_id = app_user.id)
		FROM app_user WHERE id = $1 AND email = $2
	`, id, email).Scan(&verified)
	return verified, err
}

func registrationAvailable(ctx context.Context, tx pgx.Tx, email string) error {
	if err := access.CheckSignupAddress(ctx, tx, email); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM app_user WHERE lower(email) = $1)
		    OR EXISTS (SELECT 1 FROM admin_user WHERE lower(email) = $1)
	`, email).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return errEmailUnavailable
	}
	return nil
}

func (s *Store) startEmailChallenge(ctx context.Context, challenge emailChallenge) (string, error) {
	// Expired pending registrations no longer need their password hashes or names.
	if _, err := s.db.Exec(ctx, `
		DELETE FROM email_verification_challenges WHERE deadline <= clock_timestamp()
	`); err != nil {
		return "", err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	email, err := access.LockAccountEmail(ctx, tx, challenge.recipient)
	if err != nil {
		return "", err
	}
	if challenge.userID == nil {
		if err := registrationAvailable(ctx, tx, email); err != nil {
			return "", err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO email_verifications (email) VALUES ($1) ON CONFLICT DO NOTHING
	`, email); err != nil {
		return "", err
	}
	now, err := recordEmailSend(ctx, tx, email)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO email_verification_challenges (
		    email, token_hash, code_digest, expires_at, deadline, recipient,
		    user_id, firstname, lastname, password_hash
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (email) DO UPDATE SET token_hash = EXCLUDED.token_hash,
		    code_digest = EXCLUDED.code_digest, expires_at = EXCLUDED.expires_at,
		    deadline = EXCLUDED.deadline, recipient = EXCLUDED.recipient,
		    user_id = EXCLUDED.user_id, firstname = EXCLUDED.firstname,
		    lastname = EXCLUDED.lastname, password_hash = EXCLUDED.password_hash
	`, email, challenge.tokenHash, challenge.codeDigest, now.Add(emailCodeLifetime),
		now.Add(emailChallengeLifetime), challenge.recipient, challenge.userID,
		challenge.firstName, challenge.lastName, challenge.passwordHash)
	if err != nil {
		return "", err
	}
	return challenge.recipient, tx.Commit(ctx)
}

func recordEmailSend(ctx context.Context, tx pgx.Tx, email string) (time.Time, error) {
	var now time.Time
	err := tx.QueryRow(ctx, `
		WITH time AS (SELECT clock_timestamp() AS now)
		UPDATE email_verifications
		SET sent_at = ARRAY(SELECT sent FROM unnest(sent_at) AS sent WHERE sent > now - interval '1 hour') || now
		FROM time WHERE email = $1
		  AND (SELECT count(*) FROM unnest(sent_at) AS sent WHERE sent > now - interval '1 hour') < 3
		  AND NOT EXISTS (SELECT 1 FROM unnest(sent_at) AS sent WHERE sent > now - interval '1 minute')
		RETURNING now
	`, email).Scan(&now)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, errEmailVerificationLimit
	}
	return now, err
}

func lockEmailChallenge(ctx context.Context, tx pgx.Tx, tokenHash []byte) (emailChallenge, error) {
	var email string
	err := tx.QueryRow(ctx, `SELECT email FROM email_verification_challenges WHERE token_hash = $1`, tokenHash).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return emailChallenge{}, errEmailChallengeExpired
	}
	if err != nil {
		return emailChallenge{}, err
	}
	if _, err := access.LockAccountEmail(ctx, tx, email); err != nil {
		return emailChallenge{}, err
	}
	challenge := emailChallenge{email: email}
	err = tx.QueryRow(ctx, `
		SELECT recipient, user_id, firstname, lastname, password_hash, code_digest, expires_at, deadline
		FROM email_verification_challenges
		WHERE email = $1 AND token_hash = $2 AND deadline > clock_timestamp()
		FOR UPDATE
	`, email, tokenHash).Scan(&challenge.recipient, &challenge.userID, &challenge.firstName,
		&challenge.lastName, &challenge.passwordHash, &challenge.codeDigest, &challenge.expires, &challenge.deadline)
	if errors.Is(err, pgx.ErrNoRows) {
		return emailChallenge{}, errEmailChallengeExpired
	}
	return challenge, err
}

func (s *Store) resendEmailChallenge(ctx context.Context, tokenHash, digest []byte) (string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	challenge, err := lockEmailChallenge(ctx, tx, tokenHash)
	if err != nil {
		return "", err
	}
	now, err := recordEmailSend(ctx, tx, challenge.email)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `
		UPDATE email_verification_challenges
		SET code_digest = $2, expires_at = $3, deadline = GREATEST(deadline, $3)
		WHERE email = $1
	`, challenge.email, digest, now.Add(emailCodeLifetime))
	if err != nil {
		return "", err
	}
	return challenge.recipient, tx.Commit(ctx)
}

func (s *Store) confirmEmailChallenge(ctx context.Context, tokenHash, digest []byte) (int, bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)
	challenge, err := lockEmailChallenge(ctx, tx, tokenHash)
	if err != nil {
		return 0, false, err
	}
	var now time.Time
	var failures int
	err = tx.QueryRow(ctx, `
		SELECT now, (SELECT count(*) FROM unnest(failed_at) AS failed WHERE failed > now - interval '15 minutes')
		FROM email_verifications CROSS JOIN (SELECT clock_timestamp() AS now) time WHERE email = $1
	`, challenge.email).Scan(&now, &failures)
	if err != nil {
		return 0, false, err
	}
	if !now.Before(challenge.expires) {
		return 0, false, errEmailChallengeExpired
	}
	if failures >= 5 {
		return 0, false, errEmailVerificationLimit
	}
	if !hmac.Equal(challenge.codeDigest, digest) {
		_, err := tx.Exec(ctx, `
			UPDATE email_verifications
			SET failed_at = ARRAY(SELECT failed FROM unnest(failed_at) AS failed WHERE failed > $2::timestamptz - interval '15 minutes') || $2
			WHERE email = $1
		`, challenge.email, now)
		if err != nil {
			return 0, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, false, err
		}
		return 0, false, errEmailCodeInvalid
	}

	var id int
	if challenge.userID == nil {
		if err := registrationAvailable(ctx, tx, challenge.email); err != nil {
			return 0, false, err
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO app_user (firstname, lastname, email, password)
			VALUES ($1,$2,$3,$4) RETURNING id
		`, challenge.firstName, challenge.lastName, challenge.recipient, challenge.passwordHash).Scan(&id)
	} else {
		err = tx.QueryRow(ctx, `
			SELECT id FROM app_user
			WHERE id = $1 AND email = $2 AND password = $3
			FOR UPDATE
		`, *challenge.userID, challenge.recipient, challenge.passwordHash).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, errEmailChallengeExpired
		}
	}
	if err != nil {
		return 0, false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO verified_user_emails (user_id, email) VALUES ($1,$2)
		ON CONFLICT (user_id) DO UPDATE SET email = EXCLUDED.email
	`, id, challenge.email); err != nil {
		return 0, false, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM email_verification_challenges WHERE email = $1`, challenge.email); err != nil {
		return 0, false, err
	}
	return id, challenge.userID == nil, tx.Commit(ctx)
}

func (s *Store) invalidateEmailCode(ctx context.Context, tokenHash, digest []byte) error {
	_, err := s.db.Exec(ctx, `
		UPDATE email_verification_challenges SET expires_at = clock_timestamp()
		WHERE token_hash = $1 AND code_digest = $2
	`, tokenHash, digest)
	return err
}
