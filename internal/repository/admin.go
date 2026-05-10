package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

func (s *Store) AdminStats(ctx context.Context) (AdminStats, error) {
	var stats AdminStats
	queries := []struct {
		dest  *int
		query string
	}{
		{&stats.UsersTotal, `SELECT COUNT(*) FROM users`},
		{&stats.ActiveSubscriptions, `SELECT COUNT(*) FROM subscriptions WHERE status = 'active' AND expires_at > CURRENT_TIMESTAMP`},
		{&stats.ExpiredSubscriptions, `SELECT COUNT(*) FROM subscriptions WHERE status = 'expired'`},
		{&stats.PendingPayments, `SELECT COUNT(*) FROM payments WHERE status = 'pending'`},
		{&stats.UploadedReceipts, `SELECT COUNT(*) FROM payments WHERE status = 'uploaded_receipt'`},
		{&stats.ReferralsPaid, `SELECT COUNT(*) FROM referrals WHERE status IN ('paid','rewarded')`},
		{&stats.CoinsIssued, `SELECT COALESCE(SUM(amount), 0) FROM coin_transactions WHERE amount > 0`},
	}
	for _, query := range queries {
		if err := s.db.QueryRowContext(ctx, query.query).Scan(query.dest); err != nil {
			return AdminStats{}, err
		}
	}
	return stats, nil
}

func (s *Store) ListUsers(ctx context.Context, search string, limit, offset int) ([]User, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, userSelectSQL+`
		WHERE (? = '%' OR u.username LIKE ? ESCAPE '\' OR u.first_name LIKE ? ESCAPE '\' OR u.last_name LIKE ? ESCAPE '\' OR CAST(u.telegram_id AS TEXT) LIKE ? ESCAPE '\')
		ORDER BY u.created_at DESC
		LIMIT ? OFFSET ?;
	`, sqlLike(search), sqlLike(search), sqlLike(search), sqlLike(search), sqlLike(search), limit, offset)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range users {
		sub, _ := s.GetActiveSubscription(ctx, users[i].ID)
		users[i].Subscription = sub
		users[i].CoinBalance, _ = s.CoinBalance(ctx, users[i].ID)
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM referrals WHERE inviter_user_id = ?`, users[i].ID).Scan(&users[i].ReferralCount)
	}
	return users, nil
}

func (s *Store) SetUserAccessClosed(ctx context.Context, userID int64, closed bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET access_closed = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, boolInt(closed), userID)
	return err
}

func (s *Store) ListPayments(ctx context.Context, status string, limit, offset int) ([]Payment, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	args := []any{}
	where := ``
	if status != "" {
		where = ` WHERE p.status = ?`
		args = append(args, status)
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, paymentSelectSQL+where+` ORDER BY p.created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var payments []Payment
	for rows.Next() {
		payment, err := scanPaymentRow(rows)
		if err != nil {
			return nil, err
		}
		payments = append(payments, payment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range payments {
		user, _ := s.GetUserByID(ctx, payments[i].UserID)
		payments[i].User = &user
	}
	return payments, nil
}

func (s *Store) ListSubscriptions(ctx context.Context, status string, limit, offset int) ([]Subscription, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	args := []any{}
	where := ``
	if status != "" {
		where = ` WHERE s.status = ?`
		args = append(args, status)
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, subscriptionSelectSQL+where+` ORDER BY s.expires_at DESC LIMIT ? OFFSET ?`, args...)
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

func (s *Store) UpdateSubscriptionStatus(ctx context.Context, subscriptionID int64, status string) error {
	if status != SubscriptionStatusActive && status != SubscriptionStatusExpired && status != SubscriptionStatusCancelled && status != SubscriptionStatusPaused {
		return ErrInvalidState
	}
	_, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, subscriptionID)
	return err
}

func (s *Store) UpsertLevel(ctx context.Context, level Level) (Level, error) {
	if level.Number <= 0 {
		return Level{}, ErrInvalidState
	}
	if level.ID == 0 {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO levels(number, title_kk, title_ru, description_kk, description_ru, sort_order, is_active)
			VALUES (?, ?, ?, ?, ?, ?, ?);
		`, level.Number, level.TitleKK, level.TitleRU, level.DescriptionKK, level.DescriptionRU, level.SortOrder, boolInt(level.IsActive))
		if err != nil {
			return Level{}, err
		}
		level.ID, _ = res.LastInsertId()
		return level, nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE levels SET number=?, title_kk=?, title_ru=?, description_kk=?, description_ru=?, sort_order=?, is_active=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?;
	`, level.Number, level.TitleKK, level.TitleRU, level.DescriptionKK, level.DescriptionRU, level.SortOrder, boolInt(level.IsActive), level.ID)
	return level, err
}

func (s *Store) DeleteLevel(ctx context.Context, levelID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE levels SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, levelID)
	return err
}

func (s *Store) UpsertLesson(ctx context.Context, lesson Lesson) (Lesson, error) {
	if lesson.ID == 0 {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO lessons(level_id, title_kk, title_ru, description_kk, description_ru, video_url, sort_order, is_active)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?);
		`, lesson.LevelID, lesson.TitleKK, lesson.TitleRU, lesson.DescriptionKK, lesson.DescriptionRU, lesson.VideoURL, lesson.SortOrder, boolInt(lesson.IsActive))
		if err != nil {
			return Lesson{}, err
		}
		lesson.ID, _ = res.LastInsertId()
		return lesson, nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE lessons SET level_id=?, title_kk=?, title_ru=?, description_kk=?, description_ru=?, video_url=?, sort_order=?, is_active=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?;
	`, lesson.LevelID, lesson.TitleKK, lesson.TitleRU, lesson.DescriptionKK, lesson.DescriptionRU, lesson.VideoURL, lesson.SortOrder, boolInt(lesson.IsActive), lesson.ID)
	return lesson, err
}

