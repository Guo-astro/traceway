package main

import (
	"database/sql"
	"log/slog"
	"net/http"
	"os"

	"example.com/billing-api/internal/invoices"
)

func main() {
	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		slog.Error("open database", "err", err)
		os.Exit(1)
	}
	h := invoices.NewHandler(invoices.NewStore(db))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/invoices/{id}", h.Get)
	addr := ":" + os.Getenv("PORT")
	if addr == ":" {
		addr = ":8080"
	}
	slog.Info("listening", "addr", addr)
	if err := http.ListenAndServe(addr, invoices.Recover(mux)); err != nil {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
}
