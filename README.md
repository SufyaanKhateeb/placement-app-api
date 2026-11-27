# Placement App API

The API uses Go 1.23.1 or later and managed PostgreSQL on Neon. The frontend uses the existing `/api/v1` routes.

## Run locally with a separate database

Run PostgreSQL locally and create an empty database named `placement_dev`. With Docker, start one with:

```powershell
docker run --name placement-dev-postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=placement_dev -p 127.0.0.1:5432:5432 -d postgres:16
```

From the API checkout:

```powershell
go run ./cmd/migrate up
go run ./cmd
```

The local commands use `LOCAL_DATABASE_URL` from the environment or the ignored `.env.local` file. The default is `postgres://postgres:postgres@127.0.0.1:5432/placement_dev?sslmode=disable`. They require a loopback PostgreSQL address. The local API listens at `http://127.0.0.1:8090`, generates a session key at startup, and captures verification codes and invitation links in its terminal. Run `npm run dev` in the UI checkout and open `http://localhost:3000`.

To exercise OTP, register a fresh fixture address, switch tabs to read the code in the API terminal, return to the app and submit it. The UI repository also includes repeatable browser tests for this flow.

## Managed database

Use Neon's pooled `DATABASE_URL` for the Vercel API and the direct `MIGRATION_DATABASE_URL` for the release workflow. The workflow selects that database explicitly with `go run ./cmd/migrate -managed up`. [Neon connection guide](https://neon.com/docs/connect/connect-from-any-app)

Run the API test suite with `go test ./...`. To include the PostgreSQL integration tests, set `PLACEMENT_TEST_DATABASE_URL` to a disposable migrated test database.


## Google sign-in

Register a Google OAuth web client with the exact API redirect URI, for example: `http://localhost:3000/api/v1/auth/google/callback`. Set `GOOGLE_OIDC_CLIENT_ID`, `GOOGLE_OIDC_CLIENT_SECRET`, and `GOOGLE_OIDC_REDIRECT_URL` in the API's ignored environment file. In local development the Vite proxy forwards the callback to the API. For production, the frontend and API must use HTTPS and be on the same site for browser session cookies.

Apply the database migration before enabling Google sign-in. A first Google sign-in creates a regular app account when the email is unused. An existing account can be linked automatically only when it meets the personal Gmail and email verification requirements below.
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

The deployed Go API sends invitations from the configured Gmail or Google Workspace mailbox using Gmail SMTP over TLS.

1. Turn on **2-Step Verification** for the sender mailbox and create a Google App Password named **Placement App**. The mailbox must allow App Passwords. [Google App Password setup](https://support.google.com/accounts/answer/185833)
2. In the Vercel project's **Settings → Environment Variables**, set `ADMIN_INVITE_GMAIL_EMAIL` to the full sender email address. Set `ADMIN_INVITE_GMAIL_APP_PASSWORD` to the generated App Password and store it as a **Secret**.
3. Run the paired release workflow to apply the new settings. After deployment, send a test invitation from **Roles and access** to an approved address.

The API uses `smtp.gmail.com:465` and uses the configured mailbox as the message sender. Leave both mail settings unset to disable new invitation emails while keeping promotion available. Google sign-in uses the separate `GOOGLE_OIDC_*` settings. [Google SMTP documentation](https://knowledge.workspace.google.com/admin/gmail/send-email-from-a-printer-scanner-or-app)

The Gmail transport test uses a local SMTP server with a fixture TLS certificate. The PostgreSQL invitation test uses a captured mailer and requires a disposable migrated database in `PLACEMENT_TEST_DATABASE_URL`.

For application rollback, retain the admin membership tables, pending invitations, and access history. Block `POST /api/v1/register-admin` when serving the previous API and confirm a legacy admin still holds Role Manager. Downgrading the schema discards invitation and admin membership state and is intended for disposable rehearsal databases.

## Password account email verification

Apply migration `20261127090000` before publishing the paired API and UI. New password sign-ups and regular password accounts verifying on their next login require the same Gmail SMTP settings as admin invitations. Already linked Google accounts and admin invitation enrollment keep their existing sign-in paths.

Automatic Google linking applies to a unique, email-verified regular password account with a matching personal Gmail address. Other Google accounts can be connected from **Account** after password sign-in and confirmation.

## Vercel deployment

The React UI and Go API are released to one Vercel project at one HTTPS origin. The repositories remain separate. The release workflow checks out a specific UI commit into `web/`, then sends both source trees to Vercel to build the UI and the Go Function in `api/index.go`. The original `go run ./cmd` server is still the local entry point. PostgreSQL remains on Neon.

### One-time setup

1. Create an **Other** framework Vercel project and link it from the API checkout with `vercel login` and `vercel link`. Use GitHub Actions as the deployment source. A standalone Git deployment of the API repository would have no UI checkout in `web/`.
2. In the Vercel project's **Production** environment, set `DATABASE_URL` to the Neon pooled connection string, `FRONTEND_ORIGIN` to the final HTTPS origin, and `JWT_PRIVATE_KEY_PEM` to a deployment RSA private key (at least 2048 bits). The function derives its verification key from the private key. Set `GOOGLE_OIDC_CLIENT_ID`, `GOOGLE_OIDC_CLIENT_SECRET`, and `GOOGLE_OIDC_REDIRECT_URL` together to enable Google sign-in. Register the exact same `https://<app-host>/api/v1/auth/google/callback` URI with Google.
3. In the API repo's GitHub Actions **production** environment, set secrets `VERCEL_ORG_ID`, `VERCEL_PROJECT_ID`, `VERCEL_TOKEN`, and `MIGRATION_DATABASE_URL` (Neon's direct connection to the same deployed database). Set the variable `PRODUCTION_ORIGIN` to the same HTTPS origin. If the UI repo is private, provide a read-only `UI_CHECKOUT_TOKEN` covering that repository.
4. From the API repo, run **Release to Vercel** on `main` and enter the full SHA of a reviewed UI `main` commit. The workflow runs tests, builds both components on Vercel, applies pending migrations, promotes the deployment, and checks the public routes. Keep the credentials in Vercel and GitHub secrets.

The Vite development server and deployed UI both use the relative path `/api/v1`. Vite proxies local requests to the loopback Go server.
