package repository

import (
	"context"
	"database/sql"
	"strings"
)

func (s *Store) ListFreeLessons(ctx context.Context, activeOnly bool) ([]FreeLesson, error) {
	where := ""
	if activeOnly {
		where = "WHERE is_active = 1"
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, short_description, description, COALESCE(image_url, ''), COALESCE(image_file_path, ''),
			image_source, youtube_url, youtube_video_id, youtube_embed_url, sort_order, is_active, created_at, updated_at
		FROM free_lessons
		`+where+`
		ORDER BY sort_order ASC, created_at DESC;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lessons := []FreeLesson{}
	for rows.Next() {
		lesson, err := scanFreeLessonRow(rows)
		if err != nil {
			return nil, err
		}
		lessons = append(lessons, lesson)
	}
	return lessons, rows.Err()
}

func (s *Store) GetFreeLesson(ctx context.Context, lessonID string, activeOnly bool) (FreeLesson, error) {
	where := "WHERE id = ?"
	if activeOnly {
		where += " AND is_active = 1"
	}
	lesson, err := scanFreeLessonRow(s.db.QueryRowContext(ctx, `
		SELECT id, title, short_description, description, COALESCE(image_url, ''), COALESCE(image_file_path, ''),
			image_source, youtube_url, youtube_video_id, youtube_embed_url, sort_order, is_active, created_at, updated_at
		FROM free_lessons
		`+where+`;
	`, lessonID))
	return lesson, rowErr(err)
}

func (s *Store) UpsertFreeLesson(ctx context.Context, lesson FreeLesson) (FreeLesson, error) {
	lesson.Title = strings.TrimSpace(lesson.Title)
	lesson.ShortDescription = strings.TrimSpace(lesson.ShortDescription)
	lesson.Description = strings.TrimSpace(lesson.Description)
	lesson.ImageURL = strings.TrimSpace(lesson.ImageURL)
	lesson.ImageFilePath = strings.TrimSpace(lesson.ImageFilePath)
	lesson.ImageSource = strings.TrimSpace(lesson.ImageSource)
	lesson.YouTubeURL = strings.TrimSpace(lesson.YouTubeURL)
	lesson.YouTubeVideoID = strings.TrimSpace(lesson.YouTubeVideoID)
	lesson.YouTubeEmbedURL = strings.TrimSpace(lesson.YouTubeEmbedURL)
	if lesson.Title == "" || lesson.Description == "" || lesson.YouTubeURL == "" || lesson.YouTubeVideoID == "" || lesson.YouTubeEmbedURL == "" {
		return FreeLesson{}, ErrInvalidState
	}
	if lesson.ImageURL == "" && lesson.ImageFilePath == "" {
		return FreeLesson{}, ErrInvalidState
	}
	if lesson.ShortDescription == "" {
		lesson.ShortDescription = lesson.Description
	}
	if lesson.ImageSource == "" {
		switch {
		case lesson.ImageFilePath != "":
			lesson.ImageSource = "uploaded"
		case lesson.ImageURL != "":
			lesson.ImageSource = "url"
		default:
			lesson.ImageSource = "none"
		}
	}
	if lesson.ImageSource != "uploaded" && lesson.ImageSource != "url" {
		lesson.ImageSource = "none"
	}
	if lesson.ImageSource == "uploaded" && lesson.ImageFilePath == "" {
		lesson.ImageSource = "none"
	}
	if lesson.ImageSource == "url" && lesson.ImageURL == "" {
		lesson.ImageSource = "none"
	}
	if lesson.SortOrder <= 0 {
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) + 1 FROM free_lessons`).Scan(&lesson.SortOrder); err != nil {
			return FreeLesson{}, err
		}
	}
	if strings.TrimSpace(lesson.ID) == "" {
		lesson.ID = newID()
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO free_lessons(id, title, short_description, description, image_url, image_file_path, image_source,
				youtube_url, youtube_video_id, youtube_embed_url, sort_order, is_active)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
		`, lesson.ID, lesson.Title, lesson.ShortDescription, lesson.Description, nullableString(lesson.ImageURL), nullableString(lesson.ImageFilePath),
			lesson.ImageSource, lesson.YouTubeURL, lesson.YouTubeVideoID, lesson.YouTubeEmbedURL, lesson.SortOrder, boolInt(lesson.IsActive))
		if err != nil {
			return FreeLesson{}, err
		}
		return s.GetFreeLesson(ctx, lesson.ID, false)
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE free_lessons
		SET title=?, short_description=?, description=?, image_url=?, image_file_path=?, image_source=?,
			youtube_url=?, youtube_video_id=?, youtube_embed_url=?, sort_order=?, is_active=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?;
	`, lesson.Title, lesson.ShortDescription, lesson.Description, nullableString(lesson.ImageURL), nullableString(lesson.ImageFilePath),
		lesson.ImageSource, lesson.YouTubeURL, lesson.YouTubeVideoID, lesson.YouTubeEmbedURL, lesson.SortOrder, boolInt(lesson.IsActive), lesson.ID)
	if err != nil {
		return FreeLesson{}, err
	}
	return s.GetFreeLesson(ctx, lesson.ID, false)
}

func (s *Store) ArchiveFreeLesson(ctx context.Context, lessonID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE free_lessons SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, lessonID)
	return err
}

func scanFreeLessonRow(row interface {
	Scan(dest ...any) error
}) (FreeLesson, error) {
	var lesson FreeLesson
	var active int
	err := row.Scan(
		&lesson.ID,
		&lesson.Title,
		&lesson.ShortDescription,
		&lesson.Description,
		&lesson.ImageURL,
		&lesson.ImageFilePath,
		&lesson.ImageSource,
		&lesson.YouTubeURL,
		&lesson.YouTubeVideoID,
		&lesson.YouTubeEmbedURL,
		&lesson.SortOrder,
		&active,
		&lesson.CreatedAt,
		&lesson.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return FreeLesson{}, err
		}
		return FreeLesson{}, err
	}
	lesson.IsActive = active == 1
	return lesson, nil
}
