package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

func (s *Store) ListPremiumCoursesForUser(ctx context.Context, userID string) ([]PremiumCourse, error) {
	courses, err := s.listPremiumCourses(ctx, userID, false)
	if err != nil {
		return nil, err
	}
	for i := range courses {
		courses[i].TelegramChatID = ""
		courses[i].ManualInviteLink = ""
		courses[i].AdminNotes = ""
	}
	return courses, nil
}

func (s *Store) ListAdminPremiumCourses(ctx context.Context) ([]PremiumCourse, error) {
	return s.listPremiumCourses(ctx, "", true)
}

func (s *Store) listPremiumCourses(ctx context.Context, userID string, admin bool) ([]PremiumCourse, error) {
	where := ""
	if !admin {
		where = "WHERE status = 'active'"
	}
	rows, err := s.db.QueryContext(ctx, premiumCourseSelectSQL+`
		`+where+`
		ORDER BY sort_order ASC, created_at ASC;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var courses []PremiumCourse
	for rows.Next() {
		course, err := scanPremiumCourse(rows)
		if err != nil {
			return nil, err
		}
		courses = append(courses, course)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range courses {
		courses[i].TelegramConfigured = premiumCourseTelegramConfigured(courses[i])
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM premium_course_lessons WHERE course_id = ? AND is_active = 1`, courses[i].ID).Scan(&courses[i].LessonCount)
		if admin {
			var activeAccess, paymentCount, revokedAccess int
			courses[i].Stats = map[string]int{}
			_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_course_access WHERE course_id = ? AND access_status = 'active' AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)`, courses[i].ID).Scan(&activeAccess)
			_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE payment_type = 'premium_course' AND premium_course_id = ?`, courses[i].ID).Scan(&paymentCount)
			_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_course_access WHERE course_id = ? AND access_status = 'revoked'`, courses[i].ID).Scan(&revokedAccess)
			courses[i].Stats["active_access_count"] = activeAccess
			courses[i].Stats["payment_count"] = paymentCount
			courses[i].Stats["revoked_access_count"] = revokedAccess
			continue
		}
		if userID != "" {
			courses[i].Access, _ = s.HasPremiumCourseAccess(ctx, userID, courses[i].ID, nowUTC())
			courses[i].PreviewLessons, _ = s.ListPremiumCourseLessons(ctx, userID, courses[i].ID)
		}
	}
	return courses, nil
}

func (s *Store) GetPremiumCourseForUser(ctx context.Context, userID, courseID string) (PremiumCourse, error) {
	course, err := s.GetPremiumCourse(ctx, courseID, true)
	if err != nil {
		return PremiumCourse{}, err
	}
	course.Access, _ = s.HasPremiumCourseAccess(ctx, userID, course.ID, nowUTC())
	course.Lessons, _ = s.ListPremiumCourseLessons(ctx, userID, course.ID)
	course.TelegramConfigured = premiumCourseTelegramConfigured(course)
	course.TelegramChatID = ""
	course.ManualInviteLink = ""
	course.AdminNotes = ""
	return course, nil
}

func (s *Store) GetPremiumCourse(ctx context.Context, courseID string, activeOnly bool) (PremiumCourse, error) {
	where := `WHERE id = ?`
	if activeOnly {
		where += ` AND status = 'active'`
	}
	course, err := scanPremiumCourse(s.db.QueryRowContext(ctx, premiumCourseSelectSQL+` `+where+`;`, courseID))
	if err != nil {
		return PremiumCourse{}, rowErr(err)
	}
	course.TelegramConfigured = premiumCourseTelegramConfigured(course)
	return course, nil
}

func (s *Store) GetPremiumCourseBySlug(ctx context.Context, slug string) (PremiumCourse, error) {
	course, err := scanPremiumCourse(s.db.QueryRowContext(ctx, premiumCourseSelectSQL+` WHERE slug = ?;`, slug))
	return course, rowErr(err)
}

func (s *Store) UpsertPremiumCourse(ctx context.Context, course PremiumCourse) (PremiumCourse, error) {
	course.Slug = strings.ToLower(strings.TrimSpace(course.Slug))
	course.Title = strings.TrimSpace(course.Title)
	course.Description = strings.TrimSpace(course.Description)
	course.Status = strings.TrimSpace(course.Status)
	course.InviteLinkType = strings.TrimSpace(course.InviteLinkType)
	course.ManualInviteLink = strings.TrimSpace(course.ManualInviteLink)
	course.TelegramButtonTitle = strings.TrimSpace(course.TelegramButtonTitle)
	course.AdminNotes = strings.TrimSpace(course.AdminNotes)
	course.CoverImageURL = strings.TrimSpace(course.CoverImageURL)
	course.CoverImagePath = strings.TrimSpace(course.CoverImagePath)
	course.CoverImageSource = strings.TrimSpace(course.CoverImageSource)
	if course.Status == "" {
		course.Status = PremiumCourseStatusActive
	}
	if course.InviteLinkType == "" {
		course.InviteLinkType = "manual"
	}
	if course.DefaultAccessDurationType == "" {
		course.DefaultAccessDurationType = PremiumAccessDurationLifetime
	}
	if course.CoverImageSource == "" {
		switch {
		case course.CoverImagePath != "":
			course.CoverImageSource = "uploaded"
		case course.CoverImageURL != "":
			course.CoverImageSource = "url"
		default:
			course.CoverImageSource = "none"
		}
	}
	if course.CoverImageSource != "uploaded" && course.CoverImageSource != "url" {
		course.CoverImageSource = "none"
	}
	if course.CoverImageSource == "uploaded" && course.CoverImagePath == "" {
		course.CoverImageSource = "none"
	}
	if course.CoverImageSource == "url" && course.CoverImageURL == "" {
		course.CoverImageSource = "none"
	}
	telegramChatID, err := NormalizeTelegramChatID(course.TelegramChatID)
	if err != nil {
		return PremiumCourse{}, err
	}
	course.TelegramChatID = telegramChatID
	if course.Slug == "" || !validSlug(course.Slug) || course.Title == "" || course.PriceKZT <= 0 {
		return PremiumCourse{}, ErrInvalidState
	}
	if !validPremiumCourseStatus(course.Status) || !validInviteType(course.InviteLinkType) || !validPremiumDuration(course.DefaultAccessDurationType) {
		return PremiumCourse{}, ErrInvalidState
	}
	if course.DefaultAccessDurationType == PremiumAccessDurationCustom && course.DefaultAccessExpiresAt != nil && !course.DefaultAccessExpiresAt.After(nowUTC()) {
		return PremiumCourse{}, ErrInvalidState
	}
	if course.SortOrder <= 0 {
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) + 1 FROM premium_courses`).Scan(&course.SortOrder); err != nil {
			return PremiumCourse{}, err
		}
	}
	if strings.TrimSpace(course.ID) == "" {
		course.ID = newID()
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO premium_courses(
				id, slug, title, description, price_kzt, status, sort_order, default_access_duration_type,
				default_access_expires_at, telegram_chat_id, invite_link_type, manual_invite_link,
				telegram_button_title, admin_notes, cover_image_url, cover_image_path, cover_image_source
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
		`, course.ID, course.Slug, course.Title, nullableString(course.Description), course.PriceKZT, course.Status, course.SortOrder,
			course.DefaultAccessDurationType, nullableTimePtr(course.DefaultAccessExpiresAt), nullableString(course.TelegramChatID),
			course.InviteLinkType, nullableString(course.ManualInviteLink), nullableString(course.TelegramButtonTitle),
			nullableString(course.AdminNotes), nullableString(course.CoverImageURL), nullableString(course.CoverImagePath), course.CoverImageSource)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "constraint") {
				return PremiumCourse{}, ErrInvalidState
			}
			return PremiumCourse{}, err
		}
		return s.GetPremiumCourse(ctx, course.ID, false)
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE premium_courses
		SET slug=?, title=?, description=?, price_kzt=?, status=?, sort_order=?, default_access_duration_type=?,
			default_access_expires_at=?, telegram_chat_id=?, invite_link_type=?, manual_invite_link=?,
			telegram_button_title=?, admin_notes=?, cover_image_url=?, cover_image_path=?, cover_image_source=?,
			updated_at=CURRENT_TIMESTAMP
		WHERE id=?;
	`, course.Slug, course.Title, nullableString(course.Description), course.PriceKZT, course.Status, course.SortOrder,
		course.DefaultAccessDurationType, nullableTimePtr(course.DefaultAccessExpiresAt), nullableString(course.TelegramChatID),
		course.InviteLinkType, nullableString(course.ManualInviteLink), nullableString(course.TelegramButtonTitle),
		nullableString(course.AdminNotes), nullableString(course.CoverImageURL), nullableString(course.CoverImagePath),
		course.CoverImageSource, course.ID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "constraint") {
			return PremiumCourse{}, ErrInvalidState
		}
		return PremiumCourse{}, err
	}
	return s.GetPremiumCourse(ctx, course.ID, false)
}

