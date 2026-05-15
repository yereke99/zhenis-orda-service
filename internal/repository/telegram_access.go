package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

func NormalizeTelegramChatID(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "-100") {
		suffix := strings.TrimPrefix(value, "-100")
		if suffix == "" || suffix[0] == '0' || !digitsOnly(suffix) {
			return "", ErrInvalidState
		}
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return "", ErrInvalidState
		}
		return value, nil
	}
	if strings.HasPrefix(value, "-") {
		return "", ErrInvalidState
	}
	if !digitsOnly(value) {
		return "", ErrInvalidState
	}
	if parsed, err := strconv.ParseInt(value, 10, 64); err != nil || parsed <= 0 {
		return "", ErrInvalidState
	}
	normalized := "-100" + value
	if _, err := strconv.ParseInt(normalized, 10, 64); err != nil {
		return "", ErrInvalidState
	}
	return normalized, nil
}

func digitsOnly(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func (s *Store) GetLevelByNumber(ctx context.Context, levelNumber int) (Level, error) {
	var level Level
	var active int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, number, title_kk, title_ru, COALESCE(description_kk, ''), COALESCE(description_ru, ''),
			COALESCE(telegram_chat_id, ''), sort_order, is_active
		FROM levels
		WHERE number = ?;
	`, levelNumber).Scan(&level.ID, &level.Number, &level.TitleKK, &level.TitleRU, &level.DescriptionKK, &level.DescriptionRU, &level.TelegramChatID, &level.SortOrder, &active)
	if err != nil {
		return Level{}, rowErr(err)
	}
	level.IsActive = active == 1
	level.TelegramConfigured = level.TelegramChatID != ""
	return level, nil
}

func (s *Store) ActiveSubscriptionForLevelInvite(ctx context.Context, userID string, levelNumber int) (*Subscription, error) {
	access, err := s.CanAccessLevel(ctx, userID, levelNumber)
	if err != nil || !access {
		if err != nil {
			return nil, err
		}
		return nil, ErrForbidden
	}
	sub, err := s.GetActiveSubscription(ctx, userID)
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, ErrForbidden
	}
	return sub, nil
}

func (s *Store) ReusableLevelTelegramInvite(ctx context.Context, userID, levelID, telegramChatID string) (*UserLevelTelegramInvite, error) {
	invite, err := scanLevelTelegramInvite(s.db.QueryRowContext(ctx, `
		SELECT id, user_id, telegram_user_id, level_id, telegram_chat_id, invite_link, COALESCE(invite_link_id, ''),
			COALESCE(raw_payload, '{}'), expires_at, status, created_at, updated_at
		FROM user_level_telegram_invites
		WHERE user_id = ? AND level_id = ? AND telegram_chat_id = ? AND status = 'issued'
			AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
		ORDER BY created_at DESC
		LIMIT 1;
	`, userID, levelID, telegramChatID))
	if err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &invite, nil
}

func (s *Store) CreateLevelTelegramInvite(ctx context.Context, invite UserLevelTelegramInvite) (UserLevelTelegramInvite, error) {
	if strings.TrimSpace(invite.UserID) == "" || strings.TrimSpace(invite.LevelID) == "" || strings.TrimSpace(invite.TelegramChatID) == "" || strings.TrimSpace(invite.InviteLink) == "" {
		return UserLevelTelegramInvite{}, ErrInvalidState
	}
	if invite.RawPayload == "" {
		invite.RawPayload = "{}"
	}
	if !json.Valid([]byte(invite.RawPayload)) {
		invite.RawPayload = "{}"
	}
	if invite.Status == "" {
		invite.Status = "issued"
	}
	if invite.Status != "issued" && invite.Status != "used" && invite.Status != "expired" && invite.Status != "revoked" {
		return UserLevelTelegramInvite{}, ErrInvalidState
	}
	invite.ID = newID()
	var telegramUserID any
	if invite.TelegramUserID != nil {
		telegramUserID = *invite.TelegramUserID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_level_telegram_invites(id, user_id, telegram_user_id, level_id, telegram_chat_id, invite_link, invite_link_id, raw_payload, expires_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`, invite.ID, invite.UserID, telegramUserID, invite.LevelID, invite.TelegramChatID, invite.InviteLink, nullableString(invite.InviteLinkID), invite.RawPayload, nullableTimePtr(invite.ExpiresAt), invite.Status)
	if err != nil {
		return UserLevelTelegramInvite{}, err
	}
	return s.GetLevelTelegramInvite(ctx, invite.ID)
}

