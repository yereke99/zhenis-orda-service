package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

func Open(ctx context.Context, path string) (*sql.DB, error) {
	if path == "" {
		path = "data/zhenis_orda.sqlite"
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}

	dsn := path
	if !strings.Contains(path, "?") {
		dsn = fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`); err != nil {
		return err
	}
	if err := guardLegacyIntegerIDs(ctx, db); err != nil {
		return err
	}
	statements := []string{schemaV1}
	for i, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migration statement %d: %w", i+1, err)
		}
	}
	if err := addColumnIfMissing(ctx, db, "users", "photo_url", "TEXT"); err != nil {
		return err
	}
	if err := addTariffColumns(ctx, db); err != nil {
		return err
	}
	if err := addContactPhoneColumns(ctx, db); err != nil {
		return err
	}
	if err := addLevelTelegramColumns(ctx, db); err != nil {
		return err
	}
	if err := addLevelInviteTables(ctx, db); err != nil {
		return err
	}
	if err := addPaymentReceiptColumns(ctx, db); err != nil {
		return err
	}
	if err := addPremiumCourseTables(ctx, db); err != nil {
		return err
	}
	if err := migratePaymentsForPremiumCourses(ctx, db); err != nil {
		return err
	}
	if err := migrateLessonOwnedTests(ctx, db); err != nil {
		return err
	}
	if err := addTestCorrectnessColumns(ctx, db); err != nil {
		return err
	}
	if err := addLegalAgreementTables(ctx, db); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, indexesV1); err != nil {
		return fmt.Errorf("migration indexes: %w", err)
	}
	if err := addReceiptUniqueIndexes(ctx, db); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, name) VALUES (1, 'initial_zhenis_orda_schema');`); err != nil {
		return err
	}
	return Seed(ctx, db)
}

func addTariffColumns(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		name       string
		definition string
	}{
		{"short_description_kk", "TEXT NOT NULL DEFAULT ''"},
		{"full_description_kk", "TEXT NOT NULL DEFAULT ''"},
		{"image_url", "TEXT"},
		{"image_file_path", "TEXT"},
		{"image_source", "TEXT NOT NULL DEFAULT 'none'"},
	}
	for _, column := range columns {
		if err := addColumnIfMissing(ctx, db, "tariffs", column.name, column.definition); err != nil {
			return err
		}
	}
	return nil
}

func addContactPhoneColumns(ctx context.Context, db *sql.DB) error {
	if err := addColumnIfMissing(ctx, db, "users", "phone", "TEXT"); err != nil {
		return err
	}
	return addColumnIfMissing(ctx, db, "payments", "contact_phone", "TEXT")
}

func addLevelTelegramColumns(ctx context.Context, db *sql.DB) error {
	return addColumnIfMissing(ctx, db, "levels", "telegram_chat_id", "TEXT")
}