func (s *Store) ArchivePremiumCourse(ctx context.Context, courseID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE premium_courses SET status = 'archived', updated_at = CURRENT_TIMESTAMP WHERE id = ?`, courseID)
	return err
}

func (s *Store) ListPremiumCourseLessons(ctx context.Context, userID, courseID string) ([]PremiumCourseLesson, error) {
	access := false
	if userID != "" {
		access, _ = s.HasPremiumCourseAccess(ctx, userID, courseID, nowUTC())
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, course_id, title, COALESCE(description, ''), COALESCE(video_url, ''), COALESCE(content_text, ''),
			position, is_preview, is_active, created_at, updated_at
		FROM premium_course_lessons
		WHERE course_id = ? AND is_active = 1
		ORDER BY position ASC, created_at ASC;
	`, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lessons []PremiumCourseLesson
	for rows.Next() {
		lesson, err := scanPremiumCourseLesson(rows)
		if err != nil {
			return nil, err
		}
		lesson.Access = lesson.IsPreview || access
		if !lesson.Access {
			lesson.VideoURL = ""
			lesson.ContentText = ""
		}
		lessons = append(lessons, lesson)
	}
	return lessons, rows.Err()
}

func (s *Store) GetPremiumCourseLesson(ctx context.Context, userID, lessonID string) (PremiumCourseLesson, error) {
	lesson, err := scanPremiumCourseLesson(s.db.QueryRowContext(ctx, `
		SELECT l.id, l.course_id, l.title, COALESCE(l.description, ''), COALESCE(l.video_url, ''), COALESCE(l.content_text, ''),
			l.position, l.is_preview, l.is_active, l.created_at, l.updated_at
		FROM premium_course_lessons l
		JOIN premium_courses c ON c.id = l.course_id
		WHERE l.id = ? AND l.is_active = 1 AND c.status = 'active';
	`, lessonID))
	if err != nil {
		return PremiumCourseLesson{}, rowErr(err)
	}
	access := lesson.IsPreview
	if !access {
		access, err = s.HasPremiumCourseAccess(ctx, userID, lesson.CourseID, nowUTC())
		if err != nil {
			return PremiumCourseLesson{}, err
		}
	}
	lesson.Access = access
	if !access {
		lesson.VideoURL = ""
		lesson.ContentText = ""
		return lesson, ErrForbidden
	}
	return lesson, nil
}

