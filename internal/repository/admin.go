package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type AdminLessonFilter struct {
	Query  string
	Level  int
	Status string
}

type AdminTestFilter struct {
	Query  string
	Level  int
	Status string
}

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
		{&stats.ApprovedPayments, `SELECT COUNT(*) FROM payments WHERE status = 'approved'`},
		{&stats.MonthlyRevenueKZT, `SELECT COALESCE(SUM(amount_kzt), 0) FROM payments WHERE status = 'approved' AND approved_at >= datetime('now', 'start of month')`},
		{&stats.LessonsCount, `SELECT COUNT(*) FROM lessons WHERE is_active = 1`},
		{&stats.TestsCount, `SELECT COUNT(*) FROM tests WHERE is_active = 1`},
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

func (s *Store) SetUserAccessClosed(ctx context.Context, userID string, closed bool) error {
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
		if receipt, _ := s.LatestReceiptForPayment(ctx, payments[i].ID); receipt != nil {
			payments[i].Receipt = receipt
		}
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

func (s *Store) UpdateSubscriptionStatus(ctx context.Context, subscriptionID string, status string) error {
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
	if level.SortOrder <= 0 {
		level.SortOrder = level.Number
	}
	if strings.TrimSpace(level.ID) == "" {
		level.ID = newID()
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO levels(id, number, title_kk, title_ru, description_kk, description_ru, sort_order, is_active)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?);
		`, level.ID, level.Number, level.TitleKK, level.TitleRU, level.DescriptionKK, level.DescriptionRU, level.SortOrder, boolInt(level.IsActive))
		if err != nil {
			return Level{}, err
		}
		return level, nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE levels SET number=?, title_kk=?, title_ru=?, description_kk=?, description_ru=?, sort_order=?, is_active=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?;
	`, level.Number, level.TitleKK, level.TitleRU, level.DescriptionKK, level.DescriptionRU, level.SortOrder, boolInt(level.IsActive), level.ID)
	return level, err
}

func (s *Store) ListAdminLevels(ctx context.Context) ([]Level, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, number, title_kk, title_ru, COALESCE(description_kk, ''), COALESCE(description_ru, ''), sort_order, is_active
		FROM levels
		ORDER BY sort_order ASC, number ASC;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var levels []Level
	for rows.Next() {
		var level Level
		var active int
		if err := rows.Scan(&level.ID, &level.Number, &level.TitleKK, &level.TitleRU, &level.DescriptionKK, &level.DescriptionRU, &level.SortOrder, &active); err != nil {
			return nil, err
		}
		level.IsActive = active == 1
		levels = append(levels, level)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return levels, nil
}

func (s *Store) DeleteLevel(ctx context.Context, levelID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE levels SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, levelID)
	return err
}

func (s *Store) UpsertLesson(ctx context.Context, lesson Lesson) (Lesson, error) {
	if strings.TrimSpace(lesson.LevelID) == "" || strings.TrimSpace(lesson.TitleKK) == "" || strings.TrimSpace(lesson.VideoURL) == "" {
		return Lesson{}, ErrInvalidState
	}
	var levelExists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM levels WHERE id = ?`, lesson.LevelID).Scan(&levelExists); err != nil {
		return Lesson{}, err
	}
	if levelExists == 0 {
		return Lesson{}, ErrNotFound
	}
	if strings.TrimSpace(lesson.TitleRU) == "" {
		lesson.TitleRU = lesson.TitleKK
	}
	if lesson.SortOrder <= 0 {
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) + 1 FROM lessons WHERE level_id = ?`, lesson.LevelID).Scan(&lesson.SortOrder); err != nil {
			return Lesson{}, err
		}
	}
	if strings.TrimSpace(lesson.ID) == "" {
		lesson.ID = newID()
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO lessons(id, level_id, title_kk, title_ru, description_kk, description_ru, video_url, sort_order, is_active)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
		`, lesson.ID, lesson.LevelID, lesson.TitleKK, lesson.TitleRU, lesson.DescriptionKK, lesson.DescriptionRU, lesson.VideoURL, lesson.SortOrder, boolInt(lesson.IsActive))
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "constraint") {
				return Lesson{}, ErrInvalidState
			}
			return Lesson{}, err
		}
		return lesson, nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE lessons SET level_id=?, title_kk=?, title_ru=?, description_kk=?, description_ru=?, video_url=?, sort_order=?, is_active=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?;
	`, lesson.LevelID, lesson.TitleKK, lesson.TitleRU, lesson.DescriptionKK, lesson.DescriptionRU, lesson.VideoURL, lesson.SortOrder, boolInt(lesson.IsActive), lesson.ID)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "constraint") {
		return Lesson{}, ErrInvalidState
	}
	return lesson, err
}

