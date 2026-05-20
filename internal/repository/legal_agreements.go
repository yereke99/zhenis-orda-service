package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type UserLegalAgreement struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	TelegramID       int64     `json:"telegram_id"`
	DocumentType     string    `json:"document_type"`
	DocumentVersion  string    `json:"document_version"`
	DocumentLanguage string    `json:"document_language"`
	DocumentHash     string    `json:"document_hash"`
	AcceptedAt       time.Time `json:"accepted_at"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (s *Store) GetUserLegalAgreement(ctx context.Context, userID, documentType, documentVersion string) (*UserLegalAgreement, error) {
	agreement, err := scanUserLegalAgreement(s.db.QueryRowContext(ctx, `
		SELECT id, user_id, telegram_id, document_type, document_version, document_language, document_hash, accepted_at, created_at, updated_at
		FROM user_legal_agreements
		WHERE user_id = ? AND document_type = ? AND document_version = ?;
	`, userID, documentType, documentVersion))
	if err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &agreement, nil
}

func (s *Store) AcceptUserLegalAgreement(ctx context.Context, agreement UserLegalAgreement) (UserLegalAgreement, error) {
	agreement.DocumentType = strings.TrimSpace(agreement.DocumentType)
	agreement.DocumentVersion = strings.TrimSpace(agreement.DocumentVersion)
	agreement.DocumentLanguage = strings.TrimSpace(agreement.DocumentLanguage)
	agreement.DocumentHash = strings.TrimSpace(agreement.DocumentHash)
	if strings.TrimSpace(agreement.UserID) == "" || agreement.TelegramID == 0 || agreement.DocumentType == "" ||
		agreement.DocumentVersion == "" || agreement.DocumentHash == "" ||
		(agreement.DocumentLanguage != "kk" && agreement.DocumentLanguage != "ru") {
		return UserLegalAgreement{}, ErrInvalidState
	}
	if agreement.ID == "" {
		agreement.ID = newID()
	}
	if agreement.AcceptedAt.IsZero() {
		agreement.AcceptedAt = nowUTC()
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO user_legal_agreements(id, user_id, telegram_id, document_type, document_version, document_language, document_hash, accepted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, document_type, document_version) DO UPDATE SET
			telegram_id = excluded.telegram_id,
			document_language = excluded.document_language,
			document_hash = excluded.document_hash,
			accepted_at = user_legal_agreements.accepted_at,
			updated_at = CURRENT_TIMESTAMP;
	`, agreement.ID, agreement.UserID, agreement.TelegramID, agreement.DocumentType, agreement.DocumentVersion, agreement.DocumentLanguage, agreement.DocumentHash, agreement.AcceptedAt); err != nil {
		return UserLegalAgreement{}, err
	}
	saved, err := s.GetUserLegalAgreement(ctx, agreement.UserID, agreement.DocumentType, agreement.DocumentVersion)
	if err != nil {
		return UserLegalAgreement{}, err
	}
	if saved == nil {
		return UserLegalAgreement{}, ErrNotFound
	}
	return *saved, nil
}

func scanUserLegalAgreement(row interface{ Scan(dest ...any) error }) (UserLegalAgreement, error) {
	var agreement UserLegalAgreement
	if err := row.Scan(
		&agreement.ID,
		&agreement.UserID,
		&agreement.TelegramID,
		&agreement.DocumentType,
		&agreement.DocumentVersion,
		&agreement.DocumentLanguage,
		&agreement.DocumentHash,
		&agreement.AcceptedAt,
		&agreement.CreatedAt,
		&agreement.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return UserLegalAgreement{}, ErrNotFound
		}
		return UserLegalAgreement{}, err
	}
	return agreement, nil
}
