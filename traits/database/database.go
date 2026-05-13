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
	statements := []string{schemaV1, indexesV1}
	for i, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migration statement %d: %w", i+1, err)
		}
	}
	if err := addColumnIfMissing(ctx, db, "users", "photo_url", "TEXT"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, name) VALUES (1, 'initial_zhenis_orda_schema');`); err != nil {
		return err
	}
	return Seed(ctx, db)
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
		Features []string
		Order    int
	}{
		{"BASIC", "BASIC", 4990, []string{"ай сайынғы сабақтар", "тесттер", "прогресс жүйесі", "рефералдық сілтеме"}, 1},
		{"STANDARD", "STANDARD", 9990, []string{"BASIC ішіндегі барлық мүмкіндік", "апталық жабық талдау эфирі", "эфир жазбалары"}, 2},
		{"VIP", "VIP", 24900, []string{"STANDARD ішіндегі барлық мүмкіндік", "VIP чат", "басымдықтағы сұрақтар", "ай сайынғы жеке мини-талдау"}, 3},
	}
	for _, tariff := range tariffs {
		features, _ := json.Marshal(tariff.Features)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tariffs(id, code, title, price_kzt, features_json, sort_order, is_active)
			VALUES (?, ?, ?, ?, ?, ?, 1)
			ON CONFLICT(code) DO UPDATE SET
				title=excluded.title,
				price_kzt=excluded.price_kzt,
				features_json=excluded.features_json,
				sort_order=excluded.sort_order,
				is_active=1,
				updated_at=CURRENT_TIMESTAMP;
		`, uuid.NewString(), tariff.Code, tariff.Title, tariff.Price, string(features), tariff.Order); err != nil {
			return err
		}
	}
	return nil
}

func seedSettings(ctx context.Context, tx *sql.Tx) error {
	settings := map[string]string{
		"platform_name": "ZHENIS ORDA INSIDE",
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
		{12, "MASTER LEVEL", "MASTER LEVEL", []string{"финалдық тест", "сертификат", "MASTER мәртебесі", "жабық түлектер клубы"}, []string{"финалдық тест", "сертификат", "MASTER мәртебесі", "жабық түлектер клубы"}, ""},
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
		`, uuid.NewString(), level.Number, level.TitleKK, level.TitleRU, fmt.Sprintf("LEVEL %d / Month %d", level.Number, level.Number), fmt.Sprintf("LEVEL %d / Month %d", level.Number, level.Number), level.Number); err != nil {
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
			`, uuid.NewString(), levelID, lessonKK, lessonRU, "ZHENIS ORDA INSIDE", "ZHENIS ORDA INSIDE", fmt.Sprintf("https://t.me/zhenisorda_content/%d%d", level.Number, i+1), i+1); err != nil {
				return err
			}
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tests(id, level_id, title, pass_percent, is_active)
			VALUES (?, ?, ?, 70, 1)
			ON CONFLICT(level_id) DO UPDATE SET title=excluded.title, pass_percent=70, is_active=1, updated_at=CURRENT_TIMESTAMP;
		`, uuid.NewString(), levelID, fmt.Sprintf("LEVEL %d test", level.Number)); err != nil {
			return err
		}
		var testID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM tests WHERE level_id = ?`, levelID).Scan(&testID); err != nil {
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
			`, uuid.NewString(), testID, fmt.Sprintf("LEVEL %d сұрақ %d", level.Number, q), fmt.Sprintf("LEVEL %d вопрос %d", level.Number, q), q); err != nil {
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
