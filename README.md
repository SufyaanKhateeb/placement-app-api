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