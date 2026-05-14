package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (s *Store) RegisterOrUpdateTelegramUser(ctx context.Context, input TelegramUserInput) (User, bool, error) {
	var user User
	created := false
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var existingID string
		err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE telegram_id = ?`, input.TelegramID).Scan(&existingID)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == sql.ErrNoRows {
			created = true
			userID := newID()
			referralCode := fmt.Sprintf("ref%d", input.TelegramID)
			var invitedBy *string
			if strings.HasPrefix(input.StartParam, "ref") {
				var inviterID string
				var inviterTelegramID int64
				refErr := tx.QueryRowContext(ctx, `SELECT id, telegram_id FROM users WHERE referral_code = ?`, input.StartParam).Scan(&inviterID, &inviterTelegramID)
				if refErr == nil && inviterTelegramID != input.TelegramID {
					invitedBy = &inviterID
				}
			}
			_, err = tx.ExecContext(ctx, `
				INSERT INTO users(id, telegram_id, username, first_name, last_name, photo_url, language, referral_code, invited_by_user_id, current_level, last_seen_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, CURRENT_TIMESTAMP);
			`, userID, input.TelegramID, input.Username, input.FirstName, input.LastName, input.PhotoURL, input.Language, referralCode, nullableStringPtrValue(invitedBy))
			if err != nil {
				return err
			}
			if invitedBy != nil {
				if _, err := tx.ExecContext(ctx, `
					INSERT OR IGNORE INTO referrals(id, inviter_user_id, invited_user_id, status)
					VALUES (?, ?, ?, 'registered');
				`, newID(), *invitedBy, userID); err != nil {
					return err
				}
			}
		} else {
			_, err = tx.ExecContext(ctx, `
				UPDATE users SET
					username = COALESCE(NULLIF(?, ''), username),
					first_name = COALESCE(NULLIF(?, ''), first_name),
					last_name = COALESCE(NULLIF(?, ''), last_name),
					photo_url = COALESCE(NULLIF(?, ''), photo_url),
					language = CASE WHEN language = '' THEN COALESCE(NULLIF(?, ''), language) ELSE language END,
					last_seen_at = CURRENT_TIMESTAMP,
					updated_at = CURRENT_TIMESTAMP
				WHERE id = ?;
			`, input.Username, input.FirstName, input.LastName, input.PhotoURL, input.Language, existingID)
			if err != nil {
				return err
			}
		}
		found, err := scanUserRow(tx.QueryRowContext(ctx, userSelectSQL+` WHERE u.telegram_id = ?`, input.TelegramID))
		if err != nil {
			return err
		}
		user = found
		return nil
	})
	return user, created, err
}

func (s *Store) SetLanguage(ctx context.Context, userID string, language string) error {
	if language != "kk" && language != "ru" {
		language = "kk"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET language = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, language, userID)
	return err
}

func (s *Store) GetUserByTelegramID(ctx context.Context, telegramID int64) (User, error) {
	user, err := scanUserRow(s.db.QueryRowContext(ctx, userSelectSQL+` WHERE u.telegram_id = ?`, telegramID))
	return user, rowErr(err)
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (User, error) {
	user, err := scanUserRow(s.db.QueryRowContext(ctx, userSelectSQL+` WHERE u.id = ?`, userID))
	return user, rowErr(err)
}

func (s *Store) TouchUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_seen_at = CURRENT_TIMESTAMP WHERE id = ?`, userID)
	return err
}

