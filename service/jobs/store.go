package jobs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/jackc/pgx/v5"
)

const pageSize = 50

var (
	errInvalidAudience = errors.New("choose verified users or valid audience roles")
	errIncompleteDraft = errors.New("company, title, and description are required to publish")
	errConflict        = errors.New("the job has changed or is already published")
)

type draft struct {
	Company                 string     `json:"company"`
	Title                   string     `json:"title"`
	Description             string     `json:"description"`
	Requirements            string     `json:"requirements"`
	Deadline                *time.Time `json:"deadline,omitempty"`
	Contact                 string     `json:"contact"`
	ApplicationInstructions string     `json:"applicationInstructions"`
}

func (d *draft) normalize() error {
	for _, field := range []struct {
		value *string
		name  string
		limit int
	}{
		{&d.Company, "company", 160}, {&d.Title, "title", 160}, {&d.Description, "description", 10000},
		{&d.Requirements, "requirements", 5000}, {&d.Contact, "contact", 320},
		{&d.ApplicationInstructions, "application instructions", 5000},
	} {
		*field.value = strings.TrimSpace(*field.value)
		if utf8.RuneCountInString(*field.value) > field.limit || strings.ContainsRune(*field.value, 0) {
			return fmt.Errorf("%s exceeds its character limit or contains an invalid character", field.name)
		}
	}
	return nil
}

type opportunity struct {
	ID int64 `json:"id"`
	draft
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
}

type actor struct {
	AccountType string `json:"accountType"`
	AccountID   int    `json:"accountId"`
}

type audience struct {
	VerifiedUsers bool    `json:"verifiedUsers"`
	RoleIDs       []int64 `json:"roleIds"`
}

func (a *audience) normalize() error {
	if len(a.RoleIDs) > 100 {
		return errInvalidAudience
	}
	if a.RoleIDs == nil {
		a.RoleIDs = []int64{}
	}
	slices.Sort(a.RoleIDs)
	a.RoleIDs = slices.Compact(a.RoleIDs)
	if (len(a.RoleIDs) == 0 && !a.VerifiedUsers) || (len(a.RoleIDs) > 0 && a.RoleIDs[0] <= 0) {
		return errInvalidAudience
	}
	return nil
}

type adminOpportunity struct {
	opportunity
	Revision       int       `json:"revision"`
	CreatedAt      time.Time `json:"createdAt"`
	PublishedBy    *actor    `json:"publishedBy,omitempty"`
	Audience       *audience `json:"audience,omitempty"`
	RecipientCount int64     `json:"recipientCount"`
}

type page[T opportunity | adminOpportunity] struct {
	Items      []T   `json:"items"`
	NextBefore int64 `json:"nextBefore,omitempty"`
}

const visibleColumns = `j.id, j.company, j.title, j.description, j.requirements, j.deadline,
	j.contact, j.application_instructions, j.published_at`
const adminColumns = visibleColumns + `, j.revision, j.created_at, j.publisher_type, j.publisher_id,
	j.verified_audience, j.audience_role_ids, j.recipient_count`

func scanOpportunity(row pgx.Row) (opportunity, error) {
	var j opportunity
	err := row.Scan(j.destinations()...)
	return j, err
}

func (j *opportunity) destinations() []any {
	return []any{&j.ID, &j.Company, &j.Title, &j.Description, &j.Requirements,
		&j.Deadline, &j.Contact, &j.ApplicationInstructions, &j.PublishedAt}
}

func scanAdmin(row pgx.Row) (adminOpportunity, error) {
	var j adminOpportunity
	var publisherType *string
	var publisherID *int
	var target audience
	err := row.Scan(append(j.destinations(), &j.Revision, &j.CreatedAt, &publisherType, &publisherID,
		&target.VerifiedUsers, &target.RoleIDs, &j.RecipientCount)...)
	if err == nil && j.PublishedAt != nil {
		j.PublishedBy = &actor{AccountType: *publisherType, AccountID: *publisherID}
		j.Audience = &target
	}
	return j, err
}

