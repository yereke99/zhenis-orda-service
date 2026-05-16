package repository

import (
	"context"
	"database/sql"
	"strings"
)

func (s *Store) ListBooks(ctx context.Context, activeOnly bool) ([]Book, error) {
	where := ""
	if activeOnly {
		where = "WHERE is_active = 1"
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, description, price_kzt, COALESCE(image_url, ''), COALESCE(image_file_path, ''),
			image_source, sort_order, is_active, created_at, updated_at
		FROM books
		`+where+`
		ORDER BY sort_order ASC, created_at DESC;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	books := []Book{}
	for rows.Next() {
		book, err := scanBookRow(rows)
		if err != nil {
			return nil, err
		}
		books = append(books, book)
	}
	return books, rows.Err()
}

func (s *Store) GetBook(ctx context.Context, bookID string, activeOnly bool) (Book, error) {
	where := "WHERE id = ?"
	if activeOnly {
		where += " AND is_active = 1"
	}
	book, err := scanBookRow(s.db.QueryRowContext(ctx, `
		SELECT id, title, description, price_kzt, COALESCE(image_url, ''), COALESCE(image_file_path, ''),
			image_source, sort_order, is_active, created_at, updated_at
		FROM books
		`+where+`;
	`, bookID))
	return book, rowErr(err)
}

func (s *Store) UpsertBook(ctx context.Context, book Book) (Book, error) {
	book.Title = strings.TrimSpace(book.Title)
	book.Description = strings.TrimSpace(book.Description)
	book.ImageURL = strings.TrimSpace(book.ImageURL)
	book.ImageFilePath = strings.TrimSpace(book.ImageFilePath)
	book.ImageSource = strings.TrimSpace(book.ImageSource)
	if book.Title == "" || book.Description == "" || book.PriceKZT <= 0 {
		return Book{}, ErrInvalidState
	}
	if book.ImageURL == "" && book.ImageFilePath == "" {
		return Book{}, ErrInvalidState
	}
	if book.ImageSource == "" {
		switch {
		case book.ImageFilePath != "":
			book.ImageSource = "uploaded"
		case book.ImageURL != "":
			book.ImageSource = "url"
		default:
			book.ImageSource = "none"
		}
	}
	if book.ImageSource != "uploaded" && book.ImageSource != "url" {
		book.ImageSource = "none"
	}
	if book.ImageSource == "uploaded" && book.ImageFilePath == "" {
		book.ImageSource = "none"
	}
	if book.ImageSource == "url" && book.ImageURL == "" {
		book.ImageSource = "none"
	}
	if book.SortOrder <= 0 {
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) + 1 FROM books`).Scan(&book.SortOrder); err != nil {
			return Book{}, err
		}
	}
	if strings.TrimSpace(book.ID) == "" {
		book.ID = newID()
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO books(id, title, description, price_kzt, image_url, image_file_path, image_source, sort_order, is_active)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
		`, book.ID, book.Title, book.Description, book.PriceKZT, nullableString(book.ImageURL), nullableString(book.ImageFilePath), book.ImageSource, book.SortOrder, boolInt(book.IsActive))
		if err != nil {
			return Book{}, err
		}
		return s.GetBook(ctx, book.ID, false)
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE books
		SET title=?, description=?, price_kzt=?, image_url=?, image_file_path=?, image_source=?,
			sort_order=?, is_active=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?;
	`, book.Title, book.Description, book.PriceKZT, nullableString(book.ImageURL), nullableString(book.ImageFilePath), book.ImageSource, book.SortOrder, boolInt(book.IsActive), book.ID)
	if err != nil {
		return Book{}, err
	}
	return s.GetBook(ctx, book.ID, false)
}

func (s *Store) ArchiveBook(ctx context.Context, bookID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE books SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, bookID)
	return err
}

func scanBookRow(row interface {
	Scan(dest ...any) error
}) (Book, error) {
	var book Book
	var active int
	err := row.Scan(
		&book.ID,
		&book.Title,
		&book.Description,
		&book.PriceKZT,
		&book.ImageURL,
		&book.ImageFilePath,
		&book.ImageSource,
		&book.SortOrder,
		&active,
		&book.CreatedAt,
		&book.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return Book{}, err
		}
		return Book{}, err
	}
	book.IsActive = active == 1
	return book, nil
}