func addLevelInviteTables(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS user_level_telegram_invites (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			telegram_user_id INTEGER,
			level_id TEXT NOT NULL REFERENCES levels(id) ON DELETE CASCADE,
			telegram_chat_id TEXT NOT NULL,
			invite_link TEXT NOT NULL,
			invite_link_id TEXT,
			raw_payload TEXT NOT NULL DEFAULT '{}',
			expires_at DATETIME,
			status TEXT NOT NULL DEFAULT 'issued' CHECK(status IN ('issued','used','expired','revoked')),
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS financial_iq_results (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			score INTEGER NOT NULL,
			result_title TEXT NOT NULL,
			result_level TEXT NOT NULL,
			result_text TEXT NOT NULL DEFAULT '',
			answers_json TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	return err
}

func addPaymentReceiptColumns(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		name       string
		definition string
	}{
		{"expected_amount_kzt", "INTEGER"},
		{"amount_difference_kzt", "INTEGER"},
		{"receipt_transaction_key", "TEXT"},
		{"parsed_recipient_bin", "TEXT"},
		{"expected_recipient_bin", "TEXT"},
	}
	for _, column := range columns {
		if err := addColumnIfMissing(ctx, db, "payment_receipts", column.name, column.definition); err != nil {
			return err
		}
	}
	return nil
}

func addPremiumCourseTables(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS premium_courses (
			id TEXT PRIMARY KEY,
			slug TEXT NOT NULL UNIQUE,
			title TEXT NOT NULL,
			description TEXT,
			price_kzt INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','inactive','archived')),
			sort_order INTEGER NOT NULL DEFAULT 0,
			default_access_duration_type TEXT NOT NULL DEFAULT 'lifetime' CHECK(default_access_duration_type IN ('lifetime','30_days','90_days','custom')),
			default_access_expires_at DATETIME,
			telegram_chat_id TEXT,
			invite_link_type TEXT NOT NULL DEFAULT 'manual' CHECK(invite_link_type IN ('bot','manual')),
			manual_invite_link TEXT,
			telegram_button_title TEXT,
			admin_notes TEXT,
			cover_image_url TEXT,
			cover_image_path TEXT,
			cover_image_source TEXT NOT NULL DEFAULT 'none',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS premium_course_lessons (
			id TEXT PRIMARY KEY,
			course_id TEXT NOT NULL REFERENCES premium_courses(id) ON DELETE CASCADE,
			title TEXT NOT NULL,
			description TEXT,
			video_url TEXT,
			content_text TEXT,
			position INTEGER NOT NULL DEFAULT 0,
			is_preview INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(course_id, position)
		);

		CREATE TABLE IF NOT EXISTS user_course_access (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			course_id TEXT NOT NULL REFERENCES premium_courses(id) ON DELETE CASCADE,
			access_status TEXT NOT NULL CHECK(access_status IN ('active', 'revoked', 'expired')),
			access_source TEXT NOT NULL CHECK(access_source IN ('manual', 'payment', 'bonus', 'gift')),
			granted_by_admin_id INTEGER,
			payment_id TEXT REFERENCES payments(id) ON DELETE SET NULL,
			granted_at DATETIME NOT NULL,
			expires_at DATETIME,
			revoked_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS user_premium_course_telegram_invites (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			course_id TEXT NOT NULL REFERENCES premium_courses(id) ON DELETE CASCADE,
			telegram_chat_id TEXT NOT NULL,
			invite_link TEXT NOT NULL,
			expires_at DATETIME,
			status TEXT NOT NULL DEFAULT 'issued' CHECK(status IN ('issued','used','expired','revoked')),
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return err
	}
	columns := []struct {
		name       string
		definition string
	}{
		{"default_access_duration_type", "TEXT NOT NULL DEFAULT 'lifetime'"},
		{"default_access_expires_at", "DATETIME"},
		{"telegram_chat_id", "TEXT"},
		{"invite_link_type", "TEXT NOT NULL DEFAULT 'manual'"},
		{"manual_invite_link", "TEXT"},
		{"telegram_button_title", "TEXT"},
		{"admin_notes", "TEXT"},
		{"cover_image_url", "TEXT"},
		{"cover_image_path", "TEXT"},
		{"cover_image_source", "TEXT NOT NULL DEFAULT 'none'"},
	}
	for _, column := range columns {
		if err := addColumnIfMissing(ctx, db, "premium_courses", column.name, column.definition); err != nil {
			return err
		}
	}
	if err := addColumnIfMissing(ctx, db, "premium_course_lessons", "content_text", "TEXT"); err != nil {
		return err
	}
	return nil
}

func migratePaymentsForPremiumCourses(ctx context.Context, db *sql.DB) error {
	if err := addColumnIfMissing(ctx, db, "payments", "payment_type", "TEXT NOT NULL DEFAULT 'subscription'"); err != nil {
		return err
	}
	if err := addColumnIfMissing(ctx, db, "payments", "premium_course_id", "TEXT REFERENCES premium_courses(id) ON DELETE SET NULL"); err != nil {
		return err
	}
	notNull, err := columnNotNull(ctx, db, "payments", "tariff_id")
	if err != nil {
		return err
	}
	if !notNull {
		return nil
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=OFF; PRAGMA legacy_alter_table=ON;`); err != nil {
		return err
	}
	defer db.ExecContext(ctx, `PRAGMA legacy_alter_table=OFF; PRAGMA foreign_keys=ON;`)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `ALTER TABLE payments RENAME TO payments_legacy_premium;`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE payments (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			tariff_id TEXT REFERENCES tariffs(id),
			payment_type TEXT NOT NULL DEFAULT 'subscription' CHECK(payment_type IN ('subscription','premium_course')),
			premium_course_id TEXT REFERENCES premium_courses(id) ON DELETE SET NULL,
			subscription_id TEXT REFERENCES subscriptions(id) ON DELETE SET NULL,
			amount_kzt INTEGER NOT NULL,
			provider TEXT NOT NULL CHECK(provider IN ('kaspi_qr','kaspi_pay','halyk','bank_card')),
			status TEXT NOT NULL CHECK(status IN ('pending','uploaded_receipt','approved','rejected','expired','cancelled')),
			contact_phone TEXT,
			receipt_file_path TEXT,
			admin_comment TEXT,
			approved_by_admin_id INTEGER,
			approved_at DATETIME,
			expires_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO payments(
			id, user_id, tariff_id, payment_type, premium_course_id, subscription_id, amount_kzt, provider, status,
			contact_phone, receipt_file_path, admin_comment, approved_by_admin_id, approved_at, expires_at, created_at, updated_at
		)
		SELECT id, user_id, tariff_id, COALESCE(payment_type, 'subscription'), premium_course_id, subscription_id,
			amount_kzt, provider, status, contact_phone, receipt_file_path, admin_comment, approved_by_admin_id,
			approved_at, expires_at, created_at, updated_at
		FROM payments_legacy_premium;
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE payments_legacy_premium;`); err != nil {
		return err
	}
	return tx.Commit()
}

func addReceiptUniqueIndexes(ctx context.Context, db *sql.DB) error {
	for _, name := range []string{
		"uniq_receipts_approved_file_hash",
		"uniq_receipts_approved_qr_hash",
		"uniq_receipts_approved_raw_text_hash",
	} {
		if _, err := db.ExecContext(ctx, `DROP INDEX IF EXISTS `+name); err != nil {
			return err
		}
	}
	indexes := []struct {
		name   string
		column string
	}{
		{"uniq_receipts_approved_transaction_key", "receipt_transaction_key"},
		{"uniq_receipts_approved_check", "parsed_check_id"},
	}
	for _, index := range indexes {
		ok, err := noApprovedReceiptDuplicates(ctx, db, index.column)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		stmt := fmt.Sprintf(
			`CREATE UNIQUE INDEX IF NOT EXISTS %s ON payment_receipts(%s) WHERE validation_status = 'approved' AND %s IS NOT NULL AND %s <> '';`,
			index.name,
			index.column,
			index.column,
			index.column,
		)
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func noApprovedReceiptDuplicates(ctx context.Context, db *sql.DB, column string) (bool, error) {
	query := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM (
			SELECT %s
			FROM payment_receipts
			WHERE validation_status = 'approved' AND %s IS NOT NULL AND %s <> ''
			GROUP BY %s
			HAVING COUNT(*) > 1
			LIMIT 1
		);
	`, column, column, column, column)
	var count int
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return false, err
	}
	return count == 0, nil
}

func migrateLessonOwnedTests(ctx context.Context, db *sql.DB) error {
	var createSQL string
	if err := db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'tests'`).Scan(&createSQL); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	hasLessonID, err := columnExists(ctx, db, "tests", "lesson_id")
	if err != nil {
		return err
	}
	hasLevelUnique := strings.Contains(strings.ToUpper(createSQL), "UNIQUE(LEVEL_ID)")
	if hasLessonID && !hasLevelUnique {
		return nil
	}

	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=OFF; PRAGMA legacy_alter_table=ON;`); err != nil {
		return err
	}
	defer db.ExecContext(ctx, `PRAGMA legacy_alter_table=OFF; PRAGMA foreign_keys=ON;`)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `ALTER TABLE tests RENAME TO tests_legacy_migrate;`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE tests (
			id TEXT PRIMARY KEY,
			level_id TEXT REFERENCES levels(id) ON DELETE CASCADE,
			lesson_id TEXT REFERENCES lessons(id) ON DELETE CASCADE,
			title TEXT NOT NULL,
			pass_percent INTEGER NOT NULL DEFAULT 70,
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return err
	}
	lessonSelect := "NULL"
	if hasLessonID {
		lessonSelect = "lesson_id"
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO tests(id, level_id, lesson_id, title, pass_percent, is_active, created_at, updated_at)
		SELECT id, level_id, %s, title, pass_percent, is_active, created_at, updated_at
		FROM tests_legacy_migrate;
	`, lessonSelect)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE tests_legacy_migrate;`); err != nil {
		return err
	}
	return tx.Commit()
}

func addTestCorrectnessColumns(ctx context.Context, db *sql.DB) error {
	if err := addColumnIfMissing(ctx, db, "test_options", "is_correct", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return addColumnIfMissing(ctx, db, "test_answers", "is_correct", "INTEGER NOT NULL DEFAULT 0")
}

func addLegalAgreementTables(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS user_legal_agreements (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			telegram_id INTEGER NOT NULL,
			document_type TEXT NOT NULL,
			document_version TEXT NOT NULL,
			document_language TEXT NOT NULL,
			document_hash TEXT NOT NULL,
			accepted_at DATETIME NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_user_legal_agreements_telegram ON user_legal_agreements(telegram_id);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_user_legal_agreements_unique ON user_legal_agreements(user_id, document_type, document_version);
		INSERT OR IGNORE INTO schema_migrations(version, name) VALUES (2, 'user_legal_agreements');
	`); err != nil {
		return err
	}
	return nil
}

func columnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	var exists int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

func columnNotNull(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT name, "notnull" FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var notNull int
		if err := rows.Scan(&name, &notNull); err != nil {
			return false, err
		}
		if name == column {
			return notNull == 1, nil
		}
	}
	return false, rows.Err()
}

func addColumnIfMissing(ctx context.Context, db *sql.DB, table, column, definition string) error {
	var exists int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&exists)
	if err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	_, err = db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

func guardLegacyIntegerIDs(ctx context.Context, db *sql.DB) error {
	var idType string
	err := db.QueryRowContext(ctx, `SELECT type FROM pragma_table_info('users') WHERE name = 'id'`).Scan(&idType)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if strings.EqualFold(idType, "TEXT") {
		return nil
	}
	if os.Getenv("RESET_LEGACY_INTEGER_IDS") != "1" {
		return fmt.Errorf("legacy integer-id database detected; set RESET_LEGACY_INTEGER_IDS=1 in development to recreate UUID tables after backup")
	}
	tables := []string{
		"test_answers", "test_attempts", "test_options", "test_questions", "tests",
		"lesson_progress", "lessons", "assignment_submissions", "assignments",
		"payment_receipts", "payments", "subscriptions", "diagnostics",
		"referral_rewards", "referrals", "coin_transactions",
		"user_premium_course_telegram_invites", "user_course_access", "premium_course_lessons", "premium_courses",
		"channel_invite_links", "channels",
		"user_level_telegram_invites", "financial_iq_results",
		"user_stream_attendance", "live_stream_recordings", "live_stream_reminders", "live_streams",
		"broadcast_messages", "broadcasts", "support_messages", "admin_actions",
		"free_lessons", "books", "levels", "tariffs", "users",
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=OFF;`); err != nil {
		return err
	}
	for _, table := range tables {
		if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
			return err
		}
	}
	_, err = db.ExecContext(ctx, `PRAGMA foreign_keys=ON;`)
	return err
}

func Seed(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := seedTariffs(ctx, tx); err != nil {
		return err
	}
	if err := seedLevels(ctx, tx); err != nil {
		return err
	}
	if err := seedSettings(ctx, tx); err != nil {
		return err
	}
	if err := seedPremiumCourses(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func seedTariffs(ctx context.Context, tx *sql.Tx) error {
	tariffs := []struct {
		Code     string
		Title    string
		Price    int
		OldPrice int
		Short    string
		Full     string
		Features []string
		Order    int
	}{
		{
			Code:     "BASIC",
			Title:    "BASIC",
			Price:    9900,
			OldPrice: 4990,
			Short:    "ZHENIS ORDA UNIVERSE платформасына базалық қолжетімділік.",
			Full:     "Платформаға қолжетімділік, ай сайын ашылатын сабақтар, тесттер, workbook, streak, бонус жүйесі және мотивациялық push хабарламалар.",
			Features: []string{"платформаға қолжетімділік", "ай сайын ашылатын сабақтар", "тесттер", "workbook", "streak", "бонус жүйесі", "мотивациялық push хабарламалар"},
			Order:    1,
		},
		{
			Code:     "STANDARD",
			Title:    "STANDARD",
			Price:    24900,
			OldPrice: 9990,
			Short:    "BASIC мүмкіндіктеріне қосымша жабық эфирлер мен диагностика.",
			Full:     "BASIC ішіндегі барлық мүмкіндікке апталық жабық эфир, диагностика, қосымша мастер-класс және арнайы контент қосылады.",
			Features: []string{"BASIC ішіндегі барлық мүмкіндік", "апталық жабық эфир", "диагностика", "қосымша мастер-класс", "арнайы контент"},
			Order:    2,
		},
		{
			Code:     "VIP",
			Title:    "VIP",
			Price:    49900,
			OldPrice: 24900,
			Short:    "STANDARD мүмкіндіктеріне қосымша VIP қолдау және mentor touchpoint.",
			Full:     "STANDARD ішіндегі барлық мүмкіндікке VIP жабық эфир, mentor touchpoint, mini-разбор мүмкіндігі және priority support қосылады.",
			Features: []string{"STANDARD ішіндегі барлық мүмкіндік", "VIP жабық эфир", "mentor touchpoint", "mini-разбор мүмкіндігі", "priority support"},
			Order:    3,
		},
	}
	for _, tariff := range tariffs {
		features, _ := json.Marshal(tariff.Features)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tariffs(id, code, title, price_kzt, short_description_kk, full_description_kk, features_json, sort_order, is_active, image_source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 'none')
			ON CONFLICT(code) DO NOTHING;
		`, uuid.NewString(), tariff.Code, tariff.Title, tariff.Price, tariff.Short, tariff.Full, string(features), tariff.Order); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE tariffs
			SET title = ?, price_kzt = ?, short_description_kk = ?, full_description_kk = ?,
				features_json = ?, sort_order = ?, updated_at = CURRENT_TIMESTAMP
			WHERE code = ? AND title = ? AND price_kzt = ?;
		`, tariff.Title, tariff.Price, tariff.Short, tariff.Full, string(features), tariff.Order, tariff.Code, tariff.Title, tariff.OldPrice); err != nil {
			return err
		}
	}
	return nil
}

func seedSettings(ctx context.Context, tx *sql.Tx) error {
	settings := map[string]string{
		"platform_name": "ZHENIS ORDA UNIVERSE",
		"brand_line":    "Жүйелі өсу ордасы.",
		"stream_name":   "ZHABYQ RAZBOR NIGHT",
		"channel_link":  "https://t.me/zhenisOrdaFinanceBot",
	}
	for key, value := range settings {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO app_settings(key, value)
			VALUES (?, ?)
			ON CONFLICT(key) DO NOTHING;
		`, key, value); err != nil {
			return err
		}
	}
	return nil
}