func (s *Store) DeleteLesson(ctx context.Context, lessonID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE lessons SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, lessonID)
	return err
}

func (s *Store) ListAdminLessons(ctx context.Context, filter AdminLessonFilter) ([]Lesson, error) {
	args := []any{}
	conditions := []string{"1=1"}
	if filter.Level > 0 {
		conditions = append(conditions, "lv.number = ?")
		args = append(args, filter.Level)
	}
	switch filter.Status {
	case "active":
		conditions = append(conditions, "l.is_active = 1")
	case "inactive":
		conditions = append(conditions, "l.is_active = 0")
	}
	if strings.TrimSpace(filter.Query) != "" {
		like := sqlLike(filter.Query)
		conditions = append(conditions, "(l.title_kk LIKE ? ESCAPE '\\' OR l.title_ru LIKE ? ESCAPE '\\' OR COALESCE(l.video_url, '') LIKE ? ESCAPE '\\')")
		args = append(args, like, like, like)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id, l.level_id, lv.number, l.title_kk, l.title_ru, COALESCE(l.description_kk, ''), COALESCE(l.description_ru, ''),
			COALESCE(l.video_url, ''), l.sort_order, l.is_active, 0, NULL
		FROM lessons l
		JOIN levels lv ON lv.id = l.level_id
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY lv.number ASC, l.sort_order ASC, l.created_at DESC;
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lessons := []Lesson{}
	for rows.Next() {
		var lesson Lesson
		var active, watched int
		var watchedAt sql.NullTime
		if err := rows.Scan(&lesson.ID, &lesson.LevelID, &lesson.LevelNumber, &lesson.TitleKK, &lesson.TitleRU, &lesson.DescriptionKK, &lesson.DescriptionRU, &lesson.VideoURL, &lesson.SortOrder, &active, &watched, &watchedAt); err != nil {
			return nil, err
		}
		lesson.IsActive = active == 1
		lesson.Watched = watched == 1
		lesson.WatchedAt = scanTime(watchedAt)
		lesson.Access = true
		lessons = append(lessons, lesson)
	}
	return lessons, rows.Err()
}

func (s *Store) UpsertTest(ctx context.Context, test Test) (Test, error) {
	if strings.TrimSpace(test.LevelID) == "" {
		return Test{}, ErrInvalidState
	}
	if strings.TrimSpace(test.Title) == "" {
		return Test{}, ErrInvalidState
	}
	if test.PassPercent < 1 || test.PassPercent > 100 {
		return Test{}, ErrInvalidState
	}
	if len(test.Questions) == 0 {
		return Test{}, ErrInvalidState
	}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var levelExists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM levels WHERE id = ?`, test.LevelID).Scan(&levelExists); err != nil {
			return err
		}
		if levelExists == 0 {
			return ErrNotFound
		}
		if err := validateTestPayload(test); err != nil {
			return err
		}
		if strings.TrimSpace(test.ID) == "" {
			test.ID = newID()
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO tests(id, level_id, title, pass_percent, is_active)
				VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(level_id) DO UPDATE SET title=excluded.title, pass_percent=excluded.pass_percent, is_active=excluded.is_active, updated_at=CURRENT_TIMESTAMP;
			`, test.ID, test.LevelID, test.Title, test.PassPercent, boolInt(test.IsActive)); err != nil {
				return err
			}
			var persistedID string
			if err := tx.QueryRowContext(ctx, `SELECT id FROM tests WHERE level_id = ?`, test.LevelID).Scan(&persistedID); err != nil {
				return err
			}
			test.ID = persistedID
		} else {
			if _, err := tx.ExecContext(ctx, `
				UPDATE tests SET level_id=?, title=?, pass_percent=?, is_active=?, updated_at=CURRENT_TIMESTAMP
				WHERE id=?;
			`, test.LevelID, test.Title, test.PassPercent, boolInt(test.IsActive), test.ID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM test_questions WHERE test_id = ?`, test.ID); err != nil {
			return err
		}
		for qi, question := range test.Questions {
			questionID := newID()
			if question.SortOrder <= 0 {
				question.SortOrder = qi + 1
			}
			if strings.TrimSpace(question.QuestionTextRU) == "" {
				question.QuestionTextRU = question.QuestionTextKK
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO test_questions(id, test_id, question_text_kk, question_text_ru, sort_order, is_active)
				VALUES (?, ?, ?, ?, ?, 1);
			`, questionID, test.ID, question.QuestionTextKK, question.QuestionTextRU, question.SortOrder); err != nil {
				return err
			}
			for oi, option := range question.Options {
				if option.SortOrder <= 0 {
					option.SortOrder = oi + 1
				}
				if strings.TrimSpace(option.OptionTextRU) == "" {
					option.OptionTextRU = option.OptionTextKK
				}
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO test_options(id, question_id, option_text_kk, option_text_ru, is_correct, sort_order)
					VALUES (?, ?, ?, ?, ?, ?);
				`, newID(), questionID, option.OptionTextKK, option.OptionTextRU, boolInt(option.IsCorrect), option.SortOrder); err != nil {
					return err
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

func validateTestPayload(test Test) error {
	for _, question := range test.Questions {
		if strings.TrimSpace(question.QuestionTextKK) == "" || len(question.Options) < 2 {
			return ErrInvalidState
		}
		correct := 0
		for _, option := range question.Options {
			if strings.TrimSpace(option.OptionTextKK) == "" {
				return ErrInvalidState
			}
			if option.IsCorrect {
				correct++
			}
		}
		if correct != 1 {
			return ErrInvalidState
		}
	}
	return nil
}

func (s *Store) DeleteTest(ctx context.Context, testID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tests SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, testID)
	return err
}

func (s *Store) ListAdminTests(ctx context.Context, filter AdminTestFilter) ([]Test, error) {
	args := []any{}
	conditions := []string{"1=1"}
	if filter.Level > 0 {
		conditions = append(conditions, "lv.number = ?")
		args = append(args, filter.Level)
	}
	switch filter.Status {
	case "active":
		conditions = append(conditions, "t.is_active = 1")
	case "inactive":
		conditions = append(conditions, "t.is_active = 0")
	}
	if strings.TrimSpace(filter.Query) != "" {
		conditions = append(conditions, "t.title LIKE ? ESCAPE '\\'")
		args = append(args, sqlLike(filter.Query))
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.level_id, lv.number, t.title, t.pass_percent, t.is_active
		FROM tests t
		JOIN levels lv ON lv.id = t.level_id
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY lv.number ASC, t.created_at DESC;
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tests := []Test{}
	for rows.Next() {
		var test Test
		var active int
		if err := rows.Scan(&test.ID, &test.LevelID, &test.LevelNumber, &test.Title, &test.PassPercent, &active); err != nil {
			return nil, err
		}
		test.IsActive = active == 1
		tests = append(tests, test)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range tests {
		tests[i].Questions, _ = s.loadTestQuestions(ctx, s.db, tests[i].ID, true)
	}
	return tests, nil
}

func (s *Store) loadTestQuestions(ctx context.Context, q queryer, testID string, includeCorrect bool) ([]TestQuestion, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT q.id, q.test_id, q.question_text_kk, q.question_text_ru, q.sort_order,
			o.id, o.option_text_kk, o.option_text_ru, o.sort_order, o.is_correct
		FROM test_questions q
		JOIN test_options o ON o.question_id = q.id
		WHERE q.test_id = ? AND q.is_active = 1
		ORDER BY q.sort_order ASC, o.sort_order ASC;
	`, testID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	questions := map[string]*TestQuestion{}
	for rows.Next() {
		var qn TestQuestion
		var opt TestOption
		var correct int
		if err := rows.Scan(&qn.ID, &qn.TestID, &qn.QuestionTextKK, &qn.QuestionTextRU, &qn.SortOrder, &opt.ID, &opt.OptionTextKK, &opt.OptionTextRU, &opt.SortOrder, &correct); err != nil {
			return nil, err
		}
		if _, ok := questions[qn.ID]; !ok {
			copy := qn
			questions[qn.ID] = &copy
		}
		opt.QuestionID = qn.ID
		if includeCorrect {
			opt.IsCorrect = correct == 1
		}
		questions[qn.ID].Options = append(questions[qn.ID].Options, opt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	list := make([]TestQuestion, 0, len(questions))
	for _, question := range questions {
		list = append(list, *question)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].SortOrder < list[j].SortOrder })
	return list, nil
}

func (s *Store) ReviewAssignmentSubmission(ctx context.Context, submissionID string, adminID int64, status string) error {
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
	if strings.TrimSpace(tariff.ID) == "" {
		tariff.ID = newID()
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO tariffs(id, code, title, price_kzt, features_json, sort_order, is_active)
			VALUES (?, ?, ?, ?, ?, ?, ?);
		`, tariff.ID, strings.ToUpper(tariff.Code), tariff.Title, tariff.PriceKZT, string(features), tariff.SortOrder, boolInt(tariff.IsActive))
		if err != nil {
			return Tariff{}, err
		}
		return tariff, nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE tariffs SET code=?, title=?, price_kzt=?, features_json=?, sort_order=?, is_active=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?;
	`, strings.ToUpper(tariff.Code), tariff.Title, tariff.PriceKZT, string(features), tariff.SortOrder, boolInt(tariff.IsActive), tariff.ID)
	return tariff, err
}

func (s *Store) ListChannels(ctx context.Context, userID string, admin bool) ([]Channel, error) {
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
		if userID != "" {
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

func (s *Store) CanAccessChannel(ctx context.Context, userID string, channel Channel) (bool, error) {
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

func (s *Store) GetChannel(ctx context.Context, channelID string) (Channel, error) {
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
	if strings.TrimSpace(channel.ID) == "" {
		channel.ID = newID()
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO channels(id, title, telegram_chat_id, invite_link_type, manual_invite_link, tariff_requirement, level_requirement, is_active)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?);
		`, channel.ID, channel.Title, channel.TelegramChatID, channel.InviteLinkType, nullableString(channel.ManualInviteLink), channel.TariffRequirement, channel.LevelRequirement, boolInt(channel.IsActive))
		if err != nil {
			return Channel{}, err
		}
		return channel, nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE channels SET title=?, telegram_chat_id=?, invite_link_type=?, manual_invite_link=?, tariff_requirement=?, level_requirement=?, is_active=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?;
	`, channel.Title, channel.TelegramChatID, channel.InviteLinkType, nullableString(channel.ManualInviteLink), channel.TariffRequirement, channel.LevelRequirement, boolInt(channel.IsActive), channel.ID)
	return channel, err
}

func (s *Store) DeleteChannel(ctx context.Context, channelID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE channels SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, channelID)
	return err
}

func (s *Store) RecordInviteLink(ctx context.Context, userID, channelID string, inviteLink string, expiresAt *string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO channel_invite_links(id, user_id, channel_id, invite_link, expires_at, status)
		VALUES (?, ?, ?, ?, ?, 'issued');
	`, newID(), userID, channelID, inviteLink, nullableStringPtr(expiresAt))
	return err
}

func nullableStringPtr(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func (s *Store) ListStreams(ctx context.Context, userID string, admin bool) ([]LiveStream, error) {
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
	if !admin && userID != "" {
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
	if strings.TrimSpace(stream.ID) == "" {
		stream.ID = newID()
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO live_streams(id, title, description, starts_at, stream_url, tariff_requirement, status)
			VALUES (?, ?, ?, ?, ?, ?, ?);
		`, stream.ID, stream.Title, stream.Description, stream.StartsAt, stream.StreamURL, stream.TariffRequirement, stream.Status)
		if err != nil {
			return LiveStream{}, err
		}
		return stream, nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE live_streams SET title=?, description=?, starts_at=?, stream_url=?, tariff_requirement=?, status=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?;
	`, stream.Title, stream.Description, stream.StartsAt, stream.StreamURL, stream.TariffRequirement, stream.Status, stream.ID)
	return stream, err
}

func (s *Store) DeleteStream(ctx context.Context, streamID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE live_streams SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP WHERE id = ?`, streamID)
	return err
}

