package repository_test

import (
	"context"
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

func TestReferralRegistration(t *testing.T) {
	store, ctx := newTestStore(t)
	inviter := registerUser(t, ctx, store, 1001, "")
	invited := registerUser(t, ctx, store, 1002, inviter.ReferralCode)

	if invited.InvitedByUserID == nil || *invited.InvitedByUserID != inviter.ID {
		t.Fatalf("expected inviter %s, got %#v", inviter.ID, invited.InvitedByUserID)
	}
	summary, err := store.ReferralSummary(ctx, inviter.ID, "zhenisorda_bot")
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

func TestAdminTestCRUDWithQuestions(t *testing.T) {
	store, ctx := newTestStore(t)
	var levelID string
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM levels WHERE number = 2`).Scan(&levelID); err != nil {
		t.Fatal(err)
	}
	test, err := store.UpsertTest(ctx, repository.Test{
		LevelID:     levelID,
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
	body := []byte("Kaspi чек transaction ABC123 amount 4 990 ₸ 10.05.2026")
	if err := os.WriteFile(file, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachReceiptToPayment(ctx, first.ID, p1.ID, file, "receipt.pdf", "application/pdf", int64(len(body))); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApprovePaymentReviewed(ctx, p1.ID, repository.AdminActor{ID: 1, Role: repository.RoleSuperAdmin}, 30, "manual review"); err != nil {
		t.Fatal(err)
	}
	file2 := filepath.Join(dir, "receipt-copy.pdf")
	if err := os.WriteFile(file2, body, 0o600); err != nil {
		t.Fatal(err)
	}
	_, receipt, err := store.AttachReceiptToPayment(ctx, second.ID, p2.ID, file2, "receipt-copy.pdf", "application/pdf", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ValidationStatus != repository.ReceiptStatusDuplicate {
		t.Fatalf("expected duplicate receipt, got %#v", receipt)
	}
	if _, err := store.ApprovePaymentReviewed(ctx, p2.ID, repository.AdminActor{ID: 1, Role: repository.RoleSuperAdmin}, 30, ""); err == nil {
		t.Fatal("duplicate receipt approved without override comment")
	}
}
