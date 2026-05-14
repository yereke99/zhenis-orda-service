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
	if err := migrateLessonOwnedTests(ctx, db); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, indexesV1); err != nil {
		return fmt.Errorf("migration indexes: %w", err)
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

func columnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	var exists int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists > 0, nil
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
		"channel_invite_links", "channels",
		"user_stream_attendance", "live_stream_recordings", "live_stream_reminders", "live_streams",
		"broadcast_messages", "broadcasts", "support_messages", "admin_actions",
		"levels", "tariffs", "users",
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