func (s *Store) HasPremiumCourseAccess(ctx context.Context, userID, courseID string, at time.Time) (bool, error) {
	return hasPremiumCourseAccessQuery(ctx, s.db, userID, courseID, at)
}

func (s *Store) ActivePremiumCourseAccess(ctx context.Context, userID, courseID string) (*UserCourseAccess, error) {
	access, err := scanUserCourseAccess(s.db.QueryRowContext(ctx, userCourseAccessSelectSQL+`
		WHERE user_id = ? AND course_id = ? AND access_status = 'active'
			AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
		ORDER BY granted_at DESC
		LIMIT 1;
	`, userID, courseID))
	if err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &access, nil
}

func hasPremiumCourseAccessQuery(ctx context.Context, q queryer, userID, courseID string, at time.Time) (bool, error) {
	var count int
	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(a.id)
		FROM user_course_access a
		JOIN users u ON u.id = a.user_id
		WHERE a.user_id = ? AND a.course_id = ? AND a.access_status = 'active'
			AND u.access_closed = 0
			AND (a.expires_at IS NULL OR a.expires_at > ?);
	`, userID, courseID, at.UTC()).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) GrantPremiumCourseAccess(ctx context.Context, userID, courseID, source string, adminID int64, paymentID *string, durationType string, customExpiresAt *time.Time) (UserCourseAccess, error) {
	var access UserCourseAccess
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		out, err := s.grantPremiumCourseAccessTx(ctx, tx, userID, courseID, source, adminID, paymentID, durationType, customExpiresAt)
		if err != nil {
			return err
		}
		access = out
		return nil
	})
	return access, err
}

func (s *Store) grantPremiumCourseAccessTx(ctx context.Context, tx *sql.Tx, userID, courseID, source string, adminID int64, paymentID *string, durationType string, customExpiresAt *time.Time) (UserCourseAccess, error) {
	if source == "" {
		source = PremiumAccessSourceManual
	}
	if !validPremiumAccessSource(source) {
		return UserCourseAccess{}, ErrInvalidState
	}
	course, err := scanPremiumCourse(tx.QueryRowContext(ctx, premiumCourseSelectSQL+` WHERE id = ?;`, courseID))
	if err != nil {
		return UserCourseAccess{}, rowErr(err)
	}
	if durationType == "" {
		durationType = course.DefaultAccessDurationType
	}
	if !validPremiumDuration(durationType) {
		return UserCourseAccess{}, ErrInvalidState
	}
	now := nowUTC()
	expiresAt, err := premiumAccessExpiry(course, durationType, customExpiresAt, now)
	if err != nil {
		return UserCourseAccess{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE user_course_access
		SET access_status = 'expired', updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ? AND course_id = ? AND access_status = 'active'
			AND expires_at IS NOT NULL AND expires_at <= CURRENT_TIMESTAMP;
	`, userID, courseID); err != nil {
		return UserCourseAccess{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE user_course_access
		SET access_status = 'revoked', revoked_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ? AND course_id = ? AND access_status = 'active';
	`, now, userID, courseID); err != nil {
		return UserCourseAccess{}, err
	}
	var adminValue any
	if adminID != 0 {
		adminValue = adminID
	}
	accessID := newID()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO user_course_access(
			id, user_id, course_id, access_status, access_source, granted_by_admin_id, payment_id, granted_at, expires_at
		)
		VALUES (?, ?, ?, 'active', ?, ?, ?, ?, ?);
	`, accessID, userID, courseID, source, adminValue, nullableStringPtrValue(paymentID), now, nullableTimePtr(expiresAt))
	if err != nil {
		return UserCourseAccess{}, err
	}
	return scanUserCourseAccess(tx.QueryRowContext(ctx, userCourseAccessSelectSQL+` WHERE id = ?;`, accessID))
}