func (s *Store) Broadcast(ctx context.Context, actor AdminActor, title, body, target string) (string, error) {
	target = normalizeBroadcastTarget(target)
	id := newID()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO broadcasts(id, admin_id, title, body, target, status)
		VALUES (?, ?, ?, ?, ?, 'queued');
	`, id, actor.ID, title, body, target)
	if err != nil {
		return "", err
	}
	return id, nil
}

func normalizeBroadcastTarget(target string) string {
	switch strings.TrimSpace(target) {
	case "active", "inactive":
		return target
	default:
		return "all"
	}
}

func (s *Store) ListBroadcasts(ctx context.Context, limit int) ([]Broadcast, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id, b.admin_id, COALESCE(b.title, ''), b.body, b.target, b.status, b.created_at, b.sent_at,
			COALESCE(SUM(CASE WHEN bm.status = 'sent' THEN 1 ELSE 0 END), 0) AS sent_count,
			COALESCE(SUM(CASE WHEN bm.status = 'failed' THEN 1 ELSE 0 END), 0) AS failed_count
		FROM broadcasts b
		LEFT JOIN broadcast_messages bm ON bm.broadcast_id = b.id
		GROUP BY b.id
		ORDER BY b.created_at DESC
		LIMIT ?;
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var broadcasts []Broadcast
	for rows.Next() {
		var b Broadcast
		var adminID sql.NullInt64
		var sentAt sql.NullTime
		if err := rows.Scan(&b.ID, &adminID, &b.Title, &b.Body, &b.Target, &b.Status, &b.CreatedAt, &sentAt, &b.SentCount, &b.FailedCount); err != nil {
			return nil, err
		}
		b.AdminID = scanInt64(adminID)
		b.SentAt = scanTime(sentAt)
		broadcasts = append(broadcasts, b)
	}
	return broadcasts, rows.Err()
}

func (s *Store) ClaimQueuedBroadcast(ctx context.Context) (*Broadcast, error) {
	var b Broadcast
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var adminID sql.NullInt64
		err := tx.QueryRowContext(ctx, `
			SELECT id, admin_id, COALESCE(title, ''), body, target, status, created_at, sent_at
			FROM broadcasts
			WHERE status = 'queued'
			ORDER BY created_at ASC
			LIMIT 1;
		`).Scan(&b.ID, &adminID, &b.Title, &b.Body, &b.Target, &b.Status, &b.CreatedAt, new(sql.NullTime))
		if err != nil {
			return rowErr(err)
		}
		res, err := tx.ExecContext(ctx, `UPDATE broadcasts SET status = 'processing' WHERE id = ? AND status = 'queued'`, b.ID)
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return ErrInvalidState
		}
		b.AdminID = scanInt64(adminID)
		b.Status = "processing"
		return nil
	})
	if err == ErrNotFound {
		return nil, nil
	}
	return &b, err
}

func (s *Store) BroadcastRecipients(ctx context.Context, target string) ([]BroadcastRecipient, error) {
	target = normalizeBroadcastTarget(target)
	query := `
		SELECT u.id, u.telegram_id, COALESCE(u.language, '')
		FROM users u
		WHERE u.telegram_id IS NOT NULL`
	switch target {
	case "active":
		query += ` AND EXISTS (
			SELECT 1 FROM subscriptions s
			WHERE s.user_id = u.id AND s.status = 'active' AND s.expires_at > CURRENT_TIMESTAMP
		)`
	case "inactive":
		query += ` AND NOT EXISTS (
			SELECT 1 FROM subscriptions s
			WHERE s.user_id = u.id AND s.status = 'active' AND s.expires_at > CURRENT_TIMESTAMP
		)`
	}
	query += ` ORDER BY u.id ASC;`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var recipients []BroadcastRecipient
	for rows.Next() {
		var recipient BroadcastRecipient
		if err := rows.Scan(&recipient.UserID, &recipient.TelegramID, &recipient.Language); err != nil {
			return nil, err
		}
		recipients = append(recipients, recipient)
	}
	return recipients, rows.Err()
}

func (s *Store) RecordBroadcastMessage(ctx context.Context, broadcastID string, recipient BroadcastRecipient, status, errorText string) error {
	var sentAt any
	if status == "sent" {
		sentAt = nowUTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO broadcast_messages(id, broadcast_id, user_id, telegram_id, status, error, sent_at)
		VALUES (?, ?, ?, ?, ?, ?, ?);
	`, newID(), broadcastID, recipient.UserID, recipient.TelegramID, status, nullableString(errorText), sentAt)
	return err
}

func (s *Store) FinishBroadcast(ctx context.Context, broadcastID string, failed bool) error {
	status := "completed"
	if failed {
		status = "failed"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE broadcasts SET status = ?, sent_at = CURRENT_TIMESTAMP WHERE id = ?`, status, broadcastID)
	return err
}

func (s *Store) CreateSupportMessage(ctx context.Context, userID string, body string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO support_messages(id, user_id, body) VALUES (?, ?, ?)`, newID(), userID, body)
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
		var id string
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