func (s *Store) DeleteLesson(ctx context.Context, lessonID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE lessons SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, lessonID)
	return err
}

func (s *Store) UpsertTest(ctx context.Context, test Test) (Test, error) {
	if test.LevelID == 0 {
		return Test{}, ErrInvalidState
	}
	if test.PassPercent == 0 {
		test.PassPercent = 70
	}
	if test.Title == "" {
		test.Title = "Level test"
	}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if test.ID == 0 {
			res, err := tx.ExecContext(ctx, `
				INSERT INTO tests(level_id, title, pass_percent, is_active)
				VALUES (?, ?, ?, ?)
				ON CONFLICT(level_id) DO UPDATE SET title=excluded.title, pass_percent=excluded.pass_percent, is_active=excluded.is_active, updated_at=CURRENT_TIMESTAMP;
			`, test.LevelID, test.Title, test.PassPercent, boolInt(test.IsActive))
			if err != nil {
				return err
			}
			id, _ := res.LastInsertId()
			if id == 0 {
				if err := tx.QueryRowContext(ctx, `SELECT id FROM tests WHERE level_id = ?`, test.LevelID).Scan(&id); err != nil {
					return err
				}
			}
			test.ID = id
		} else {
			if _, err := tx.ExecContext(ctx, `
				UPDATE tests SET level_id=?, title=?, pass_percent=?, is_active=?, updated_at=CURRENT_TIMESTAMP
				WHERE id=?;
			`, test.LevelID, test.Title, test.PassPercent, boolInt(test.IsActive), test.ID); err != nil {
				return err
			}
		}
		for _, question := range test.Questions {
			questionID := question.ID
			if questionID == 0 {
				res, err := tx.ExecContext(ctx, `
					INSERT INTO test_questions(test_id, question_text_kk, question_text_ru, sort_order, is_active)
					VALUES (?, ?, ?, ?, 1);
				`, test.ID, question.QuestionTextKK, question.QuestionTextRU, question.SortOrder)
				if err != nil {
					return err
				}
				questionID, _ = res.LastInsertId()
			} else {
				if _, err := tx.ExecContext(ctx, `
					UPDATE test_questions SET question_text_kk=?, question_text_ru=?, sort_order=?, is_active=1, updated_at=CURRENT_TIMESTAMP
					WHERE id=? AND test_id=?;
				`, question.QuestionTextKK, question.QuestionTextRU, question.SortOrder, questionID, test.ID); err != nil {
					return err
				}
			}
			for _, option := range question.Options {
				if option.ID == 0 {
					if _, err := tx.ExecContext(ctx, `
						INSERT INTO test_options(question_id, option_text_kk, option_text_ru, is_correct, sort_order)
						VALUES (?, ?, ?, ?, ?);
					`, questionID, option.OptionTextKK, option.OptionTextRU, boolInt(option.IsCorrect), option.SortOrder); err != nil {
						return err
					}
				} else {
					if _, err := tx.ExecContext(ctx, `
						UPDATE test_options SET option_text_kk=?, option_text_ru=?, is_correct=?, sort_order=?, updated_at=CURRENT_TIMESTAMP
						WHERE id=? AND question_id=?;
					`, option.OptionTextKK, option.OptionTextRU, boolInt(option.IsCorrect), option.SortOrder, option.ID, questionID); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return Test{}, err
	}
	return test, nil
}

func (s *Store) DeleteTest(ctx context.Context, testID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tests SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, testID)
	return err
}

func (s *Store) ReviewAssignmentSubmission(ctx context.Context, submissionID, adminID int64, status string) error {
	if status == "" {
		status = "reviewed"
	}
	if status != "reviewed" && status != "rejected" {
		return ErrInvalidState
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE assignment_submissions
		SET status = ?, reviewed_by_admin_id = ?, reviewed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?;
	`, status, adminID, submissionID)
	return err
}

func (s *Store) UpsertTariff(ctx context.Context, tariff Tariff) (Tariff, error) {
	features, _ := json.Marshal(tariff.Features)
	if tariff.ID == 0 {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO tariffs(code, title, price_kzt, features_json, sort_order, is_active)
			VALUES (?, ?, ?, ?, ?, ?);
		`, strings.ToUpper(tariff.Code), tariff.Title, tariff.PriceKZT, string(features), tariff.SortOrder, boolInt(tariff.IsActive))
		if err != nil {
			return Tariff{}, err
		}
		tariff.ID, _ = res.LastInsertId()
		return tariff, nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE tariffs SET code=?, title=?, price_kzt=?, features_json=?, sort_order=?, is_active=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?;
	`, strings.ToUpper(tariff.Code), tariff.Title, tariff.PriceKZT, string(features), tariff.SortOrder, boolInt(tariff.IsActive), tariff.ID)
	return tariff, err
}

func (s *Store) ListChannels(ctx context.Context, userID int64, admin bool) ([]Channel, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, telegram_chat_id, invite_link_type, COALESCE(manual_invite_link, ''), tariff_requirement, level_requirement, is_active
		FROM channels
		`+activeWhere(admin)+`
		ORDER BY level_requirement ASC, tariff_requirement ASC, title ASC;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var channels []Channel
	for rows.Next() {
		var channel Channel
		var active int
		if err := rows.Scan(&channel.ID, &channel.Title, &channel.TelegramChatID, &channel.InviteLinkType, &channel.ManualInviteLink, &channel.TariffRequirement, &channel.LevelRequirement, &active); err != nil {
			return nil, err
		}
		channel.IsActive = active == 1
		channels = append(channels, channel)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range channels {
		if userID > 0 {
			channels[i].Access, _ = s.CanAccessChannel(ctx, userID, channels[i])
		}
	}
	return channels, nil
}

func activeWhere(admin bool) string {
	if admin {
		return ""
	}
	return "WHERE is_active = 1"
}

func (s *Store) CanAccessChannel(ctx context.Context, userID int64, channel Channel) (bool, error) {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil || user.AccessClosed {
		return false, err
	}
	sub, err := s.GetActiveSubscription(ctx, userID)
	if err != nil || sub == nil {
		return false, err
	}
	if user.CurrentLevel < channel.LevelRequirement {
		return false, nil
	}
	return tariffRank(sub.TariffCode) >= tariffRank(channel.TariffRequirement), nil
}

func tariffRank(code string) int {
	switch strings.ToUpper(code) {
	case "VIP":
		return 3
	case "STANDARD":
		return 2
	default:
		return 1
	}
}

func (s *Store) GetChannel(ctx context.Context, channelID int64) (Channel, error) {
	var channel Channel
	var active int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, telegram_chat_id, invite_link_type, COALESCE(manual_invite_link, ''), tariff_requirement, level_requirement, is_active
		FROM channels
		WHERE id = ?;
	`, channelID).Scan(&channel.ID, &channel.Title, &channel.TelegramChatID, &channel.InviteLinkType, &channel.ManualInviteLink, &channel.TariffRequirement, &channel.LevelRequirement, &active)
	if err != nil {
		return Channel{}, rowErr(err)
	}
	channel.IsActive = active == 1
	return channel, nil
}