func seedPremiumCourses(ctx context.Context, tx *sql.Tx) error {
	courses := []struct {
		Slug        string
		Title       string
		Description string
		Price       int
		Order       int
		Lessons     []struct {
			Title       string
			Description string
			Content     string
			Preview     bool
		}
	}{
		{
			Slug:        "altyn-formula",
			Title:       "АЛТЫН ФОРМУЛА",
			Description: "Қаржы, тәртіп және ақша көбейтуге арналған бөлек premium курс.",
			Price:       250000,
			Order:       1,
			Lessons: []struct {
				Title       string
				Description string
				Content     string
				Preview     bool
			}{
				{"Preview video", "Курсқа қысқаша ашық кіріспе.", "Бұл preview сабақ төлемсіз ашық.", true},
				{"Lesson 1", "Premium сабақ", "Бұл сабақ premium қолжетімділік ашылғаннан кейін көрінеді.", false},
				{"Lesson 2", "Premium сабақ", "Бұл сабақ premium қолжетімділік ашылғаннан кейін көрінеді.", false},
				{"Lesson 3", "Premium сабақ", "Бұл сабақ premium қолжетімділік ашылғаннан кейін көрінеді.", false},
			},
		},
		{
			Slug:        "biznes-praktikum",
			Title:       "БИЗНЕС ПРАКТИКУМ",
			Description: "Бизнес жүйелеуге арналған бөлек premium практикум.",
			Price:       650000,
			Order:       2,
			Lessons: []struct {
				Title       string
				Description string
				Content     string
				Preview     bool
			}{
				{"Preview video", "Курсқа қысқаша ашық кіріспе.", "Бұл preview сабақ төлемсіз ашық.", true},
				{"Lesson 1", "Premium сабақ", "Бұл сабақ premium қолжетімділік ашылғаннан кейін көрінеді.", false},
				{"Lesson 2", "Premium сабақ", "Бұл сабақ premium қолжетімділік ашылғаннан кейін көрінеді.", false},
				{"Lesson 3", "Premium сабақ", "Бұл сабақ premium қолжетімділік ашылғаннан кейін көрінеді.", false},
			},
		},
	}
	for _, course := range courses {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO premium_courses(id, slug, title, description, price_kzt, status, sort_order, default_access_duration_type, invite_link_type, cover_image_source)
			VALUES (?, ?, ?, ?, ?, 'active', ?, 'lifetime', 'manual', 'none')
			ON CONFLICT(slug) DO NOTHING;
		`, uuid.NewString(), course.Slug, course.Title, course.Description, course.Price, course.Order); err != nil {
			return err
		}
		var courseID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM premium_courses WHERE slug = ?`, course.Slug).Scan(&courseID); err != nil {
			return err
		}
		for i, lesson := range course.Lessons {
			preview := 0
			if lesson.Preview {
				preview = 1
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO premium_course_lessons(id, course_id, title, description, content_text, position, is_preview, is_active)
				VALUES (?, ?, ?, ?, ?, ?, ?, 1)
				ON CONFLICT(course_id, position) DO NOTHING;
			`, uuid.NewString(), courseID, lesson.Title, lesson.Description, lesson.Content, i, preview); err != nil {
				return err
			}
		}
	}
	return nil
}

func seedLevels(ctx context.Context, tx *sql.Tx) error {
	seedDemoContent := os.Getenv("SEED_DEMO_CONTENT") == "1"
	levels := []struct {
		Number     int
		TitleKK    string
		TitleRU    string
		LessonsKK  []string
		LessonsRU  []string
		Assignment string
	}{
		{1, "ОЙЛАУ / ІРГЕТАС", "ОЙЛАУ / ІРГЕТАС", []string{"Барлығы осы іргетасқа байланған", "НАР-ТӘУЕКЕЛ", "Үлкен ризықтың есігін аш", "Өз ісіңді тап"}, []string{"Барлығы осы іргетасқа байланған", "НАР-ТӘУЕКЕЛ", "Үлкен ризықтың есігін аш", "Өз ісіңді тап"}, "Менің қазіргі басты проблемам"},
		{2, "ҚАРЖЫ RESET", "ҚАРЖЫ RESET", []string{"Қарызсыз QAZAQ", "Ақшаға апаратын 3 қадам"}, []string{"Қарызсыз QAZAQ", "Ақшаға апаратын 3 қадам"}, "Жеке қаржы картасын толтыру"},
		{3, "АЛТЫН ФОРМУЛА START", "ЗОЛОТАЯ ФОРМУЛА START", []string{"Ақша табу", "Ақша сақтау", "Ақша көбейту"}, []string{"Заработать деньги", "Сохранить деньги", "Приумножить деньги"}, ""},
		{4, "АЛТЫН ФОРМУЛА FULL", "АЛТЫН ФОРМУЛА FULL", []string{"қаржы тәртібі", "ақшаға қатынас", "ризық заңдылықтары"}, []string{"қаржы тәртібі", "ақшаға қатынас", "ризық заңдылықтары"}, ""},
		{5, "БИЗНЕС ІРГЕТАСЫ", "БИЗНЕС ІРГЕТАСЫ", []string{"Кәсіпкерліктің 6 сатысы"}, []string{"Кәсіпкерліктің 6 сатысы"}, ""},
		{6, "БИЗНЕС ПРАКТИКУМ", "БИЗНЕС ПРАКТИКУМ", []string{"32 күндік бизнес практикум"}, []string{"32 күндік бизнес практикум"}, ""},
		{7, "BUSINESS UPGRADE", "BUSINESS UPGRADE", []string{"жүйе", "команда", "басқару", "сату"}, []string{"жүйе", "команда", "басқару", "сату"}, ""},
		{8, "САТУ ЖӘНЕ ЫҚПАЛ", "САТУ ЖӘНЕ ЫҚПАЛ", []string{"ұсыныс", "келіссөз", "клиент психологиясы"}, []string{"ұсыныс", "келіссөз", "клиент психологиясы"}, ""},
		{9, "КӨШБАСШЫЛЫҚ", "КӨШБАСШЫЛЫҚ", []string{"лидерлік", "тәртіп", "жауапкершілік"}, []string{"лидерлік", "тәртіп", "жауапкершілік"}, ""},
		{10, "ІШКІ ЖҰМЫС", "ІШКІ ЖҰМЫС", []string{"ішкі блок", "қорқыныш", "кінә", "ұят", "шектеуші сенім"}, []string{"ішкі блок", "қорқыныш", "кінә", "ұят", "шектеуші сенім"}, ""},
		{11, "ЖЕКЕ БРЕНД", "ЖЕКЕ БРЕНД", []string{"позиция", "контент", "эксперттік бейне", "аудитория жинау"}, []string{"позиция", "контент", "эксперттік бейне", "аудитория жинау"}, ""},
		{12, "MASTER ДЕҢГЕЙ", "MASTER ДЕҢГЕЙ", []string{"финалдық тест", "сертификат", "MASTER мәртебесі", "жабық түлектер клубы"}, []string{"финалдық тест", "сертификат", "MASTER мәртебесі", "жабық түлектер клубы"}, ""},
	}

	for _, level := range levels {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO levels(id, number, title_kk, title_ru, description_kk, description_ru, sort_order, is_active)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1)
			ON CONFLICT(number) DO UPDATE SET
				title_kk=excluded.title_kk,
				title_ru=excluded.title_ru,
				description_kk=excluded.description_kk,
				description_ru=excluded.description_ru,
				sort_order=excluded.sort_order,
				is_active=1,
				updated_at=CURRENT_TIMESTAMP;
			`, uuid.NewString(), level.Number, level.TitleKK, level.TitleRU, fmt.Sprintf("Деңгей %d / Ай %d", level.Number, level.Number), fmt.Sprintf("Деңгей %d / Ай %d", level.Number, level.Number), level.Number); err != nil {
			return err
		}
		if !seedDemoContent {
			continue
		}
		var levelID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM levels WHERE number = ?`, level.Number).Scan(&levelID); err != nil {
			return err
		}
		for i, lessonKK := range level.LessonsKK {
			lessonRU := lessonKK
			if i < len(level.LessonsRU) {
				lessonRU = level.LessonsRU[i]
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO lessons(id, level_id, title_kk, title_ru, description_kk, description_ru, video_url, sort_order, is_active)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
				ON CONFLICT(level_id, sort_order) DO UPDATE SET
					title_kk=excluded.title_kk,
					title_ru=excluded.title_ru,
					description_kk=excluded.description_kk,
					description_ru=excluded.description_ru,
					updated_at=CURRENT_TIMESTAMP;
			`, uuid.NewString(), levelID, lessonKK, lessonRU, "ZHENIS ORDA UNIVERSE", "ZHENIS ORDA UNIVERSE", fmt.Sprintf("https://t.me/zhenisorda_content/%d%d", level.Number, i+1), i+1); err != nil {
				return err
			}
		}

		var firstLessonID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM lessons WHERE level_id = ? ORDER BY sort_order ASC LIMIT 1`, levelID).Scan(&firstLessonID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
				INSERT INTO tests(id, level_id, lesson_id, title, pass_percent, is_active)
				VALUES (?, ?, ?, ?, 70, 1)
				ON CONFLICT(lesson_id) DO UPDATE SET title=excluded.title, pass_percent=70, is_active=1, updated_at=CURRENT_TIMESTAMP;
			`, uuid.NewString(), levelID, firstLessonID, fmt.Sprintf("Деңгей %d тесті", level.Number)); err != nil {
			return err
		}
		var testID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM tests WHERE lesson_id = ?`, firstLessonID).Scan(&testID); err != nil {
			return err
		}
		for q := 1; q <= 10; q++ {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO test_questions(id, test_id, question_text_kk, question_text_ru, sort_order, is_active)
				VALUES (?, ?, ?, ?, ?, 1)
				ON CONFLICT(test_id, sort_order) DO UPDATE SET
					question_text_kk=excluded.question_text_kk,
					question_text_ru=excluded.question_text_ru,
					is_active=1,
					updated_at=CURRENT_TIMESTAMP;
				`, uuid.NewString(), testID, fmt.Sprintf("Деңгей %d сұрақ %d", level.Number, q), fmt.Sprintf("Деңгей %d сұрақ %d", level.Number, q), q); err != nil {
				return err
			}
			var questionID string
			if err := tx.QueryRowContext(ctx, `SELECT id FROM test_questions WHERE test_id = ? AND sort_order = ?`, testID, q).Scan(&questionID); err != nil {
				return err
			}
			for option := 1; option <= 4; option++ {
				isCorrect := 0
				if option == 1 {
					isCorrect = 1
				}
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO test_options(id, question_id, option_text_kk, option_text_ru, is_correct, sort_order)
					VALUES (?, ?, ?, ?, ?, ?)
					ON CONFLICT(question_id, sort_order) DO UPDATE SET
						option_text_kk=excluded.option_text_kk,
						option_text_ru=excluded.option_text_ru,
						is_correct=excluded.is_correct,
						updated_at=CURRENT_TIMESTAMP;
				`, uuid.NewString(), questionID, fmt.Sprintf("Жауап %d", option), fmt.Sprintf("Ответ %d", option), isCorrect, option); err != nil {
					return err
				}
			}
		}

		if level.Assignment != "" {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO assignments(id, level_id, title_kk, title_ru, description_kk, description_ru, is_active)
				VALUES (?, ?, ?, ?, ?, ?, 1)
				ON CONFLICT(level_id) DO UPDATE SET
					title_kk=excluded.title_kk,
					title_ru=excluded.title_ru,
					description_kk=excluded.description_kk,
					description_ru=excluded.description_ru,
					is_active=1,
					updated_at=CURRENT_TIMESTAMP;
			`, uuid.NewString(), levelID, level.Assignment, level.Assignment, level.Assignment, level.Assignment); err != nil {
				return err
			}
		}
	}
	return nil
}
