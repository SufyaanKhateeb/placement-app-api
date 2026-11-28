# Placement App API v1

All paths below are relative to `/api/v1`. Authenticated requests use the existing access and refresh cookies. The API resolves permissions for the current session on each protected request. Clients should send JSON on JSON write routes and include credentials on cross-origin requests from the configured frontend.

## Old and new API comparison

| Operation | Legacy contract | Canonical contract |
| --- | --- | --- |
| Password registration | `POST /register` → `202` pending verification | `POST /users` → `202` pending verification |
| Password sign-in | `POST /login` or `POST /login-admin` → `200` with JSON `null` and cookies, or `202` pending verification. Rejected credentials return `400`. | `POST /auth/sessions` with `{email,password,mode}`, where `mode` is `user` or `admin`. Returns empty `204` with cookies for completed sign-in or `202` for pending verification. Rejected credentials return `401 invalid_credentials`. |
| Read profile | `GET /user` → `200` profile | `GET /users/me` → `200` with the same profile, including `accountType`, `role`, `uType`, and permissions |
| Renew session | `POST /refresh` → `200` profile, with a new access cookie when renewing from the refresh cookie | `POST /auth/sessions/current/renewals` → same status, representation, and cookie rules |
| End session | `POST /logout` → `202` JSON `null` and clears cookies | `DELETE /auth/sessions/current` → empty `204` and clears cookies |
| Confirm email | `POST /auth/email-verification/confirm` with `{code}` → `200` with session cookies | `POST /auth/email-verifications/current/confirmations` → `201` with `Location: /api/v1/users/me` and cookies for account creation, or empty `204` with cookies for an existing account |
| Resend email code | `POST /auth/email-verification/resend` → `202` | `POST /auth/email-verifications/current/deliveries` → `202`. The pending response has `verificationRequired: true` and includes `deliveryWarning` if delivery failed. Legacy pending responses retain the `error` warning field. |
| Invitation-based registration | `POST /auth/admin-invitation/register` → `201` with cookies | Same route and response with `Location: /api/v1/users/me` |
| Create or read a role | `POST /admin/access/roles` → `201` with `{id}`. Roles are listed in `GET /admin/access`. | The create response also supplies `Location: /api/v1/admin/access/roles/{role}`. Follow with `GET /admin/access/roles/{role}` for the role representation. Both operations require Manage Roles. |
| Promote a user | `POST /admin/access/accounts/user/{id}/promote` with `{email}` → `204` | `PATCH /admin/access/accounts/user/{id}` with `{admin:true,email}` → `204`. Requires Promote Admins and confirms the current account email. |
| Create a form | `POST /admin/forms` with `{}` → `201` editor representation | Same route and body with `Location: /api/v1/admin/forms/{id}` |
| Open a draft from the published version | `POST /admin/forms/{id}/draft` with `{revision}` → `200` editor representation | `POST /admin/forms/{id}/drafts` → `201` with the editor representation and `Location: /api/v1/admin/forms/{id}/draft`. GET on that location returns `{revision,draft}`. Draft replacement remains `PUT /admin/forms/{id}/draft`. |
| Publish a draft | `POST /admin/forms/{id}/publish` with `{revision}` → `200` editor representation | `POST /admin/forms/{id}/versions` → `201` with the editor representation and `Location: /api/v1/admin/forms/{id}/versions/{version}`. GET on that location returns `{id,version,definition}` under Manage Forms. |
| Create a submission | `POST /forms/{id}/submission` with `{version,values}` → `201` receipt | `POST /forms/{id}/submissions` → `201` receipt and `Location: /api/v1/forms/{id}/submissions/mine` |
| Read or resubmit your submission | `GET /forms/{id}/submission` → `200` receipt or `null`. PUT with `{version,revision,values}` → `200` receipt. | `GET` and `PUT /forms/{id}/submissions/mine` keep those responses. The session identifies the owner. PUT requires a change request and the original version. |
| Decide a submission review | `PUT /admin/submissions/{id}/review` → `200` review | `PATCH /admin/submissions/{id}` takes the existing decision payload with its revision and returns `200`. Only submissions awaiting review can be decided. |

The caller must use the same authenticated context when following caller-scoped locations such as `/users/me` and `/forms/{id}/submissions/mine`.

## Other API operations

