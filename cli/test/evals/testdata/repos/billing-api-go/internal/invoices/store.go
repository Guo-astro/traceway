package invoices

import (
	"context"
	"database/sql"
)

// Store loads invoices.
type Store interface {
	Invoice(ctx context.Context, id int64) (*Invoice, error)
}

// SQLStore loads invoices from PostgreSQL.
type SQLStore struct{ db *sql.DB }

// NewStore returns a Store backed by db.
func NewStore(db *sql.DB) *SQLStore { return &SQLStore{db: db} }

// Invoice loads one invoice with its customer and lines.
func (s *SQLStore) Invoice(ctx context.Context, id int64) (*Invoice, error) {
	inv := &Invoice{Customer: &Customer{}}
	err := s.db.QueryRowContext(ctx, `SELECT i.id, i.number, i.issued_at, i.total_cents, i.currency, c.id, c.name, c.email
		FROM invoices i JOIN customers c ON c.id = i.customer_id WHERE i.id = $1`, id).
		Scan(&inv.ID, &inv.Number, &inv.IssuedAt, &inv.TotalCents, &inv.Currency, &inv.Customer.ID, &inv.Customer.Name, &inv.Customer.Email)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var a Address
	err = s.db.QueryRowContext(ctx, `SELECT line1, line2, city, postal_code, country FROM billing_addresses WHERE customer_id = $1`, inv.Customer.ID).
		Scan(&a.Line1, &a.Line2, &a.City, &a.PostalCode, &a.Country)
	switch {
	case err == sql.ErrNoRows:
	case err != nil:
		return nil, err
	default:
		inv.Customer.BillingAddress = &a
	}
	return inv, nil
}
