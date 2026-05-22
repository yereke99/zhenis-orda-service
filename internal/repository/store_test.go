package repository_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"zhenis-orda-service/internal/repository"
	"zhenis-orda-service/traits/database"
)

func newTestStore(t *testing.T) (*repository.Store, context.Context) {
	t.Helper()
	t.Setenv("SEED_DEMO_CONTENT", "1")
	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	return repository.New(db), ctx
}

func registerUser(t *testing.T, ctx context.Context, store *repository.Store, telegramID int64, startParam string) repository.User {
	t.Helper()
	user, _, err := store.RegisterOrUpdateTelegramUser(ctx, repository.TelegramUserInput{
		TelegramID: telegramID,
		Username:   "user",
		FirstName:  "Test",
		Language:   "kk",
		StartParam: startParam,
	})
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func approveBasic(t *testing.T, ctx context.Context, store *repository.Store, userID string) repository.Payment {
	t.Helper()
	payment, err := store.CreatePayment(ctx, userID, "BASIC", "kaspi_qr", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := store.ApprovePayment(ctx, payment.ID, 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	return approved
}

func firstLessonIDForLevel(t *testing.T, ctx context.Context, store *repository.Store, levelNumber int) string {
	t.Helper()
	var lessonID string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT l.id
		FROM lessons l
		JOIN levels lv ON lv.id = l.level_id
		WHERE lv.number = ?
		ORDER BY l.sort_order ASC
		LIMIT 1
	`, levelNumber).Scan(&lessonID); err != nil {
		t.Fatal(err)
	}
	return lessonID
}

func TestReferralRegistration(t *testing.T) {
	store, ctx := newTestStore(t)
	inviter := registerUser(t, ctx, store, 1001, "")
	invited := registerUser(t, ctx, store, 1002, inviter.ReferralCode)

	if invited.InvitedByUserID == nil || *invited.InvitedByUserID != inviter.ID {
		t.Fatalf("expected inviter %s, got %#v", inviter.ID, invited.InvitedByUserID)
	}
	summary, err := store.ReferralSummary(ctx, inviter.ID, "zhenisOrdaFinanceBot")
	if err != nil {
		t.Fatal(err)
	}
	if summary.InvitedCount != 1 {
		t.Fatalf("expected 1 invited user, got %d", summary.InvitedCount)
	}
}

func TestPaymentApprovalActivatesSubscriptionAndReferralReward(t *testing.T) {
	store, ctx := newTestStore(t)
	inviter := registerUser(t, ctx, store, 2001, "")
	invited := registerUser(t, ctx, store, 2002, inviter.ReferralCode)

	payment := approveBasic(t, ctx, store, invited.ID)
	if payment.Status != repository.PaymentStatusApproved {
		t.Fatalf("payment status = %s", payment.Status)
	}
	sub, err := store.GetActiveSubscription(ctx, invited.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sub == nil || sub.Status != repository.SubscriptionStatusActive {
		t.Fatalf("expected active subscription, got %#v", sub)
	}
	updated, err := store.GetUserByID(ctx, invited.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CurrentLevel != 1 {
		t.Fatalf("current level = %d", updated.CurrentLevel)
	}
	balance, err := store.CoinBalance(ctx, inviter.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 100 {
		t.Fatalf("expected inviter 100 coins, got %d", balance)
	}
	if _, err := store.ApprovePayment(ctx, payment.ID, 1, 30); err != nil {
		t.Fatal(err)
	}
	balance, _ = store.CoinBalance(ctx, inviter.ID)
	if balance != 100 {
		t.Fatalf("referral reward double-counted, got %d", balance)
	}
}

func TestLevelUnlock(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 3001, "")
	approveBasic(t, ctx, store, user.ID)

	lessons, err := store.ListLessons(ctx, user.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, lesson := range lessons {
		if _, err := store.MarkLessonWatched(ctx, user.ID, lesson.ID); err != nil {
			t.Fatal(err)
		}
	}
	test, err := store.GetTestByLevel(ctx, user.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	answers := map[string]string{}
	for _, question := range test.Questions {
		answers[question.ID] = question.Options[0].ID
	}
	attempt, progress, err := store.SubmitTest(ctx, user.ID, 1, answers)
	if err != nil {
		t.Fatal(err)
	}
	if !attempt.Passed || !progress.SubscriptionOK {
		t.Fatalf("expected passed test and subscription ok: %#v %#v", attempt, progress)
	}
	updated, _ := store.GetUserByID(ctx, user.ID)
	if updated.CurrentLevel != 2 {
		t.Fatalf("expected level 2, got %d", updated.CurrentLevel)
	}
}

func TestTestPassFailAndRetry(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 4001, "")
	approveBasic(t, ctx, store, user.ID)

	test, err := store.GetTestByLevel(ctx, user.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	wrong := map[string]string{}
	for _, question := range test.Questions {
		wrong[question.ID] = question.Options[1].ID
	}
	attempt, _, err := store.SubmitTest(ctx, user.ID, 1, wrong)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Passed {
		t.Fatal("wrong answers should fail")
	}
	right := map[string]string{}
	for _, question := range test.Questions {
		right[question.ID] = question.Options[0].ID
	}
	attempt, _, err = store.SubmitTest(ctx, user.ID, 1, right)
	if err != nil {
		t.Fatal(err)
	}
	if !attempt.Passed {
		t.Fatal("retry with correct answers should pass")
	}
}

func TestTestPassCoinsAreIdempotent(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 4101, "")
	approveBasic(t, ctx, store, user.ID)

	test, err := store.GetTestByLevel(ctx, user.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	answers := map[string]string{}
	for _, question := range test.Questions {
		answers[question.ID] = question.Options[0].ID
	}
	for i := 0; i < 2; i++ {
		attempt, _, err := store.SubmitTest(ctx, user.ID, 1, answers)
		if err != nil {
			t.Fatal(err)
		}
		if !attempt.Passed {
			t.Fatalf("attempt %d should pass: %#v", i+1, attempt)
		}
	}
	balance, err := store.CoinBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 20 {
		t.Fatalf("expected one test coin grant, got %d", balance)
	}
}

func TestSubmitTestRequiresActiveSubscription(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 4201, "")

	if _, _, err := store.SubmitTest(ctx, user.ID, 1, map[string]string{}); !errors.Is(err, repository.ErrForbidden) {
		t.Fatalf("expected forbidden without active subscription, got %v", err)
	}
}

func TestCoinIdempotency(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 5001, "")
	approveBasic(t, ctx, store, user.ID)
	lessons, err := store.ListLessons(ctx, user.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkLessonWatched(ctx, user.ID, lessons[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkLessonWatched(ctx, user.ID, lessons[0].ID); err != nil {
		t.Fatal(err)
	}
	balance, err := store.CoinBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 5 {
		t.Fatalf("expected one lesson coin grant, got %d", balance)
	}
}

func TestSubscriptionExpiration(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 6001, "")
	approveBasic(t, ctx, store, user.ID)
	_, err := store.DB().ExecContext(ctx, `UPDATE subscriptions SET expires_at = datetime('now', '-1 hour') WHERE user_id = ?`, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	n, err := store.ExpireSubscriptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected expired subscription")
	}
	sub, err := store.GetActiveSubscription(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sub != nil {
		t.Fatalf("expected no active subscription, got %#v", sub)
	}
}

func TestLessonInactiveIsPreserved(t *testing.T) {
	store, ctx := newTestStore(t)
	var levelID string
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM levels WHERE number = 1`).Scan(&levelID); err != nil {
		t.Fatal(err)
	}
	lesson, err := store.UpsertLesson(ctx, repository.Lesson{
		LevelID:   levelID,
		TitleKK:   "Жабық сабақ",
		TitleRU:   "Жабық сабақ",
		VideoURL:  "https://t.me/private/1",
		SortOrder: 99,
		IsActive:  false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if lesson.IsActive {
		t.Fatal("inactive lesson was forced active")
	}
	items, err := store.ListAdminLessons(ctx, repository.AdminLessonFilter{Status: "inactive"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.ID == lesson.ID && !item.IsActive {
			found = true
		}
	}
	if !found {
		t.Fatal("inactive lesson not visible in admin list")
	}
}

func TestLevelTelegramChatIDNormalization(t *testing.T) {
	store, ctx := newTestStore(t)
	level, err := store.UpsertLevel(ctx, repository.Level{
		Number:         20,
		TitleKK:        "Telegram деңгей",
		TitleRU:        "Telegram деңгей",
		TelegramChatID: "-1002351826422",
		IsActive:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if level.TelegramChatID != "-1002351826422" {
		t.Fatalf("normalized id = %q", level.TelegramChatID)
	}
	level.TelegramChatID = "2351826422"
	updated, err := store.UpsertLevel(ctx, level)
	if err != nil {
		t.Fatal(err)
	}
	if updated.TelegramChatID != "-1002351826422" {
		t.Fatalf("positive id normalized to %q", updated.TelegramChatID)
	}
	updated.TelegramChatID = ""
	cleared, err := store.UpsertLevel(ctx, updated)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.TelegramChatID != "" || cleared.TelegramConfigured {
		t.Fatalf("empty telegram id should be allowed, got %#v", cleared)
	}
	updated.TelegramChatID = "-12345"
	if _, err := store.UpsertLevel(ctx, updated); err != repository.ErrInvalidState {
		t.Fatalf("expected invalid state for bad chat id, got %v", err)
	}
}

func TestLessonCanBeCreatedWithoutVideoURL(t *testing.T) {
	store, ctx := newTestStore(t)
	var levelID string
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM levels WHERE number = 1`).Scan(&levelID); err != nil {
		t.Fatal(err)
	}
	lesson, err := store.UpsertLesson(ctx, repository.Lesson{
		LevelID:   levelID,
		TitleKK:   "URL жоқ сабақ",
		TitleRU:   "URL жоқ сабақ",
		SortOrder: 100,
		IsActive:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if lesson.VideoURL != "" {
		t.Fatalf("expected empty video url, got %q", lesson.VideoURL)
	}
}

func TestFinancialIQResultPersists(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 7101, "")
	result, err := store.SaveFinancialIQResult(ctx, user.ID, 88, "81-140 балл аралығы", "Қаржылық IQ деңгейі — жоғары", "Жақсы нәтиже", map[string]any{"q1": "2"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != 88 {
		t.Fatalf("score = %d", result.Score)
	}
	latest, err := store.GetLatestFinancialIQResult(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || latest.ResultLevel != "Қаржылық IQ деңгейі — жоғары" {
		t.Fatalf("unexpected latest IQ result: %#v", latest)
	}
}

func TestAdminTestValidationRequiresExactlyOneCorrectAnswer(t *testing.T) {
	store, ctx := newTestStore(t)
	lessonID := firstLessonIDForLevel(t, ctx, store, 2)

	base := func(options []repository.TestOption) repository.Test {
		return repository.Test{
			LessonID:    lessonID,
			Title:       "Дұрыс жауап валидациясы",
			PassPercent: 70,
			IsActive:    true,
			Questions: []repository.TestQuestion{{
				QuestionTextKK: "Қай жауап дұрыс?",
				QuestionTextRU: "Какой ответ правильный?",
				Options:        options,
			}},
		}
	}

	cases := []struct {
		name    string
		options []repository.TestOption
	}{
		{
			name: "zero correct answers",
			options: []repository.TestOption{
				{OptionTextKK: "Бірінші", OptionTextRU: "Первый"},
				{OptionTextKK: "Екінші", OptionTextRU: "Второй"},
			},
		},
		{
			name: "two correct answers",
			options: []repository.TestOption{
				{OptionTextKK: "Бірінші", OptionTextRU: "Первый", IsCorrect: true},
				{OptionTextKK: "Екінші", OptionTextRU: "Второй", IsCorrect: true},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.UpsertTest(ctx, base(tc.options)); !errors.Is(err, repository.ErrInvalidState) {
				t.Fatalf("expected invalid state, got %v", err)
			}
		})
	}
}

func TestAdminTestCorrectAnswerPersistsOnEditAndReorder(t *testing.T) {
	store, ctx := newTestStore(t)
	lessonID := firstLessonIDForLevel(t, ctx, store, 2)

	test, err := store.UpsertTest(ctx, repository.Test{
		LessonID:    lessonID,
		Title:       "Реттеу тесті",
		PassPercent: 80,
		IsActive:    true,
		Questions: []repository.TestQuestion{{
			QuestionTextKK: "Қайсысы дұрыс?",
			QuestionTextRU: "Что верно?",
			Options: []repository.TestOption{
				{OptionTextKK: "Қате", OptionTextRU: "Неверно", SortOrder: 1},
				{OptionTextKK: "Дұрыс", OptionTextRU: "Верно", IsCorrect: true, SortOrder: 2},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tests, err := store.ListAdminTests(ctx, repository.AdminTestFilter{Lesson: lessonID})
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 || correctOptionText(tests[0]) != "Дұрыс" {
		t.Fatalf("expected saved correct answer, got %#v", tests)
	}

	if _, err := store.UpsertTest(ctx, repository.Test{
		ID:          test.ID,
		LessonID:    lessonID,
		Title:       "Реттелген тест",
		PassPercent: 80,
		IsActive:    true,
		Questions: []repository.TestQuestion{{
			QuestionTextKK: "Қайсысы дұрыс?",
			QuestionTextRU: "Что верно?",
			Options: []repository.TestOption{
				{OptionTextKK: "Дұрыс", OptionTextRU: "Верно", IsCorrect: true, SortOrder: 1},
				{OptionTextKK: "Қате", OptionTextRU: "Неверно", SortOrder: 2},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	tests, err = store.ListAdminTests(ctx, repository.AdminTestFilter{Lesson: lessonID})
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 || correctOptionText(tests[0]) != "Дұрыс" || tests[0].Questions[0].Options[0].OptionTextKK != "Дұрыс" {
		t.Fatalf("expected reordered correct answer to stay on same row, got %#v", tests)
	}
}

func correctOptionText(test repository.Test) string {
	for _, question := range test.Questions {
		for _, option := range question.Options {
			if option.IsCorrect {
				return option.OptionTextKK
			}
		}
	}
	return ""
}

func TestAdminTestCRUDWithQuestions(t *testing.T) {
	store, ctx := newTestStore(t)
	lessonID := firstLessonIDForLevel(t, ctx, store, 2)
	test, err := store.UpsertTest(ctx, repository.Test{
		LessonID:    lessonID,
		Title:       "Қаржы тесті",
		PassPercent: 80,
		IsActive:    true,
		Questions: []repository.TestQuestion{{
			QuestionTextKK: "Бірінші сұрақ",
			QuestionTextRU: "Первый вопрос",
			SortOrder:      1,
			Options: []repository.TestOption{
				{OptionTextKK: "Дұрыс", OptionTextRU: "Верно", IsCorrect: true, SortOrder: 1},
				{OptionTextKK: "Қате", OptionTextRU: "Неверно", IsCorrect: false, SortOrder: 2},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tests, err := store.ListAdminTests(ctx, repository.AdminTestFilter{Level: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) == 0 || tests[0].Title != "Қаржы тесті" || len(tests[0].Questions) != 1 || len(tests[0].Questions[0].Options) != 2 {
		t.Fatalf("unexpected test list: %#v", tests)
	}
	if err := store.DeleteTest(ctx, test.ID); err != nil {
		t.Fatal(err)
	}
	tests, err = store.ListAdminTests(ctx, repository.AdminTestFilter{Level: 2, Status: "inactive"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) == 0 || tests[0].IsActive {
		t.Fatalf("expected inactive test, got %#v", tests)
	}
}

func TestReceiptDuplicateBlocksApprovalWithoutOverride(t *testing.T) {
	store, ctx := newTestStore(t)
	first := registerUser(t, ctx, store, 7001, "")
	second := registerUser(t, ctx, store, 7002, "")
	p1, err := store.CreatePayment(ctx, first.ID, "BASIC", "kaspi_qr", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := store.CreatePayment(ctx, second.ID, "BASIC", "kaspi_qr", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "receipt.pdf")
	body := []byte("Kaspi чек transaction ABC123 Получатель БИН 830520499025 amount 9 900 ₸")
	if err := os.WriteFile(file, body, 0o600); err != nil {
		t.Fatal(err)
	}
	opts := repository.ReceiptValidationOptions{
		ExpectedRecipientBIN: "830520499025",
		AllowedRecipientBINs: []string{"830520499025"},
		SubscriptionDays:     30,
	}
	if _, _, err := store.AttachReceiptToPaymentWithValidation(ctx, first.ID, p1.ID, file, "receipt.pdf", "application/pdf", int64(len(body)), opts); err != nil {
		t.Fatal(err)
	}
	file2 := filepath.Join(dir, "receipt-copy.pdf")
	if err := os.WriteFile(file2, body, 0o600); err != nil {
		t.Fatal(err)
	}
	updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, second.ID, p2.ID, file2, "receipt-copy.pdf", "application/pdf", int64(len(body)), opts)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ValidationStatus != repository.ReceiptStatusDuplicate {
		t.Fatalf("expected duplicate receipt, got %#v", receipt)
	}
	if updated.Status != repository.PaymentStatusRejected {
		t.Fatalf("expected duplicate payment rejected, got %s", updated.Status)
	}
	if _, err := store.ApprovePaymentReviewed(ctx, p2.ID, repository.AdminActor{ID: 1, Role: repository.RoleSuperAdmin}, 30, ""); err == nil {
		t.Fatal("duplicate receipt approved without override comment")
	}
	if _, err := store.ApprovePaymentReviewed(ctx, p2.ID, repository.AdminActor{ID: 1, Role: repository.RoleSuperAdmin}, 30, "manual override"); err == nil {
		t.Fatal("duplicate receipt approved with override comment")
	}
}
