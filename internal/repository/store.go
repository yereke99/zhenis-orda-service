package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrForbidden    = errors.New("forbidden")
	ErrInvalidState = errors.New("invalid state")
)

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func scanString(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func scanStringPtr(ns sql.NullString) *string {
	if ns.Valid {
		value := ns.String
		return &value
	}
	return nil
}

func scanInt64(ni sql.NullInt64) *int64 {
	if ni.Valid {
		value := ni.Int64
		return &value
	}
	return nil
}

func scanTime(nt sql.NullTime) *time.Time {
	if nt.Valid {
		value := nt.Time
		return &value
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func parseFeatures(raw string) []string {
	var features []string
	if err := json.Unmarshal([]byte(raw), &features); err != nil {
		return nil
	}
	return features
}

func normalizeProvider(provider string) string {
	switch strings.TrimSpace(provider) {
	case PaymentProviderKaspiPay, PaymentProviderHalyk, PaymentProviderBankCard:
		return provider
	default:
		return PaymentProviderKaspiQR
	}
}

func newID() string {
	return uuid.NewString()
}

func IsUUID(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
}

func sourceID(id string) string {
	return id
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

func (s *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Audit(ctx context.Context, actor AdminActor, action, entityType, entityID string, metadata any) error {
	raw := "{}"
	if metadata != nil {
		if b, err := json.Marshal(metadata); err == nil {
			raw = string(b)
		}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO admin_actions(id, admin_id, role, action, entity_type, entity_id, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?);
	`, newID(), actor.ID, actor.Role, action, entityType, entityID, raw)
	return err
}

func rowErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func nullableStringPtrValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func sqlLike(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return "%"
	}
	query = strings.ReplaceAll(query, "%", "\\%")
	query = strings.ReplaceAll(query, "_", "\\_")
	return fmt.Sprintf("%%%s%%", query)
}