func createDraft(ctx context.Context, tx pgx.Tx, input draft) (adminOpportunity, error) {
	return scanAdmin(tx.QueryRow(ctx, `
		INSERT INTO job_opportunities AS j (company, title, description, requirements, deadline, contact, application_instructions)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING `+adminColumns,
		input.Company, input.Title, input.Description, input.Requirements, input.Deadline, input.Contact, input.ApplicationInstructions))
}

func updateDraft(ctx context.Context, tx pgx.Tx, id int64, revision int, input draft) (adminOpportunity, error) {
	current, err := lockOpportunity(ctx, tx, id)
	if err != nil {
		return current, err
	}
	if current.PublishedAt != nil || current.Revision != revision {
		return current, errConflict
	}
	return scanAdmin(tx.QueryRow(ctx, `
		UPDATE job_opportunities AS j
		SET company=$2, title=$3, description=$4, requirements=$5, deadline=$6, contact=$7,
		    application_instructions=$8, revision=revision+1
		WHERE id=$1 RETURNING `+adminColumns,
		id, input.Company, input.Title, input.Description, input.Requirements, input.Deadline, input.Contact, input.ApplicationInstructions))
}

func lockOpportunity(ctx context.Context, tx pgx.Tx, id int64) (adminOpportunity, error) {
	return scanAdmin(tx.QueryRow(ctx, "SELECT "+adminColumns+" FROM job_opportunities j WHERE j.id=$1 FOR UPDATE", id))
}

func publish(ctx context.Context, tx pgx.Tx, id int64, revision int, target audience, publisher types.TokenInput) (adminOpportunity, error) {
	current, err := lockOpportunity(ctx, tx, id)
	if err != nil {
		return current, err
	}
	if current.Revision != revision {
		return current, errConflict
	}
	if current.PublishedAt != nil {
		if current.Audience.VerifiedUsers == target.VerifiedUsers && slices.Equal(current.Audience.RoleIDs, target.RoleIDs) {
			return current, nil
		}
		return current, errConflict
	}
	if current.Company == "" || current.Title == "" || current.Description == "" {
		return current, errIncompleteDraft
	}
	var roleCount int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM access_roles WHERE id=ANY($1::bigint[])", target.RoleIDs).Scan(&roleCount); err != nil {
		return current, err
	}
	if roleCount != len(target.RoleIDs) {
		return current, errInvalidAudience
	}

	// A single statement snapshots the union of both audience selectors.
	deliveries, err := tx.Exec(ctx, `
		INSERT INTO job_deliveries (user_id, job_id)
		SELECT recipients.user_id, $1
		FROM (
			SELECT id AS user_id FROM app_user WHERE $2 AND verified
			UNION
			SELECT sub.user_id FROM form_verifications v
			JOIN form_submissions sub ON sub.id=v.submission_id WHERE $2
			UNION
			SELECT account_id FROM access_assignments WHERE account_type='user' AND role_id=ANY($3::bigint[])
			UNION
			SELECT user_id FROM (`+access.UserApprovalRoleGrantsSQL+`) grants WHERE role_id=ANY($3::bigint[])
		) recipients
		ON CONFLICT (user_id, job_id) DO NOTHING
	`, id, target.VerifiedUsers, target.RoleIDs)
	if err != nil {
		return current, err
	}
	current, err = scanAdmin(tx.QueryRow(ctx, `
		UPDATE job_opportunities AS j
		SET published_at=clock_timestamp(), publisher_type=$2, publisher_id=$3,
		    verified_audience=$4, audience_role_ids=$5, recipient_count=$6
		WHERE id=$1 RETURNING `+adminColumns,
		id, publisher.Type, publisher.Id, target.VerifiedUsers, target.RoleIDs, deliveries.RowsAffected()))
	if err != nil {
		return current, err
	}
	err = access.Audit(ctx, tx, publisher, "job.published", map[string]any{
		"jobId": id, "audience": target, "recipientCount": current.RecipientCount,
	})
	return current, err
}