func premiumAccessExpiry(course PremiumCourse, durationType string, customExpiresAt *time.Time, now time.Time) (*time.Time, error) {
	switch durationType {
	case PremiumAccessDurationLifetime:
		return nil, nil
	case PremiumAccessDuration30Days:
		value := now.AddDate(0, 0, 30)
		return &value, nil
	case PremiumAccessDuration90Days:
		value := now.AddDate(0, 0, 90)
		return &value, nil
	case PremiumAccessDurationCustom:
		if customExpiresAt != nil {
			value := customExpiresAt.UTC()
			if !value.After(now) {
				return nil, ErrInvalidState
			}
			return &value, nil
		}
		if course.DefaultAccessExpiresAt != nil {
			value := course.DefaultAccessExpiresAt.UTC()
			if !value.After(now) {
				return nil, ErrInvalidState
			}
			return &value, nil
		}
		return nil, nil
	default:
		return nil, ErrInvalidState
	}
}

func (s *Store) RevokePremiumCourseAccess(ctx context.Context, userID, courseID string, adminID int64) error {
	now := nowUTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE user_course_access
		SET access_status = 'revoked', revoked_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ? AND course_id = ? AND access_status = 'active';
	`, now, userID, courseID)
	return err
}

func (s *Store) ExpirePremiumCourseAccesses(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE user_course_access
		SET access_status = 'expired', updated_at = CURRENT_TIMESTAMP
		WHERE access_status = 'active' AND expires_at IS NOT NULL AND expires_at <= CURRENT_TIMESTAMP;
	`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) ListUserPremiumCourseAccess(ctx context.Context, userID string) ([]PremiumCourseAccessView, error) {
	rows, err := s.db.QueryContext(ctx, premiumCourseSelectSQL+`
		WHERE status <> 'archived'
		ORDER BY sort_order ASC, created_at ASC;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PremiumCourseAccessView
	for rows.Next() {
		course, err := scanPremiumCourse(rows)
		if err != nil {
			return nil, err
		}
		view := PremiumCourseAccessView{Course: course}
		access, err := scanUserCourseAccess(s.db.QueryRowContext(ctx, userCourseAccessSelectSQL+`
			WHERE user_id = ? AND course_id = ?
			ORDER BY CASE WHEN access_status = 'active' THEN 0 ELSE 1 END, created_at DESC
			LIMIT 1;
		`, userID, course.ID))
		if err == nil {
			view.Access = &access
		} else if err != ErrNotFound {
			return nil, err
		}
		view.Active, _ = s.HasPremiumCourseAccess(ctx, userID, course.ID, nowUTC())
		result = append(result, view)
	}
	return result, rows.Err()
}

func (s *Store) ReusablePremiumCourseTelegramInvite(ctx context.Context, userID, courseID, telegramChatID string) (*PremiumCourseTelegramInvite, error) {
	invite, err := scanPremiumCourseTelegramInvite(s.db.QueryRowContext(ctx, premiumCourseInviteSelectSQL+`
		WHERE user_id = ? AND course_id = ? AND telegram_chat_id = ? AND status = 'issued'
			AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
		ORDER BY created_at DESC
		LIMIT 1;
	`, userID, courseID, telegramChatID))
	if err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &invite, nil
}

func (s *Store) CreatePremiumCourseTelegramInvite(ctx context.Context, invite PremiumCourseTelegramInvite) (PremiumCourseTelegramInvite, error) {
	if strings.TrimSpace(invite.UserID) == "" || strings.TrimSpace(invite.CourseID) == "" || strings.TrimSpace(invite.TelegramChatID) == "" || strings.TrimSpace(invite.InviteLink) == "" {
		return PremiumCourseTelegramInvite{}, ErrInvalidState
	}
	if invite.Status == "" {
		invite.Status = "issued"
	}
	if invite.Status != "issued" && invite.Status != "used" && invite.Status != "expired" && invite.Status != "revoked" {
		return PremiumCourseTelegramInvite{}, ErrInvalidState
	}
	invite.ID = newID()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_premium_course_telegram_invites(id, user_id, course_id, telegram_chat_id, invite_link, expires_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?);
	`, invite.ID, invite.UserID, invite.CourseID, invite.TelegramChatID, invite.InviteLink, nullableTimePtr(invite.ExpiresAt), invite.Status)
	if err != nil {
		return PremiumCourseTelegramInvite{}, err
	}
	return scanPremiumCourseTelegramInvite(s.db.QueryRowContext(ctx, premiumCourseInviteSelectSQL+` WHERE id = ?;`, invite.ID))
}