func (s *Store) UpsertChannel(ctx context.Context, channel Channel) (Channel, error) {
	if channel.InviteLinkType == "" {
		channel.InviteLinkType = "manual"
	}
	if channel.TariffRequirement == "" {
		channel.TariffRequirement = "BASIC"
	}
	if channel.LevelRequirement <= 0 {
		channel.LevelRequirement = 1
	}
	if channel.ID == 0 {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO channels(title, telegram_chat_id, invite_link_type, manual_invite_link, tariff_requirement, level_requirement, is_active)
			VALUES (?, ?, ?, ?, ?, ?, ?);
		`, channel.Title, channel.TelegramChatID, channel.InviteLinkType, nullableString(channel.ManualInviteLink), channel.TariffRequirement, channel.LevelRequirement, boolInt(channel.IsActive))
		if err != nil {
			return Channel{}, err
		}
		channel.ID, _ = res.LastInsertId()
		return channel, nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE channels SET title=?, telegram_chat_id=?, invite_link_type=?, manual_invite_link=?, tariff_requirement=?, level_requirement=?, is_active=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?;
	`, channel.Title, channel.TelegramChatID, channel.InviteLinkType, nullableString(channel.ManualInviteLink), channel.TariffRequirement, channel.LevelRequirement, boolInt(channel.IsActive), channel.ID)
	return channel, err
}

