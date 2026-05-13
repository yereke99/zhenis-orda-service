package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
)

func (s *Store) CanAccessLevel(ctx context.Context, userID string, levelNumber int) (bool, error) {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return false, err
	}
	if user.AccessClosed || levelNumber < 1 || levelNumber > 12 {
		return false, nil
	}
	sub, err := s.GetActiveSubscription(ctx, userID)
	if err != nil || sub == nil {
		return false, err
	}
	return user.CurrentLevel >= levelNumber, nil
}

func (s *Store) CanUnlockNextLevel(ctx context.Context, userID string, currentLevel int) (bool, error) {
	return s.canUnlockNextLevel(ctx, s.db, userID, currentLevel)
}

func (s *Store) canUnlockNextLevel(ctx context.Context, q queryer, userID string, currentLevel int) (bool, error) {
	if currentLevel < 1 || currentLevel >= 12 {
		return false, nil
	}
	active, err := hasActiveSubscription(ctx, q, userID)
	if err != nil || !active {
		return false, err
	}
	var totalLessons, watchedLessons int
	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(l.id), COALESCE(SUM(CASE WHEN lp.watched = 1 THEN 1 ELSE 0 END), 0)
		FROM levels lv
		JOIN lessons l ON l.level_id = lv.id AND l.is_active = 1
		LEFT JOIN lesson_progress lp ON lp.lesson_id = l.id AND lp.user_id = ?
		WHERE lv.number = ? AND lv.is_active = 1;
	`, userID, currentLevel).Scan(&totalLessons, &watchedLessons); err != nil {
		return false, err
	}
	if totalLessons == 0 || watchedLessons < totalLessons {
		return false, nil
	}
	passed, err := testPassed(ctx, q, userID, currentLevel)
	if err != nil {
		return false, err
	}
	return passed, nil
}

func (s *Store) RecalculateUserProgress(ctx context.Context, userID string) (Progress, error) {
	var progress Progress
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var currentLevel int
		if err := tx.QueryRowContext(ctx, `SELECT current_level FROM users WHERE id = ?`, userID).Scan(&currentLevel); err != nil {
			return rowErr(err)
		}
		if currentLevel == 0 {
			progress = Progress{LevelNumber: 0, Percent: 0, NextRequirement: "Төлем жасаңыз."}
			return nil
		}
		for currentLevel < 12 {
			ok, err := s.canUnlockNextLevel(ctx, tx, userID, currentLevel)
			if err != nil || !ok {
				if err != nil {
					return err
				}
				break
			}
			currentLevel++
			if _, err := tx.ExecContext(ctx, `UPDATE users SET current_level = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND current_level < ?`, currentLevel, userID, currentLevel); err != nil {
				return err
			}
		}
		p, err := s.progressForLevel(ctx, tx, userID, currentLevel)
		if err != nil {
			return err
		}
		progress = p
		return nil
	})
	return progress, err
}

func (s *Store) CurrentProgress(ctx context.Context, userID string) (Progress, error) {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return Progress{}, err
	}
	if user.CurrentLevel == 0 {
		return Progress{LevelNumber: 0, Percent: 0, NextRequirement: "Төлем жасаңыз."}, nil
	}
	return s.progressForLevel(ctx, s.db, userID, user.CurrentLevel)
}

func (s *Store) progressForLevel(ctx context.Context, q queryer, userID string, levelNumber int) (Progress, error) {
	var progress Progress
	progress.LevelNumber = levelNumber
	active, err := hasActiveSubscription(ctx, q, userID)
	if err != nil {
		return progress, err
	}
	progress.SubscriptionOK = active
	if !active {
		progress.NextRequirement = "Жазылымыңызды ұзартыңыз."
	}

	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(l.id), COALESCE(SUM(CASE WHEN lp.watched = 1 THEN 1 ELSE 0 END), 0)
		FROM levels lv
		JOIN lessons l ON l.level_id = lv.id AND l.is_active = 1
		LEFT JOIN lesson_progress lp ON lp.lesson_id = l.id AND lp.user_id = ?
		WHERE lv.number = ? AND lv.is_active = 1;
	`, userID, levelNumber).Scan(&progress.TotalLessons, &progress.WatchedLessons); err != nil {
		return progress, err
	}
	progress.TestPassed, err = testPassed(ctx, q, userID, levelNumber)
	if err != nil {
		return progress, err
	}
	progress.AssignmentDone, err = assignmentSubmitted(ctx, q, userID, levelNumber)
	if err != nil {
		return progress, err
	}
	hasAssignment, err := hasAssignment(ctx, q, levelNumber)
	if err != nil {
		return progress, err
	}

	totalUnits := progress.TotalLessons + 1
	doneUnits := progress.WatchedLessons
	if progress.TestPassed {
		doneUnits++
	}
	if hasAssignment {
		totalUnits++
		if progress.AssignmentDone {
			doneUnits++
		}
	}
	if totalUnits > 0 {
		progress.Percent = int(math.Round(float64(doneUnits) / float64(totalUnits) * 100))
	}
	progress.CanUnlockNext = active && progress.TotalLessons > 0 && progress.WatchedLessons >= progress.TotalLessons && progress.TestPassed && levelNumber < 12
	progress.Completed = progress.CanUnlockNext || (levelNumber == 12 && progress.TestPassed)
	if progress.NextRequirement == "" {
		switch {
		case progress.TotalLessons > 0 && progress.WatchedLessons < progress.TotalLessons:
			progress.NextRequirement = fmt.Sprintf("LEVEL %d ашылуы үшін барлық сабақтарды өтіңіз.", levelNumber+1)
		case !progress.TestPassed:
			progress.NextRequirement = fmt.Sprintf("LEVEL %d ашылуы үшін тест тапсырыңыз.", levelNumber+1)
		case levelNumber >= 12:
			progress.NextRequirement = "MASTER LEVEL аяқталды. Сертификат статусы дайындалады."
		default:
			progress.NextRequirement = fmt.Sprintf("LEVEL %d ашуға дайын.", levelNumber+1)
		}
	}
	return progress, nil
}