func (s *Store) SaveDiagnostics(ctx context.Context, userID string, answers map[string]string) error {
	raw, _ := json.Marshal(answers)
	age := 0
	if answers["age"] != "" {
		fmt.Sscanf(answers["age"], "%d", &age)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO diagnostics(id, user_id, name, city, age, income, has_debt, has_business, main_problem, growth_area, answers_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`, newID(), userID, answers["name"], answers["city"], age, answers["income"], answers["has_debt"], answers["has_business"], answers["main_problem"], answers["growth_area"], string(raw))
	return err
}

func (s *Store) ListTariffs(ctx context.Context, onlyActive bool) ([]Tariff, error) {
	query := `SELECT id, code, title, price_kzt, COALESCE(short_description_kk, ''), COALESCE(full_description_kk, ''),
		features_json, COALESCE(image_url, ''), COALESCE(image_file_path, ''), COALESCE(image_source, 'none'), sort_order, is_active FROM tariffs`
	if onlyActive {
		query += ` WHERE is_active = 1`
	}
	query += ` ORDER BY sort_order ASC, price_kzt ASC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tariffs []Tariff
	for rows.Next() {
		tariff, err := scanTariff(rows)
		if err != nil {
			return nil, err
		}
		tariffs = append(tariffs, tariff)
	}
	return tariffs, rows.Err()
}

func (s *Store) GetTariffByCode(ctx context.Context, code string) (Tariff, error) {
	tariff, err := scanTariff(s.db.QueryRowContext(ctx, `
		SELECT id, code, title, price_kzt, COALESCE(short_description_kk, ''), COALESCE(full_description_kk, ''),
			features_json, COALESCE(image_url, ''), COALESCE(image_file_path, ''), COALESCE(image_source, 'none'), sort_order, is_active
		FROM tariffs WHERE code = ?;
	`, strings.ToUpper(code)))
	if err != nil {
		return Tariff{}, rowErr(err)
	}
	return tariff, nil
}

func (s *Store) GetTariffByID(ctx context.Context, id string) (Tariff, error) {
	tariff, err := scanTariff(s.db.QueryRowContext(ctx, `
		SELECT id, code, title, price_kzt, COALESCE(short_description_kk, ''), COALESCE(full_description_kk, ''),
			features_json, COALESCE(image_url, ''), COALESCE(image_file_path, ''), COALESCE(image_source, 'none'), sort_order, is_active
		FROM tariffs WHERE id = ?;
	`, id))
	if err != nil {
		return Tariff{}, rowErr(err)
	}
	return tariff, nil
}

type tariffScanner interface {
	Scan(dest ...any) error
}

func scanTariff(row tariffScanner) (Tariff, error) {
	var tariff Tariff
	var active int
	if err := row.Scan(
		&tariff.ID,
		&tariff.Code,
		&tariff.Title,
		&tariff.PriceKZT,
		&tariff.ShortDescriptionKK,
		&tariff.FullDescriptionKK,
		&tariff.FeaturesJSON,
		&tariff.ImageURL,
		&tariff.ImageFilePath,
		&tariff.ImageSource,
		&tariff.SortOrder,
		&active,
	); err != nil {
		return Tariff{}, err
	}
	tariff.IsActive = active == 1
	tariff.Features = parseFeatures(tariff.FeaturesJSON)
	return tariff, nil
}

func (s *Store) GetActiveSubscription(ctx context.Context, userID string) (*Subscription, error) {
	sub, err := scanSubscriptionRow(s.db.QueryRowContext(ctx, subscriptionSelectSQL+`
		WHERE s.user_id = ? AND s.status = 'active' AND s.expires_at > CURRENT_TIMESTAMP
		ORDER BY s.expires_at DESC
		LIMIT 1;
	`, userID))
	if err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &sub, nil
}

func (s *Store) CoinBalance(ctx context.Context, userID string) (int, error) {
	var balance sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount), 0) FROM coin_transactions WHERE user_id = ?`, userID).Scan(&balance); err != nil {
		return 0, err
	}
	return int(balance.Int64), nil
}

func (s *Store) ReferralSummary(ctx context.Context, userID string, botUsername string) (ReferralSummary, error) {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return ReferralSummary{}, err
	}
	var invited, paid int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM referrals WHERE inviter_user_id = ?`, userID).Scan(&invited); err != nil {
		return ReferralSummary{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM referrals WHERE inviter_user_id = ? AND status IN ('paid','rewarded')`, userID).Scan(&paid); err != nil {
		return ReferralSummary{}, err
	}
	rewards, err := s.ListReferralRewards(ctx, userID)
	if err != nil {
		return ReferralSummary{}, err
	}
	link := fmt.Sprintf("https://t.me/zhenisorda_bot?start=%s", user.ReferralCode)
	if botUsername != "" {
		link = fmt.Sprintf("https://t.me/%s?start=%s", strings.TrimPrefix(botUsername, "@"), user.ReferralCode)
	}
	return ReferralSummary{ReferralCode: user.ReferralCode, ReferralLink: link, InvitedCount: invited, PaidCount: paid, Rewards: rewards}, nil
}

func (s *Store) ListReferralRewards(ctx context.Context, userID string) ([]ReferralReward, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, threshold_count, reward_type, status, created_at
		FROM referral_rewards
		WHERE user_id = ?
		ORDER BY threshold_count ASC;
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rewards []ReferralReward
	for rows.Next() {
		var reward ReferralReward
		if err := rows.Scan(&reward.ID, &reward.UserID, &reward.ThresholdCount, &reward.RewardType, &reward.Status, &reward.CreatedAt); err != nil {
			return nil, err
		}
		rewards = append(rewards, reward)
	}
	return rewards, rows.Err()
}

func (s *Store) AddCoins(ctx context.Context, userID string, amount int, reason, sourceType string, sourceID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO coin_transactions(id, user_id, amount, reason, source_type, source_id)
		VALUES (?, ?, ?, ?, ?, ?);
	`, newID(), userID, amount, reason, sourceType, sourceID)
	if err != nil {
		return false, err
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

func (s *Store) AddCoinsTx(ctx context.Context, tx *sql.Tx, userID string, amount int, reason, sourceType string, sourceID string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO coin_transactions(id, user_id, amount, reason, source_type, source_id)
		VALUES (?, ?, ?, ?, ?, ?);
	`, newID(), userID, amount, reason, sourceType, sourceID)
	return err
}

const userSelectSQL = `
	SELECT u.id, u.telegram_id, COALESCE(u.username, ''), COALESCE(u.first_name, ''), COALESCE(u.last_name, ''), COALESCE(u.photo_url, ''),
		COALESCE(u.language, ''), COALESCE(u.phone, ''), u.referral_code, u.invited_by_user_id,
		u.current_level, u.access_closed, u.created_at, u.updated_at, u.last_seen_at
	FROM users u`

func scanUserRow(row interface{ Scan(dest ...any) error }) (User, error) {
	var user User
	var invited sql.NullString
	var accessClosed int
	if err := row.Scan(&user.ID, &user.TelegramID, &user.Username, &user.FirstName, &user.LastName, &user.PhotoURL, &user.Language, &user.Phone, &user.ReferralCode, &invited, &user.CurrentLevel, &accessClosed, &user.CreatedAt, &user.UpdatedAt, &user.LastSeenAt); err != nil {
		return User{}, rowErr(err)
	}
	user.InvitedByUserID = scanStringPtr(invited)
	user.AccessClosed = accessClosed == 1
	return user, nil
}

const subscriptionSelectSQL = `
	SELECT s.id, s.user_id, s.tariff_id, t.code, t.title, s.status, s.started_at, s.expires_at,
		s.cancelled_at, s.created_at, s.updated_at
	FROM subscriptions s
	JOIN tariffs t ON t.id = s.tariff_id`

func scanSubscriptionRow(row interface{ Scan(dest ...any) error }) (Subscription, error) {
	var sub Subscription
	var cancelled sql.NullTime
	if err := row.Scan(&sub.ID, &sub.UserID, &sub.TariffID, &sub.TariffCode, &sub.TariffTitle, &sub.Status, &sub.StartedAt, &sub.ExpiresAt, &cancelled, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
		return Subscription{}, rowErr(err)
	}
	sub.CancelledAt = scanTime(cancelled)
	return sub, nil
}

func (s *Store) ListInactiveUsers(ctx context.Context, inactiveSince time.Time, limit int) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, userSelectSQL+`
		WHERE u.last_seen_at <= ?
		ORDER BY u.last_seen_at ASC
		LIMIT ?;
	`, inactiveSince, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		user, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) ListSubscriptionsExpiringBetween(ctx context.Context, from, to time.Time) ([]Subscription, error) {
	rows, err := s.db.QueryContext(ctx, subscriptionSelectSQL+`
		WHERE s.status = 'active' AND s.expires_at BETWEEN ? AND ?
		ORDER BY s.expires_at ASC;
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var subs []Subscription
	for rows.Next() {
		sub, err := scanSubscriptionRow(rows)
		if err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

func (s *Store) ListUsersWithActiveTariffAtLeast(ctx context.Context, tariffCode string, limit int) ([]User, error) {
	if limit <= 0 || limit > 2000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, userSelectSQL+`
		JOIN subscriptions s ON s.user_id = u.id AND s.status = 'active' AND s.expires_at > CURRENT_TIMESTAMP
		JOIN tariffs t ON t.id = s.tariff_id
		WHERE u.access_closed = 0
			AND CASE t.code WHEN 'VIP' THEN 3 WHEN 'STANDARD' THEN 2 ELSE 1 END >=
				CASE ? WHEN 'VIP' THEN 3 WHEN 'STANDARD' THEN 2 ELSE 1 END
		GROUP BY u.id
		ORDER BY u.id ASC
		LIMIT ?;
	`, tariffCode, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		user, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}