func (s *Store) ExpirePremiumCourseTelegramInvites(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE user_premium_course_telegram_invites
		SET status = 'expired', updated_at = CURRENT_TIMESTAMP
		WHERE status = 'issued' AND expires_at IS NOT NULL AND expires_at <= CURRENT_TIMESTAMP;
	`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func validSlug(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func validPremiumCourseStatus(value string) bool {
	return value == PremiumCourseStatusActive || value == PremiumCourseStatusInactive || value == PremiumCourseStatusArchived
}

func validInviteType(value string) bool {
	return value == "bot" || value == "manual"
}

func validPremiumDuration(value string) bool {
	return value == PremiumAccessDurationLifetime || value == PremiumAccessDuration30Days || value == PremiumAccessDuration90Days || value == PremiumAccessDurationCustom
}

func validPremiumAccessSource(value string) bool {
	return value == PremiumAccessSourceManual || value == PremiumAccessSourcePayment || value == PremiumAccessSourceBonus || value == PremiumAccessSourceGift
}

func premiumCourseTelegramConfigured(course PremiumCourse) bool {
	if course.InviteLinkType == "bot" {
		return strings.TrimSpace(course.TelegramChatID) != ""
	}
	return strings.TrimSpace(course.ManualInviteLink) != ""
}

const premiumCourseSelectSQL = `
	SELECT id, slug, title, COALESCE(description, ''), price_kzt, status, sort_order, default_access_duration_type,
		default_access_expires_at, COALESCE(telegram_chat_id, ''), invite_link_type, COALESCE(manual_invite_link, ''),
		COALESCE(telegram_button_title, ''), COALESCE(admin_notes, ''), COALESCE(cover_image_url, ''),
		COALESCE(cover_image_path, ''), COALESCE(cover_image_source, 'none'), created_at, updated_at
	FROM premium_courses`

func scanPremiumCourse(row interface{ Scan(dest ...any) error }) (PremiumCourse, error) {
	var course PremiumCourse
	var defaultExpires sql.NullTime
	if err := row.Scan(
		&course.ID,
		&course.Slug,
		&course.Title,
		&course.Description,
		&course.PriceKZT,
		&course.Status,
		&course.SortOrder,
		&course.DefaultAccessDurationType,
		&defaultExpires,
		&course.TelegramChatID,
		&course.InviteLinkType,
		&course.ManualInviteLink,
		&course.TelegramButtonTitle,
		&course.AdminNotes,
		&course.CoverImageURL,
		&course.CoverImagePath,
		&course.CoverImageSource,
		&course.CreatedAt,
		&course.UpdatedAt,
	); err != nil {
		return PremiumCourse{}, rowErr(err)
	}
	course.DefaultAccessExpiresAt = scanTime(defaultExpires)
	course.TelegramConfigured = premiumCourseTelegramConfigured(course)
	return course, nil
}

func scanPremiumCourseLesson(row interface{ Scan(dest ...any) error }) (PremiumCourseLesson, error) {
	var lesson PremiumCourseLesson
	var preview, active int
	if err := row.Scan(&lesson.ID, &lesson.CourseID, &lesson.Title, &lesson.Description, &lesson.VideoURL, &lesson.ContentText, &lesson.Position, &preview, &active, &lesson.CreatedAt, &lesson.UpdatedAt); err != nil {
		return PremiumCourseLesson{}, rowErr(err)
	}
	lesson.IsPreview = preview == 1
	lesson.IsActive = active == 1
	return lesson, nil
}

const userCourseAccessSelectSQL = `
	SELECT id, user_id, course_id, access_status, access_source, granted_by_admin_id, payment_id,
		granted_at, expires_at, revoked_at, created_at, updated_at
	FROM user_course_access`

func scanUserCourseAccess(row interface{ Scan(dest ...any) error }) (UserCourseAccess, error) {
	var access UserCourseAccess
	var adminID sql.NullInt64
	var paymentID sql.NullString
	var expiresAt, revokedAt sql.NullTime
	if err := row.Scan(&access.ID, &access.UserID, &access.CourseID, &access.AccessStatus, &access.AccessSource, &adminID, &paymentID, &access.GrantedAt, &expiresAt, &revokedAt, &access.CreatedAt, &access.UpdatedAt); err != nil {
		return UserCourseAccess{}, rowErr(err)
	}
	access.GrantedByAdminID = scanInt64(adminID)
	access.PaymentID = scanStringPtr(paymentID)
	access.ExpiresAt = scanTime(expiresAt)
	access.RevokedAt = scanTime(revokedAt)
	return access, nil
}

const premiumCourseInviteSelectSQL = `
	SELECT id, user_id, course_id, telegram_chat_id, invite_link, expires_at, status, created_at, updated_at
	FROM user_premium_course_telegram_invites`

func scanPremiumCourseTelegramInvite(row interface{ Scan(dest ...any) error }) (PremiumCourseTelegramInvite, error) {
	var invite PremiumCourseTelegramInvite
	var expiresAt sql.NullTime
	if err := row.Scan(&invite.ID, &invite.UserID, &invite.CourseID, &invite.TelegramChatID, &invite.InviteLink, &expiresAt, &invite.Status, &invite.CreatedAt, &invite.UpdatedAt); err != nil {
		return PremiumCourseTelegramInvite{}, rowErr(err)
	}
	invite.ExpiresAt = scanTime(expiresAt)
	return invite, nil
}