func (s *Store) ListLevels(ctx context.Context, userID string) ([]Level, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, number, title_kk, title_ru, COALESCE(description_kk, ''), COALESCE(description_ru, ''), sort_order, is_active
		FROM levels
		WHERE is_active = 1
		ORDER BY number ASC;
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
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range levels {
		levels[i].Access, _ = s.CanAccessLevel(ctx, userID, levels[i].Number)
		levels[i].Progress, _ = s.progressForLevel(ctx, s.db, userID, levels[i].Number)
	}
	return levels, nil
}

func (s *Store) GetLevel(ctx context.Context, userID string, levelNumber int) (Level, error) {
	var level Level
	var active int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, number, title_kk, title_ru, COALESCE(description_kk, ''), COALESCE(description_ru, ''), sort_order, is_active
		FROM levels
		WHERE number = ?;
	`, levelNumber).Scan(&level.ID, &level.Number, &level.TitleKK, &level.TitleRU, &level.DescriptionKK, &level.DescriptionRU, &level.SortOrder, &active)
	if err != nil {
		return Level{}, rowErr(err)
	}
	level.IsActive = active == 1
	level.Access, err = s.CanAccessLevel(ctx, userID, levelNumber)
	if err != nil {
		return Level{}, err
	}
	level.Progress, _ = s.progressForLevel(ctx, s.db, userID, levelNumber)
	level.Lessons, err = s.ListLessons(ctx, userID, levelNumber)
	if err != nil {
		return Level{}, err
	}
	return level, nil
}

func (s *Store) ListLessons(ctx context.Context, userID string, levelNumber int) ([]Lesson, error) {
	args := []any{userID}
	where := `WHERE l.is_active = 1`
	if levelNumber > 0 {
		where += ` AND lv.number = ?`
		args = append(args, levelNumber)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id, l.level_id, lv.number, l.title_kk, l.title_ru, COALESCE(l.description_kk, ''), COALESCE(l.description_ru, ''),
			COALESCE(l.video_url, ''), l.sort_order, l.is_active,
			COALESCE(lp.watched, 0), lp.watched_at
		FROM lessons l
		JOIN levels lv ON lv.id = l.level_id
		LEFT JOIN lesson_progress lp ON lp.lesson_id = l.id AND lp.user_id = ?
		`+where+`
		ORDER BY lv.number ASC, l.sort_order ASC;
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lessons []Lesson
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
		lessons = append(lessons, lesson)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range lessons {
		lessons[i].Access, _ = s.CanAccessLevel(ctx, userID, lessons[i].LevelNumber)
	}
	return lessons, nil
}

func (s *Store) GetLesson(ctx context.Context, userID, lessonID string) (Lesson, error) {
	var lesson Lesson
	var active, watched int
	var watchedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT l.id, l.level_id, lv.number, l.title_kk, l.title_ru, COALESCE(l.description_kk, ''), COALESCE(l.description_ru, ''),
			COALESCE(l.video_url, ''), l.sort_order, l.is_active,
			COALESCE(lp.watched, 0), lp.watched_at
		FROM lessons l
		JOIN levels lv ON lv.id = l.level_id
		LEFT JOIN lesson_progress lp ON lp.lesson_id = l.id AND lp.user_id = ?
		WHERE l.id = ?;
	`, userID, lessonID).Scan(&lesson.ID, &lesson.LevelID, &lesson.LevelNumber, &lesson.TitleKK, &lesson.TitleRU, &lesson.DescriptionKK, &lesson.DescriptionRU, &lesson.VideoURL, &lesson.SortOrder, &active, &watched, &watchedAt)
	if err != nil {
		return Lesson{}, rowErr(err)
	}
	lesson.IsActive = active == 1
	lesson.Watched = watched == 1
	lesson.WatchedAt = scanTime(watchedAt)
	access, err := s.CanAccessLevel(ctx, userID, lesson.LevelNumber)
	if err != nil {
		return Lesson{}, err
	}
	lesson.Access = access
	if !access {
		return lesson, ErrForbidden
	}
	return lesson, nil
}