func (s *Store) DeleteChannel(ctx context.Context, channelID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE channels SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, channelID)
	return err
}

func (s *Store) RecordInviteLink(ctx context.Context, userID, channelID int64, inviteLink string, expiresAt *string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO channel_invite_links(user_id, channel_id, invite_link, expires_at, status)
		VALUES (?, ?, ?, ?, 'issued');
	`, userID, channelID, inviteLink, nullableStringPtr(expiresAt))
	return err
}

func nullableStringPtr(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func (s *Store) ListStreams(ctx context.Context, userID int64, admin bool) ([]LiveStream, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ls.id, ls.title, COALESCE(ls.description, ''), ls.starts_at, COALESCE(ls.stream_url, ''), ls.tariff_requirement, ls.status,
			COALESCE((SELECT recording_url FROM live_stream_recordings r WHERE r.stream_id = ls.id ORDER BY r.id DESC LIMIT 1), '')
		FROM live_streams ls
		ORDER BY ls.starts_at DESC;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var streams []LiveStream
	for rows.Next() {
		var stream LiveStream
		if err := rows.Scan(&stream.ID, &stream.Title, &stream.Description, &stream.StartsAt, &stream.StreamURL, &stream.TariffRequirement, &stream.Status, &stream.RecordingURL); err != nil {
			return nil, err
		}
		streams = append(streams, stream)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if !admin && userID > 0 {
		sub, _ := s.GetActiveSubscription(ctx, userID)
		for i := range streams {
			if sub == nil || tariffRank(sub.TariffCode) < tariffRank(streams[i].TariffRequirement) {
				streams[i].StreamURL = ""
				streams[i].RecordingURL = ""
			}
		}
	}
	return streams, nil
}

func (s *Store) UpsertStream(ctx context.Context, stream LiveStream) (LiveStream, error) {
	if stream.TariffRequirement == "" {
		stream.TariffRequirement = "STANDARD"
	}
	if stream.Status == "" {
		stream.Status = "scheduled"
	}
	if stream.ID == 0 {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO live_streams(title, description, starts_at, stream_url, tariff_requirement, status)
			VALUES (?, ?, ?, ?, ?, ?);
		`, stream.Title, stream.Description, stream.StartsAt, stream.StreamURL, stream.TariffRequirement, stream.Status)
		if err != nil {
			return LiveStream{}, err
		}
		stream.ID, _ = res.LastInsertId()
		return stream, nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE live_streams SET title=?, description=?, starts_at=?, stream_url=?, tariff_requirement=?, status=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?;
	`, stream.Title, stream.Description, stream.StartsAt, stream.StreamURL, stream.TariffRequirement, stream.Status, stream.ID)
	return stream, err
}

func (s *Store) DeleteStream(ctx context.Context, streamID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE live_streams SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP WHERE id = ?`, streamID)
	return err
}