| Method and path | Access and response |
| --- | --- |
| `POST /register-admin` | `410`. Use an admin invitation or promotion. |
| `GET /auth/providers` | Public `200` with `{google}`. |
| `GET /auth/google`, `GET /auth/google/admin`, `GET /auth/google/invitation` | Browser entry points when Google is enabled, normally `302` to Google. The invitation flow requires the grant cookie. |
| `GET /auth/google/callback` | `303` to the UI, including browser error redirects. Register the exact existing Google redirect URI. |
| `GET` and `POST /auth/google/link`, `GET` and `POST /auth/google/admin/link` | Appropriate account session required. GET returns `200` link status. POST requires an allowed origin and password confirmation, then returns `200` with the provider URL. |
| `POST /auth/admin-invitation/open`, `GET /auth/admin-invitation`, `POST /auth/admin-invitation/accept` | Open exchanges the token for a grant cookie. Open and read return `200` with `{email,enrolled}`. The signed-in recipient accepts with `204`. |
| `GET /admin/access`, `GET /admin/access/accounts?q=…`, `GET /admin/access/accounts/{kind}/{id}/roles`, `GET /admin/access/audit?before=…` | Manage Roles. `200` catalog, matching accounts, account grants, or audit events. |
| `PUT /admin/access/roles/{role}` | Manage Roles. `200` with `{id}`. |
| `PUT` and `DELETE /admin/access/accounts/{kind}/{id}/roles/{role}`, `DELETE /admin/access/accounts/user/{id}/approvals/{submission}`, `PUT /admin/access/legacy-approval-roles/{key}` | Manage Roles. Empty `204` for completed changes. Last-manager, ownership, and approval rules apply. |
| `POST /admin/access/invitations` | Invite Admins. Empty `202` after successful delivery. |
| `GET /forms`, `GET /forms/{id}` | Browse Forms. `200` list or published definition. Restricted and follow-up form access is checked on read. |
| `GET /admin/forms`, `GET /admin/forms/{id}`, `GET /admin/forms/approval-roles` | Manage Forms. `200` list, editor representation, or eligible role choices. |
| `POST /admin/forms/preview` | Manage Forms. `200` with `{valid,errors}` for a valid preview request, including when the sample answers fail validation. |
| `GET /admin/submissions`, `GET /admin/submissions/{id}` | Review Submissions. `200` with `{items,nextBefore?}` or the review representation. The list accepts `q` (case-insensitive substring in form title, submitter name, or email), `status` (`submitted`, `changes_requested`, or `approved`), and the `before` cursor. Keep the same filters when requesting the next page. |
| `GET /admin/jobs?before=…`, `GET /admin/jobs/{id}`, `GET /admin/jobs/audience-roles` | Admin session with Publish Jobs (`jobs.publish`). Returns a page of drafts and publications, one admin job representation, or available roles. |
| `POST /admin/jobs`, `PUT /admin/jobs/{id}/draft` | Publish Jobs. Create or replace a draft. Create returns `201` with its admin `Location`. Replace also requires the current `revision` and returns `200`. |
| `PUT /admin/jobs/{id}/publication` | Publish Jobs. Accepts `{revision,audience:{verifiedUsers,roleIds}}` and returns `200` with publication time, publisher, audience, and unique recipient count. Repeating the same publication returns the original receipt. |
| `GET /jobs?before=…`, `GET /jobs/{id}` | User account. Only published jobs delivered to the caller are visible. The list returns `{items,nextBefore?}`. Both jobs lists order by descending ID and accept the previous page’s cursor. |

### Job drafts and delivery

Draft fields are `company`, `title`, `description`, `requirements`, `deadline` (an RFC 3339 timestamp), `contact`, and `applicationInstructions`. An in-progress draft may omit fields. Company, title, and description are required to publish. A stale revision, an edit to a published draft, or a changed audience on a publication retry returns `409 job_changed`.

Assign Publish Jobs through Roles and access, then sign in as an admin. Publication takes a snapshot of matching regular user accounts. Verified users include existing placement-verified accounts and users with a form approval record. Role targeting uses direct user roles and active, approval-safe form grants. A combined audience includes users matching either selector. The transaction records the publication and its audit event and creates one in-app delivery per recipient.

## Errors and migration

API errors include a stable `code`, a human-readable `error`, and `errors` when field-level messages are available.

```json
{
  "code": "invalid_answers",
  "error": "correct the highlighted answers",
  "errors": {"code": "This field is required."}
}
```

Client code should branch on the status and `code`. Typical pairs include `400 invalid_request`, `401 authentication_required`, `403 forbidden`, `404 not_found`, `405 method_not_allowed` with `Allow`, `409 conflict`, `410 gone`, `415 unsupported_media_type`, `422 invalid_answers`, and `429 rate_limited`. Revision and submission conflicts can return `draft_changed`, `already_submitted`, `version_changed`, or `submission_changed`. Expired verification returns `verification_expired`. Delivery can fail with `502` for an admin invitation, while an unconfigured mailer or unavailable API can return `503`.

Legacy routes remain available with their existing success statuses, payloads, and cookies. The JSON `code` field and creation headers are additive. Release the API before or together with the canonical UI. The Vercel release pins the UI commit and verifies signed-out JSON responses from both profile and renewal paths. An older UI can run against the new API, so a rollback to an older API must pair it with an older UI. Confirm external client use and retain legacy routes through the rollback window before choosing a removal date.