func (s *Store) MarkLessonWatched(ctx context.Context, userID, lessonID string) (Progress, error) {
	var progress Progress
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var levelNumber int
		if err := tx.QueryRowContext(ctx, `
			SELECT lv.number
			FROM lessons l JOIN levels lv ON lv.id = l.level_id
			WHERE l.id = ? AND l.is_active = 1;
		`, lessonID).Scan(&levelNumber); err != nil {
			return rowErr(err)
		}
		access, err := canAccessLevelQuery(ctx, tx, userID, levelNumber)
		if err != nil || !access {
			if err != nil {
				return err
			}
			return ErrForbidden
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO lesson_progress(user_id, lesson_id, watched, watched_at, coin_granted)
			VALUES (?, ?, 1, CURRENT_TIMESTAMP, 1)
			ON CONFLICT(user_id, lesson_id) DO UPDATE SET
				watched = 1,
				watched_at = COALESCE(lesson_progress.watched_at, CURRENT_TIMESTAMP),
				coin_granted = 1,
				updated_at = CURRENT_TIMESTAMP;
		`, userID, lessonID); err != nil {
			return err
		}
		if err := s.AddCoinsTx(ctx, tx, userID, 5, "lesson_watched", "lesson", sourceID(lessonID)); err != nil {
			return err
		}
		p, err := s.recalculateUserProgressTx(ctx, tx, userID)
		if err != nil {
			return err
		}
		progress = p
		return nil
	})
	return progress, err
}

func (s *Store) GetTestByLevel(ctx context.Context, userID string, levelNumber int) (Test, error) {
	access, err := s.CanAccessLevel(ctx, userID, levelNumber)
	if err != nil {
		return Test{}, err
	}
	if !access {
		return Test{}, ErrForbidden
	}
	test, err := s.getTestByLevelQuery(ctx, s.db, levelNumber, false)
	if err != nil {
		return Test{}, err
	}
	return test, nil
}

func (s *Store) SubmitTest(ctx context.Context, userID string, levelNumber int, selected map[string]string) (TestAttempt, Progress, error) {
	var attempt TestAttempt
	var progress Progress
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		access, err := canAccessLevelQuery(ctx, tx, userID, levelNumber)
		if err != nil || !access {
			if err != nil {
				return err
			}
			return ErrForbidden
		}
		test, err := s.getTestByLevelQuery(ctx, tx, levelNumber, true)
		if err != nil {
			return err
		}
		total := len(test.Questions)
		if total == 0 {
			return ErrInvalidState
		}
		correct := 0
		correctByQuestion := map[string]bool{}
		for _, question := range test.Questions {
			selectedID := selected[question.ID]
			for _, option := range question.Options {
				if option.ID == selectedID && option.IsCorrect {
					correct++
					correctByQuestion[question.ID] = true
				}
			}
		}
		score := int(math.Round(float64(correct) / float64(total) * 100))
		passed := score >= test.PassPercent
		attemptID := newID()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO test_attempts(id, user_id, test_id, score_percent, correct_count, total_count, passed)
			VALUES (?, ?, ?, ?, ?, ?, ?);
		`, attemptID, userID, test.ID, score, correct, total, boolInt(passed)); err != nil {
			return err
		}
		for _, question := range test.Questions {
			selectedID := selected[question.ID]
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO test_answers(id, attempt_id, question_id, selected_option_id, is_correct)
				VALUES (?, ?, ?, ?, ?);
			`, newID(), attemptID, question.ID, nullableOptionID(selectedID), boolInt(correctByQuestion[question.ID])); err != nil {
				return err
			}
		}
		if passed {
			if err := s.AddCoinsTx(ctx, tx, userID, 20, "test_passed", "test", sourceID(test.ID)); err != nil {
				return err
			}
		}
		attempt = TestAttempt{ID: attemptID, UserID: userID, TestID: test.ID, ScorePercent: score, CorrectCount: correct, TotalCount: total, Passed: passed, CreatedAt: nowUTC()}
		p, err := s.recalculateUserProgressTx(ctx, tx, userID)
		if err != nil {
			return err
		}
		progress = p
		return nil
	})
	return attempt, progress, err
}

func nullableOptionID(id string) any {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	return id
}

func (s *Store) GetAssignmentByLevel(ctx context.Context, userID string, levelNumber int) (Assignment, error) {
	access, err := s.CanAccessLevel(ctx, userID, levelNumber)
	if err != nil {
		return Assignment{}, err
	}
	if !access {
		return Assignment{}, ErrForbidden
	}
	return s.getAssignmentByLevel(ctx, s.db, levelNumber)
}

func (s *Store) SubmitAssignment(ctx context.Context, userID string, levelNumber int, answerText, filePath, linkURL string) error {
	assignment, err := s.GetAssignmentByLevel(ctx, userID, levelNumber)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO assignment_submissions(id, assignment_id, user_id, answer_text, file_path, link_url, status)
		VALUES (?, ?, ?, ?, ?, ?, 'submitted');
	`, newID(), assignment.ID, userID, answerText, filePath, linkURL)
	return err
}

