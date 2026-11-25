# Placement App API

The API uses Go 1.23.1 or later and managed PostgreSQL on Neon. The frontend uses the existing `/api/v1` routes.

## Connect to Neon

Create a Neon project and use the **direct** connection string from the Connect dialog. Set `DATABASE_URL` in your ignored local `.env` or deployment secrets. The Go API already maintains its own connection pool.

Use `sslmode=verify-full` to verify the server certificate and hostname. If the copied URL includes `channel_binding=require`, omit that parameter for the Go drivers pinned in this repository. Keep the rest of the connection details from Neon.

If the application later uses a pooled endpoint, set `DATABASE_URL` to the pooled URL and `MIGRATION_DATABASE_URL` to the direct URL. When the latter is unset, migrations use `DATABASE_URL`. [Neon connection guide](https://neon.com/docs/connect/connect-from-any-app) · [Connection pooling](https://neon.com/docs/connect/connection-pooling)

Set `PRIVATE_KEY_PATH` and `PUBLIC_KEY_PATH` to the RSA keys used for session tokens. `PORT` defaults to `8090` and `FRONTEND_ORIGIN` defaults to `http://localhost:5173`.

## Initialize or move the database

For an empty database, run the migrations and start the API from the repository root.

```powershell
go run ./cmd/migrate up
go run ./cmd
```

To move existing accounts and verification records, restore a full dump of the existing database into an empty Neon database using the direct endpoint. Include the `schema_migrations` table, omit source ownership and grants during restore, and run the migration command afterward to apply any pending repository migrations. Verify the restored data before directing the API to Neon. [Neon import guide](https://neon.com/docs/import/import-from-postgres)

Run the test suite with `go test ./...`.
## Google sign-in

Register a Google OAuth web client with the exact API redirect URI, for example: `http://localhost:8091/api/v1/auth/google/callback`. Set `GOOGLE_OIDC_CLIENT_ID`, `GOOGLE_OIDC_CLIENT_SECRET`, and `GOOGLE_OIDC_REDIRECT_URL` in the API's ignored environment file. Use `localhost` for both the local API URL and `FRONTEND_ORIGIN`. For production, the frontend and API must use HTTPS and be on the same site for browser session cookies.

Apply the database migration before enabling Google sign-in. A first Google sign-in creates a regular app account when the email is unused. An existing account is never matched by email alone. Linking existing accounts and provisioning Google admin identities are separate workflows.
Admins can sign in at `/admin/login` with their existing password. On the admin account page, confirm the current password and connect a Google account with the same verified email. Future visits to `/admin/login` support Google sign-in for that linked identity. An identity already connected to another account cannot be reused for admin access.
## Generic user migration

Migration `20261121200000` renames the account and verification tables and the OIDC user reference in place. Account IDs, Google identities, verification rows, and foreign keys are preserved. New regular-user JWTs use the `user` role. Existing signed `student` tokens are accepted as regular sessions and refreshed access tokens use `user`. Admin access checks are unchanged.

The `/api/v1/user` and `/api/v1/refresh` responses include a canonical `role` of `user` or `admin`. They retain `uType` as a compatibility field for existing clients. The updated frontend also reads profiles from older API builds.

### Rollout

1. Confirm the database is on migration `20261121190000`, with a clean migration state, and deploy the updated frontend.
2. Pause the old API before applying `go run ./cmd/migrate up`. The old API queries the original table names.
3. Start the new API with the existing JWT signing keys. Check registration, password and Google sign-in, and admin access.
4. If rollback is necessary and the current migration is still `20261121200000`, stop the API and run `go run ./cmd/migrate down-one`, then restart the previous API. The previous API may prompt users with newly issued `user` tokens to sign in again.
## Form builder

Apply migration `20261121210000` before running the form builder API. Accounts with form management access manage shared drafts at `/admin/forms`. Accounts with browsing access can view published forms at `/forms`.

Saves and publish requests include the revision the admin reviewed. A stale revision returns HTTP 409. Publishing preserves the previous snapshot and commits the new active version with the revision change. The admin preview checks sample values without saving a submission. Decimal values are passed as strings to preserve precision.

The active published version is available to signed-in users. Per-form eligibility, attribute mapping, and submission are separate features.
Field validation rules are part of the draft and each published snapshot. Text fields can set character limits and a Go regex pattern. Whole numbers, precise decimals, dates, and timestamps can set inclusive bounds. Timestamp bounds are stored with an offset, and the editor displays them in the administrator's local time zone. Existing forms without rules continue to use required and type validation.
## Role-based access

Apply migration `20261125100000` before enabling the paired API and UI. The migration assigns Member to each existing account and adds Form Manager and Reviewer for existing admins. The earliest admin by creation time and then ID receives Role Manager. If there are no admins, the first admin provisioned through the existing database setup receives it. Existing sign-in cookies retain the same account identity. The API resolves permissions from PostgreSQL.

Use **Roles and access** to manage assignments and review changes. Before rollout, inventory old approval keys on the rehearsal database. A Role Manager can map each key to an eligible custom role. Existing verification records and follow-up unlocks are preserved. Older submissions with an unmapped role key can still be approved and receive verification.

For a release rollback, retain the added tables and access history. The previous API gives every admin staff access and allows admin registration. If that would restore revoked access or bypass the new registration gate, block the affected routes until a corrected release is ready. Use `down-one` on a disposable rehearsal database to verify reversibility.

## Admin invitations and promotion

Apply migration `20261126090000` before releasing the paired API and UI. Role Managers can invite an unused email or promote a selected account under **Roles and access**. Invited recipients sign in with a password or Google, then accept the 24-hour invitation. Sending another invitation to the same address replaces the link. Account owners use **Admin sign-in** after access is granted. Their account ID, credentials, and submissions stay with their original account.

For email delivery, set `ADMIN_INVITE_RESEND_API_KEY` and `ADMIN_INVITE_FROM` in the Vercel environment. Use a verified Resend sender domain, and disable link tracking for invitation mail. Leave both variables unset to disable new invitation emails while keeping promotion available. The PostgreSQL test uses a captured mailer and requires a disposable migrated database in `PLACEMENT_TEST_DATABASE_URL`. [Resend email setup](https://resend.com/docs/introduction)

For application rollback, retain the admin membership tables, pending invitations, and access history. Block `POST /api/v1/register-admin` when serving the previous API and confirm a legacy admin still holds Role Manager. Downgrading the schema discards invitation and admin membership state and is intended for disposable rehearsal databases.

## Vercel deployment

The React UI and Go API are released to one Vercel project at one HTTPS origin. The repositories remain separate. The release workflow checks out a specific UI commit into `web/`, then sends both source trees to Vercel to build the UI and the Go Function in `api/index.go`. The original `go run ./cmd` server is still the local entry point. PostgreSQL remains on Neon.

### One-time setup

1. Create an **Other** framework Vercel project and link it from the API checkout with `vercel login` and `vercel link`. Use GitHub Actions as the deployment source. A standalone Git deployment of the API repository would have no UI checkout in `web/`.
2. In the Vercel project's **Production** environment, set `DATABASE_URL` to the Neon pooled connection string, `FRONTEND_ORIGIN` to the final HTTPS origin, and `JWT_PRIVATE_KEY_PEM` to a deployment RSA private key (at least 2048 bits). The function derives its verification key from the private key. Set `GOOGLE_OIDC_CLIENT_ID`, `GOOGLE_OIDC_CLIENT_SECRET`, and `GOOGLE_OIDC_REDIRECT_URL` together to enable Google sign-in. Register the exact same `https://<app-host>/api/v1/auth/google/callback` URI with Google.
3. In the API repo's GitHub Actions **production** environment, set secrets `VERCEL_ORG_ID`, `VERCEL_PROJECT_ID`, `VERCEL_TOKEN`, and `MIGRATION_DATABASE_URL` (Neon's direct connection to the same deployed database). Set the variable `PRODUCTION_ORIGIN` to the same HTTPS origin. If the UI repo is private, provide a read-only `UI_CHECKOUT_TOKEN` covering that repository.
4. From the API repo, run **Release to Vercel** on `main` and enter the full SHA of a reviewed UI `main` commit. The workflow runs tests, builds both components on Vercel, applies pending migrations, promotes the deployment, and checks the public routes. Keep the credentials in Vercel and GitHub secrets.

For local Vite development, keep `VITE_BACKEND_URL=http://localhost:8091/api/v1` or the URL for your local Go server. The deployed UI uses the relative path `/api/v1`.