func (s *Store) Broadcast(ctx context.Context, actor AdminActor, title, body, target string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO broadcasts(admin_id, title, body, target, status)
		VALUES (?, ?, ?, ?, 'queued');
	`, actor.ID, title, body, target)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) CreateSupportMessage(ctx context.Context, userID int64, body string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO support_messages(user_id, body) VALUES (?, ?)`, userID, body)
	return err
}

func (s *Store) ListAudit(ctx context.Context, limit, offset int) ([]map[string]any, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, admin_id, COALESCE(role, ''), action, COALESCE(entity_type, ''), COALESCE(entity_id, ''), metadata_json, created_at
		FROM admin_actions
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?;
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []map[string]any
	for rows.Next() {
		var id int64
		var adminID sql.NullInt64
		var role, action, entityType, entityID, metadata string
		var createdAt string
		if err := rows.Scan(&id, &adminID, &role, &action, &entityType, &entityID, &metadata, &createdAt); err != nil {
			return nil, err
		}
		item := map[string]any{"id": id, "role": role, "action": action, "entity_type": entityType, "entity_id": entityID, "metadata": metadata, "created_at": createdAt}
		if adminID.Valid {
			item["admin_id"] = adminID.Int64
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM app_settings ORDER BY key ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		settings[key] = value
	}
	return settings, rows.Err()
}

func (s *Store) PatchSettings(ctx context.Context, values map[string]string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		for key, value := range values {
			if strings.TrimSpace(key) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO app_settings(key, value)
				VALUES (?, ?)
				ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP;
			`, key, value); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) PlaceholderList(ctx context.Context, name string) ([]map[string]any, error) {
	var query string
	switch name {
	case "tests":
		query = `SELECT id, level_id, title, pass_percent, is_active, created_at FROM tests ORDER BY id DESC LIMIT 200`
	case "assignments":
		query = `SELECT id, level_id, title_kk, title_ru, is_active, created_at FROM assignments ORDER BY id DESC LIMIT 200`
	case "assignment_submissions":
		query = `SELECT id, assignment_id, user_id, status, created_at FROM assignment_submissions ORDER BY created_at DESC LIMIT 200`
	case "referrals":
		query = `SELECT id, inviter_user_id, invited_user_id, status, reward_granted, created_at FROM referrals ORDER BY created_at DESC LIMIT 200`
	case "coins":
		query = `SELECT id, user_id, amount, reason, source_type, source_id, created_at FROM coin_transactions ORDER BY created_at DESC LIMIT 200`
	case "invite_links":
		query = `SELECT id, user_id, channel_id, invite_link, status, created_at FROM channel_invite_links ORDER BY created_at DESC LIMIT 200`
	default:
		return nil, fmt.Errorf("unknown list %s", name)
	}
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		item := map[string]any{}
		for i, col := range cols {
			switch v := values[i].(type) {
			case []byte:
				item[col] = string(v)
			default:
				item[col] = v
			}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
