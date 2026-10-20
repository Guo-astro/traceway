# billing-api

Invoices for the storefront: the invoice page and invoice e-mails.
Go, `net/http`, PostgreSQL. Telemetry goes to Traceway over OTLP.

```
go test ./...
go run ./cmd/server
```

Releases are tagged in the commit message (`v1.8.2: ...`) and deployed from `main`.