func (s *Store) getTestByLevelQuery(ctx context.Context, q queryer, levelNumber int, includeCorrect bool) (Test, error) {
	var test Test
	var active int
	err := q.QueryRowContext(ctx, `
		SELECT t.id, t.level_id, lv.number, t.title, t.pass_percent, t.is_active
		FROM tests t
		JOIN levels lv ON lv.id = t.level_id
		WHERE lv.number = ? AND t.is_active = 1;
	`, levelNumber).Scan(&test.ID, &test.LevelID, &test.LevelNumber, &test.Title, &test.PassPercent, &active)
	if err != nil {
		return Test{}, rowErr(err)
	}
	test.IsActive = active == 1
	rows, err := q.QueryContext(ctx, `
		SELECT q.id, q.test_id, q.question_text_kk, q.question_text_ru, q.sort_order,
			o.id, o.option_text_kk, o.option_text_ru, o.sort_order, o.is_correct
		FROM test_questions q
		JOIN test_options o ON o.question_id = q.id
		WHERE q.test_id = ? AND q.is_active = 1
		ORDER BY q.sort_order ASC, o.sort_order ASC;
	`, test.ID)
	if err != nil {
		return Test{}, err
	}
	defer rows.Close()
	questions := map[string]*TestQuestion{}
	for rows.Next() {
		var qn TestQuestion
		var opt TestOption
		var correct int
		if err := rows.Scan(&qn.ID, &qn.TestID, &qn.QuestionTextKK, &qn.QuestionTextRU, &qn.SortOrder, &opt.ID, &opt.OptionTextKK, &opt.OptionTextRU, &opt.SortOrder, &correct); err != nil {
			return Test{}, err
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
		return Test{}, err
	}
	for _, question := range questions {
		test.Questions = append(test.Questions, *question)
	}
	sort.Slice(test.Questions, func(i, j int) bool { return test.Questions[i].SortOrder < test.Questions[j].SortOrder })
	return test, nil
}

func (s *Store) getAssignmentByLevel(ctx context.Context, q queryer, levelNumber int) (Assignment, error) {
	var a Assignment
	var active int
	err := q.QueryRowContext(ctx, `
		SELECT a.id, a.level_id, lv.number, a.title_kk, a.title_ru, COALESCE(a.description_kk, ''), COALESCE(a.description_ru, ''), a.is_active
		FROM assignments a
		JOIN levels lv ON lv.id = a.level_id
		WHERE lv.number = ? AND a.is_active = 1;
	`, levelNumber).Scan(&a.ID, &a.LevelID, &a.LevelNumber, &a.TitleKK, &a.TitleRU, &a.DescriptionKK, &a.DescriptionRU, &active)
	if err != nil {
		return Assignment{}, rowErr(err)
	}
	a.IsActive = active == 1
	return a, nil
}

func (s *Store) recalculateUserProgressTx(ctx context.Context, tx *sql.Tx, userID string) (Progress, error) {
	var currentLevel int
	if err := tx.QueryRowContext(ctx, `SELECT current_level FROM users WHERE id = ?`, userID).Scan(&currentLevel); err != nil {
		return Progress{}, rowErr(err)
	}
	if currentLevel == 0 {
		return Progress{LevelNumber: 0, Percent: 0, NextRequirement: "Төлем жасаңыз."}, nil
	}
	for currentLevel < 12 {
		ok, err := s.canUnlockNextLevel(ctx, tx, userID, currentLevel)
		if err != nil || !ok {
			if err != nil {
				return Progress{}, err
			}
			break
		}
		currentLevel++
		if _, err := tx.ExecContext(ctx, `UPDATE users SET current_level = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND current_level < ?`, currentLevel, userID, currentLevel); err != nil {
			return Progress{}, err
		}
	}
	return s.progressForLevel(ctx, tx, userID, currentLevel)
}

func testPassed(ctx context.Context, q queryer, userID string, levelNumber int) (bool, error) {
	var passed int
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(ta.passed), 0)
		FROM tests t
		JOIN levels lv ON lv.id = t.level_id
		LEFT JOIN test_attempts ta ON ta.test_id = t.id AND ta.user_id = ?
		WHERE lv.number = ? AND t.is_active = 1;
	`, userID, levelNumber).Scan(&passed)
	if err != nil {
		return false, err
	}
	return passed == 1, nil
}

func assignmentSubmitted(ctx context.Context, q queryer, userID string, levelNumber int) (bool, error) {
	var submitted int
	err := q.QueryRowContext(ctx, `
		SELECT CASE WHEN COUNT(s.id) > 0 THEN 1 ELSE 0 END
		FROM assignments a
		JOIN levels lv ON lv.id = a.level_id
		LEFT JOIN assignment_submissions s ON s.assignment_id = a.id AND s.user_id = ?
		WHERE lv.number = ? AND a.is_active = 1;
	`, userID, levelNumber).Scan(&submitted)
	if err != nil {
		return false, err
	}
	return submitted == 1, nil
}

func hasAssignment(ctx context.Context, q queryer, levelNumber int) (bool, error) {
	var count int
	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(a.id)
		FROM assignments a
		JOIN levels lv ON lv.id = a.level_id
		WHERE lv.number = ? AND a.is_active = 1;
	`, levelNumber).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func hasActiveSubscription(ctx context.Context, q queryer, userID string) (bool, error) {
	var count int
	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(id)
		FROM subscriptions
		WHERE user_id = ? AND status = 'active' AND expires_at > CURRENT_TIMESTAMP;
	`, userID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func canAccessLevelQuery(ctx context.Context, q queryer, userID string, levelNumber int) (bool, error) {
	var currentLevel, accessClosed int
	if err := q.QueryRowContext(ctx, `SELECT current_level, access_closed FROM users WHERE id = ?`, userID).Scan(&currentLevel, &accessClosed); err != nil {
		return false, rowErr(err)
	}
	if accessClosed == 1 || currentLevel < levelNumber || levelNumber < 1 || levelNumber > 12 {
		return false, nil
	}
	return hasActiveSubscription(ctx, q, userID)
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func ParseSelectedAnswers(raw map[string]string) map[string]string {
	selected := make(map[string]string, len(raw))
	for questionID, optionID := range raw {
		id := strings.TrimSpace(questionID)
		optionID = strings.TrimSpace(optionID)
		if IsUUID(id) && IsUUID(optionID) {
			selected[id] = optionID
		}
	}
	return selected
}