func (s *Store) GetLevelTelegramInvite(ctx context.Context, id string) (UserLevelTelegramInvite, error) {
	invite, err := scanLevelTelegramInvite(s.db.QueryRowContext(ctx, `
		SELECT id, user_id, telegram_user_id, level_id, telegram_chat_id, invite_link, COALESCE(invite_link_id, ''),
			COALESCE(raw_payload, '{}'), expires_at, status, created_at, updated_at
		FROM user_level_telegram_invites
		WHERE id = ?;
	`, id))
	return invite, err
}

func (s *Store) ExpireLevelTelegramInvites(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE user_level_telegram_invites
		SET status = 'expired', updated_at = CURRENT_TIMESTAMP
		WHERE status = 'issued' AND expires_at IS NOT NULL AND expires_at <= CURRENT_TIMESTAMP;
	`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanLevelTelegramInvite(row interface{ Scan(dest ...any) error }) (UserLevelTelegramInvite, error) {
	var invite UserLevelTelegramInvite
	var telegramUserID sql.NullInt64
	var expiresAt sql.NullTime
	if err := row.Scan(&invite.ID, &invite.UserID, &telegramUserID, &invite.LevelID, &invite.TelegramChatID, &invite.InviteLink, &invite.InviteLinkID, &invite.RawPayload, &expiresAt, &invite.Status, &invite.CreatedAt, &invite.UpdatedAt); err != nil {
		return UserLevelTelegramInvite{}, rowErr(err)
	}
	invite.TelegramUserID = scanInt64(telegramUserID)
	invite.ExpiresAt = scanTime(expiresAt)
	return invite, nil
}

func nullableTimePtr(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC()
}

func (s *Store) SaveFinancialIQResult(ctx context.Context, userID string, score int, resultTitle, resultLevel, resultText string, answers map[string]any) (FinancialIQResult, error) {
	resultTitle = strings.TrimSpace(resultTitle)
	resultLevel = strings.TrimSpace(resultLevel)
	if strings.TrimSpace(userID) == "" || resultTitle == "" || resultLevel == "" {
		return FinancialIQResult{}, ErrInvalidState
	}
	if answers == nil {
		answers = map[string]any{}
	}
	raw, _ := json.Marshal(answers)
	id := newID()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO financial_iq_results(id, user_id, score, result_title, result_level, result_text, answers_json)
		VALUES (?, ?, ?, ?, ?, ?, ?);
	`, id, userID, score, resultTitle, resultLevel, strings.TrimSpace(resultText), string(raw))
	if err != nil {
		return FinancialIQResult{}, err
	}
	result, err := s.GetLatestFinancialIQResult(ctx, userID)
	if err != nil {
		return FinancialIQResult{}, err
	}
	if result == nil {
		return FinancialIQResult{}, ErrNotFound
	}
	return *result, nil
}

func (s *Store) GetLatestFinancialIQResult(ctx context.Context, userID string) (*FinancialIQResult, error) {
	result, err := scanFinancialIQResult(s.db.QueryRowContext(ctx, `
		SELECT id, user_id, score, result_title, result_level, COALESCE(result_text, ''), answers_json, created_at, updated_at
		FROM financial_iq_results
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT 1;
	`, userID))
	if err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &result, nil
}

func scanFinancialIQResult(row interface{ Scan(dest ...any) error }) (FinancialIQResult, error) {
	var result FinancialIQResult
	if err := row.Scan(&result.ID, &result.UserID, &result.Score, &result.ResultTitle, &result.ResultLevel, &result.ResultText, &result.AnswersJSON, &result.CreatedAt, &result.UpdatedAt); err != nil {
		return FinancialIQResult{}, rowErr(err)
	}
	return result, nil
}
