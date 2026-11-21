# Placement App API

The HTTP server uses Go 1.23.1 or later and PostgreSQL. The frontend uses the existing `/api/v1` routes.

Set `DATABASE_URL` to your local PostgreSQL database. Set `PRIVATE_KEY_PATH` and `PUBLIC_KEY_PATH` to the RSA keys used to sign and verify session tokens. The server reads these settings from the environment or an ignored local `.env` file. `PORT` defaults to `8090` and `FRONTEND_ORIGIN` defaults to `http://localhost:5173`.

Apply migrations and start the API from the repository root.

```powershell
go run ./cmd/migrate up
go run ./cmd
```

Run the test suite with `go test ./...`.