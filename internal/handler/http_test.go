package handler_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"zhenis-orda-service/config"
	"zhenis-orda-service/internal/handler"
	"zhenis-orda-service/internal/repository"
	"zhenis-orda-service/traits/database"
)

func newTestHTTPServer(t *testing.T, env string) *handler.Server {
	return newTestHTTPServerWithConfig(t, env, nil)
}

func newTestHTTPServerWithConfig(t *testing.T, env string, configure func(*config.Config)) *handler.Server {
	srv, _ := newTestHTTPServerWithStoreAndConfig(t, env, configure)
	return srv
}

func newTestHTTPServerWithStore(t *testing.T, env string) (*handler.Server, *repository.Store) {
	return newTestHTTPServerWithStoreAndConfig(t, env, nil)
}

func newTestHTTPServerWithStoreAndConfig(t *testing.T, env string, configure func(*config.Config)) (*handler.Server, *repository.Store) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Token:                         "test-token",
		Port:                          "8080",
		Env:                           env,
		BaseURL:                       "http://localhost:8080",
		MiniAppURL:                    "http://localhost:8080",
		DBPath:                        ":memory:",
		UploadDir:                     t.TempDir(),
		BookUploadDir:                 t.TempDir(),
		FreeLessonUploadDir:           t.TempDir(),
		PaymentDir:                    t.TempDir(),
		AllowedOrigins:                []string{"http://localhost:8080"},
		WhatsAppSalesPhone:            "77476823396",
		PaymentPendingTTL:             time.Hour,
		PaymentRecipientBIN:           "830520499025",
		PaymentAllowedMerchantIINBINs: []string{"830520499025"},
		PaymentAmountToleranceKZT:     0,
		SubscriptionDefaultDays:       30,
		MaxReceiptBytes:               1024 * 1024,
		MaxBookImageBytes:             1024 * 1024,
		MaxFreeLessonImageBytes:       1024 * 1024,
		BrowserSessionTTL:             time.Hour,
		TelegramInitDataMaxAge:        time.Hour,
	}
	if configure != nil {
		configure(&cfg)
	}
	store := repository.New(db)
	return handler.NewServer(cfg, store, handler.NewMemoryKV(), zap.NewNop()), store
}

func acceptLatestLegalAgreement(t *testing.T, srv *handler.Server, telegramID int64) {
	t.Helper()
	query := "?miniapp_dev=1"
	if telegramID != 0 {
		query += "&telegram_id=" + strconv.FormatInt(telegramID, 10)
	}
	statusReq := httptest.NewRequest(http.MethodGet, "/api/legal/agreement-status"+query, nil)
	statusRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("legal status expected 200, got %d: %s", statusRec.Code, statusRec.Body.String())
	}
	var status struct {
		DocumentType    string `json:"document_type"`
		DocumentVersion string `json:"document_version"`
	}
	if err := json.NewDecoder(statusRec.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{
		"language":         "kk",
		"document_type":    status.DocumentType,
		"document_version": status.DocumentVersion,
	})
	acceptReq := httptest.NewRequest(http.MethodPost, "/api/legal/accept"+query, bytes.NewReader(body))
	acceptRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(acceptRec, acceptReq)
	if acceptRec.Code != http.StatusOK {
		t.Fatalf("legal accept expected 200, got %d: %s", acceptRec.Code, acceptRec.Body.String())
	}
}

type sentBotMessage struct {
	chatID int64
	text   string
}

type testInviteBot struct {
	messages  []sentBotMessage
	removed   []removedChatMember
	links     []string
	calls     int
	chatID    string
	name      string
	removeErr error
}

type removedChatMember struct {
	chatID string
	userID int64
}

func (b *testInviteBot) CreateInviteLink(ctx context.Context, chatID, name string, expiresAt time.Time) (string, error) {
	b.calls++
	b.chatID = chatID
	b.name = name
	if len(b.links) > 0 {
		return b.links[0], nil
	}
	return "https://t.me/+test", nil
}

func (b *testInviteBot) SendMessage(ctx context.Context, chatID int64, text string) error {
	b.messages = append(b.messages, sentBotMessage{chatID: chatID, text: text})
	return nil
}

func (b *testInviteBot) RemoveChatMember(ctx context.Context, chatID string, userID int64) error {
	b.removed = append(b.removed, removedChatMember{chatID: chatID, userID: userID})
	return b.removeErr
}

type failingInviteBot struct{}

func (b failingInviteBot) CreateInviteLink(ctx context.Context, chatID, name string, expiresAt time.Time) (string, error) {
	return "", errors.New("telegram says bot is not admin")
}

func (b failingInviteBot) SendMessage(ctx context.Context, chatID int64, text string) error {
	return nil
}

func TestMiniAppDevAuth(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	req := httptest.NewRequest(http.MethodGet, "/api/me?miniapp_dev=1", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["user"] == nil {
		t.Fatal("expected user payload")
	}
}

func TestPaymentRequiresLegalAgreement(t *testing.T) {
	srv, store := newTestHTTPServerWithStore(t, "development")
	req := httptest.NewRequest(http.MethodPost, "/api/payments?miniapp_dev=1&telegram_id=9101", bytes.NewBufferString(`{"tariff_code":"BASIC","provider":"kaspi_qr"}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected legal conflict, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error           string `json:"error"`
		DocumentType    string `json:"document_type"`
		DocumentVersion string `json:"document_version"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error != "LEGAL_AGREEMENT_REQUIRED" || body.DocumentType == "" || body.DocumentVersion == "" {
		t.Fatalf("unexpected legal requirement response: %#v", body)
	}

	var courseID string
	if err := store.DB().QueryRowContext(context.Background(), `SELECT id FROM premium_courses WHERE status = 'active' ORDER BY sort_order ASC LIMIT 1`).Scan(&courseID); err != nil {
		t.Fatal(err)
	}
	courseReq := httptest.NewRequest(http.MethodPost, "/api/premium-courses/"+courseID+"/payments?miniapp_dev=1&telegram_id=9101", bytes.NewBufferString(`{"provider":"kaspi_qr"}`))
	courseRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(courseRec, courseReq)
	if courseRec.Code != http.StatusConflict || !strings.Contains(courseRec.Body.String(), "LEGAL_AGREEMENT_REQUIRED") {
		t.Fatalf("premium course payment expected legal conflict, got %d: %s", courseRec.Code, courseRec.Body.String())
	}
}

func TestLegalDocumentEndpointsAndAcceptance(t *testing.T) {
	srv, store := newTestHTTPServerWithStore(t, "development")
	for _, language := range []string{"kk", "ru"} {
		req := httptest.NewRequest(http.MethodGet, "/api/legal/document?miniapp_dev=1&telegram_id=9102&lang="+language, nil)
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s document expected 200, got %d: %s", language, rec.Code, rec.Body.String())
		}
		var body struct {
			Language        string `json:"language"`
			DocumentVersion string `json:"document_version"`
			ContentHTML     string `json:"content_html"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Language != language || body.DocumentVersion == "" || !strings.Contains(body.ContentHTML, "<") {
			t.Fatalf("unexpected %s document response: %#v", language, body)
		}
		lower := strings.ToLower(body.ContentHTML)
		if strings.Contains(lower, "privacy_policy") || strings.Contains(lower, ".docx") || strings.Contains(lower, "http://") || strings.Contains(lower, "https://") || strings.Contains(lower, "t.me/") {
			t.Fatalf("document response exposed unsafe link/path: %s", body.ContentHTML)
		}
	}

	acceptLatestLegalAgreement(t, srv, 9102)
	user, err := store.GetUserByTelegramID(context.Background(), 9102)
	if err != nil {
		t.Fatal(err)
	}
	statusReq := httptest.NewRequest(http.MethodGet, "/api/legal/agreement-status?miniapp_dev=1&telegram_id=9102", nil)
	statusRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("legal status after accept expected 200, got %d: %s", statusRec.Code, statusRec.Body.String())
	}
	var status struct {
		Accepted        bool   `json:"accepted"`
		DocumentType    string `json:"document_type"`
		DocumentVersion string `json:"document_version"`
	}
	if err := json.NewDecoder(statusRec.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if !status.Accepted {
		t.Fatal("expected accepted legal status")
	}
	agreement, err := store.GetUserLegalAgreement(context.Background(), user.ID, status.DocumentType, status.DocumentVersion)
	if err != nil {
		t.Fatal(err)
	}
	if agreement == nil || agreement.UserID != user.ID || agreement.TelegramID != 9102 {
		t.Fatalf("agreement was not stored for authenticated user: %#v", agreement)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/payments?miniapp_dev=1&telegram_id=9102", bytes.NewBufferString(`{"tariff_code":"BASIC","provider":"kaspi_qr","contact_phone":"+7 700 100 20 30"}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("accepted user payment expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOldLegalAgreementVersionDoesNotUnlockPayment(t *testing.T) {
	srv, store := newTestHTTPServerWithStore(t, "development")
	ctx := context.Background()
	user, _, err := store.RegisterOrUpdateTelegramUser(ctx, repository.TelegramUserInput{
		TelegramID: 9103,
		Username:   "oldlegal",
		FirstName:  "Old",
		Language:   "kk",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcceptUserLegalAgreement(ctx, repository.UserLegalAgreement{
		UserID:           user.ID,
		TelegramID:       user.TelegramID,
		DocumentType:     "privacy_policy_offer",
		DocumentVersion:  "old-version",
		DocumentLanguage: "kk",
		DocumentHash:     "old-hash",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/payments?miniapp_dev=1&telegram_id=9103", bytes.NewBufferString(`{"tariff_code":"BASIC","provider":"kaspi_qr"}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "LEGAL_AGREEMENT_REQUIRED") {
		t.Fatalf("old agreement should not permit payment, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPaymentRequiresContactPhoneAfterLegalAgreement(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	acceptLatestLegalAgreement(t, srv, 9120)
	req := httptest.NewRequest(http.MethodPost, "/api/payments?miniapp_dev=1&telegram_id=9120", bytes.NewBufferString(`{"tariff_code":"BASIC","provider":"kaspi_qr"}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty contact phone expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPaymentContactPhoneSavedAndPendingReused(t *testing.T) {
	srv, store := newTestHTTPServerWithStore(t, "development")
	ctx := context.Background()
	acceptLatestLegalAgreement(t, srv, 9121)

	createPayment := func(phone string) repository.Payment {
		t.Helper()
		body := fmt.Sprintf(`{"tariff_code":"BASIC","provider":"kaspi_qr","contact_phone":%q}`, phone)
		req := httptest.NewRequest(http.MethodPost, "/api/payments?miniapp_dev=1&telegram_id=9121", bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create payment expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var out struct {
			Payment repository.Payment `json:"payment"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out.Payment
	}

	first := createPayment("87001002030")
	if first.ContactPhone != "+77001002030" {
		t.Fatalf("first payment phone = %q", first.ContactPhone)
	}
	updated := createPayment("+7 701 200 30 40")
	if updated.ID != first.ID {
		t.Fatalf("expected pending payment reuse, got first=%s second=%s", first.ID, updated.ID)
	}
	if updated.ContactPhone != "+77012003040" {
		t.Fatalf("updated payment phone = %q", updated.ContactPhone)
	}
	user, err := store.GetUserByTelegramID(ctx, 9121)
	if err != nil {
		t.Fatal(err)
	}
	if user.Phone != "+77012003040" {
		t.Fatalf("user phone = %q", user.Phone)
	}
	var pendingCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE user_id = ? AND status = 'pending'`, user.ID).Scan(&pendingCount); err != nil {
		t.Fatal(err)
	}
	if pendingCount != 1 {
		t.Fatalf("pending payment count = %d", pendingCount)
	}
	if _, err := store.ApprovePayment(ctx, updated.ID, 1, 30); err != nil {
		t.Fatal(err)
	}
	next := createPayment("+7 702 300 40 50")
	if next.ID == updated.ID {
		t.Fatal("expected new payment after approved payment")
	}
	approvedSnapshot, err := store.GetPayment(ctx, updated.ID)
	if err != nil {
		t.Fatal(err)
	}
	if approvedSnapshot.ContactPhone != "+77012003040" {
		t.Fatalf("approved payment phone snapshot changed: %q", approvedSnapshot.ContactPhone)
	}
	if next.ContactPhone != "+77023004050" {
		t.Fatalf("next payment phone = %q", next.ContactPhone)
	}
}

func TestStudentTestResponseDoesNotExposeCorrectAnswers(t *testing.T) {
	srv, store := newTestHTTPServerWithStore(t, "development")
	ctx := context.Background()
	user, _, err := store.RegisterOrUpdateTelegramUser(ctx, repository.TelegramUserInput{
		TelegramID: 9301,
		Username:   "student",
		FirstName:  "Student",
		Language:   "kk",
	})
	if err != nil {
		t.Fatal(err)
	}
	var levelID string
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM levels WHERE number = 1`).Scan(&levelID); err != nil {
		t.Fatal(err)
	}
	lesson, err := store.UpsertLesson(ctx, repository.Lesson{
		LevelID:   levelID,
		TitleKK:   "Тест сабағы",
		TitleRU:   "Тестовый урок",
		VideoURL:  "https://t.me/content/1",
		SortOrder: 1,
		IsActive:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertTest(ctx, repository.Test{
		LessonID:    lesson.ID,
		Title:       "Жасырын жауап тесті",
		PassPercent: 50,
		IsActive:    true,
		Questions: []repository.TestQuestion{{
			QuestionTextKK: "Дұрыс жауап қайсы?",
			QuestionTextRU: "Какой ответ правильный?",
			Options: []repository.TestOption{
				{OptionTextKK: "Дұрыс", OptionTextRU: "Верно", IsCorrect: true, SortOrder: 1},
				{OptionTextKK: "Қате", OptionTextRU: "Неверно", SortOrder: 2},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	payment, err := store.CreatePayment(ctx, user.ID, "BASIC", "kaspi_qr", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApprovePayment(ctx, payment.ID, 1, 30); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tests/1?miniapp_dev=1&telegram_id=9301", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "is_correct") {
		t.Fatalf("student response exposed correct answer flag: %s", rec.Body.String())
	}
}

func TestSubmitTestReturnsAnswerReviewData(t *testing.T) {
	srv, store := newTestHTTPServerWithStore(t, "development")
	ctx := context.Background()
	user, _, err := store.RegisterOrUpdateTelegramUser(ctx, repository.TelegramUserInput{
		TelegramID: 9302,
		Username:   "reviewer",
		FirstName:  "Reviewer",
		Language:   "kk",
	})
	if err != nil {
		t.Fatal(err)
	}
	var levelID string
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM levels WHERE number = 1`).Scan(&levelID); err != nil {
		t.Fatal(err)
	}
	lesson, err := store.UpsertLesson(ctx, repository.Lesson{
		LevelID:   levelID,
		TitleKK:   "Нәтиже сабағы",
		TitleRU:   "Урок результата",
		SortOrder: 1,
		IsActive:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertTest(ctx, repository.Test{
		LessonID:    lesson.ID,
		Title:       "Нәтиже тесті",
		PassPercent: 70,
		IsActive:    true,
		Questions: []repository.TestQuestion{{
			QuestionTextKK: "Дұрыс жауап қайсы?",
			QuestionTextRU: "Какой ответ правильный?",
			SortOrder:      1,
			Options: []repository.TestOption{
				{OptionTextKK: "Дұрыс", OptionTextRU: "Верно", IsCorrect: true, SortOrder: 1},
				{OptionTextKK: "Қате", OptionTextRU: "Неверно", SortOrder: 2},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	payment, err := store.CreatePayment(ctx, user.ID, "BASIC", "kaspi_qr", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApprovePayment(ctx, payment.ID, 1, 30); err != nil {
		t.Fatal(err)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/tests/1?miniapp_dev=1&telegram_id=9302", nil)
	getRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("test get expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}
	var getBody struct {
		Test repository.Test `json:"test"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&getBody); err != nil {
		t.Fatal(err)
	}
	if len(getBody.Test.Questions) == 0 || len(getBody.Test.Questions[0].Options) < 2 {
		t.Fatalf("seed test is incomplete: %#v", getBody.Test)
	}
	if strings.Contains(getRec.Body.String(), "is_correct") {
		t.Fatalf("get test exposed correct answer flag: %s", getRec.Body.String())
	}

	wrongAnswers := map[string]string{}
	rightAnswers := map[string]string{}
	for _, question := range getBody.Test.Questions {
		rightAnswers[question.ID] = question.Options[0].ID
		wrongAnswers[question.ID] = question.Options[1].ID
	}
	wrongBody, _ := json.Marshal(map[string]any{"answers": wrongAnswers})
	wrongReq := httptest.NewRequest(http.MethodPost, "/api/tests/1/submit?miniapp_dev=1&telegram_id=9302", bytes.NewReader(wrongBody))
	wrongRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(wrongRec, wrongReq)
	if wrongRec.Code != http.StatusOK {
		t.Fatalf("wrong submit expected 200, got %d: %s", wrongRec.Code, wrongRec.Body.String())
	}
	var wrongResult struct {
		Attempt      repository.TestAttempt        `json:"attempt"`
		Passed       bool                          `json:"passed"`
		ScorePercent int                           `json:"score_percent"`
		CorrectCount int                           `json:"correct_count"`
		TotalCount   int                           `json:"total_count"`
		PassPercent  int                           `json:"pass_percent"`
		AttemptID    string                        `json:"attempt_id"`
		Results      []repository.TestAnswerResult `json:"results"`
	}
	if err := json.NewDecoder(wrongRec.Body).Decode(&wrongResult); err != nil {
		t.Fatal(err)
	}
	if wrongResult.Passed || wrongResult.Attempt.Passed || wrongResult.ScorePercent != 0 || wrongResult.CorrectCount != 0 {
		t.Fatalf("wrong answers should fail with zero score: %#v", wrongResult)
	}
	if wrongResult.TotalCount != len(getBody.Test.Questions) || wrongResult.PassPercent != getBody.Test.PassPercent || wrongResult.AttemptID == "" {
		t.Fatalf("missing submit summary data: %#v", wrongResult)
	}
	if len(wrongResult.Results) != len(getBody.Test.Questions) {
		t.Fatalf("expected per-question results, got %#v", wrongResult.Results)
	}
	firstWrong := wrongResult.Results[0]
	if firstWrong.IsCorrect || firstWrong.SelectedOptionID != wrongAnswers[firstWrong.QuestionID] || firstWrong.CorrectOptionID == "" || firstWrong.CorrectOptionID == firstWrong.SelectedOptionID {
		t.Fatalf("wrong answer review data is not useful: %#v", firstWrong)
	}

	rightBody, _ := json.Marshal(map[string]any{"answers": rightAnswers})
	rightReq := httptest.NewRequest(http.MethodPost, "/api/tests/1/submit?miniapp_dev=1&telegram_id=9302", bytes.NewReader(rightBody))
	rightRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rightRec, rightReq)
	if rightRec.Code != http.StatusOK {
		t.Fatalf("right submit expected 200, got %d: %s", rightRec.Code, rightRec.Body.String())
	}
	var rightResult struct {
		Passed  bool                          `json:"passed"`
		Results []repository.TestAnswerResult `json:"results"`
	}
	if err := json.NewDecoder(rightRec.Body).Decode(&rightResult); err != nil {
		t.Fatal(err)
	}
	if !rightResult.Passed || len(rightResult.Results) == 0 {
		t.Fatalf("correct answers should pass with review data: %#v", rightResult)
	}
	firstRight := rightResult.Results[0]
	if !firstRight.IsCorrect || firstRight.SelectedOptionID == "" || firstRight.SelectedOptionID != firstRight.CorrectOptionID {
		t.Fatalf("correct answer review data is not useful: %#v", firstRight)
	}
}

func TestMiniAppRequiresInitDataWithoutExplicitDev(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestMiniAppAcceptsSignedTelegramInitData(t *testing.T) {
	srv := newTestHTTPServer(t, "production")
	initData := signedTelegramInitData(t, "test-token", time.Now(), map[string]any{
		"id":            99112233,
		"first_name":    "Yasmina",
		"last_name":     "Inside",
		"username":      "yasmina_inside",
		"language_code": "ru",
		"photo_url":     "https://t.me/i/userpic/320/yasmina.jpg",
	}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("X-Telegram-Init-Data", initData)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		User repository.User `json:"user"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.User.TelegramID != 99112233 {
		t.Fatalf("expected telegram id 99112233, got %d", body.User.TelegramID)
	}
	if body.User.Language != "kk" {
		t.Fatalf("expected kk language, got %q", body.User.Language)
	}
	if body.User.PhotoURL != "https://t.me/i/userpic/320/yasmina.jpg" {
		t.Fatalf("expected photo url to be returned, got %q", body.User.PhotoURL)
	}
}

func TestMiniAppPreservesTelegramPhotoWhenInitDataOmitsIt(t *testing.T) {
	srv := newTestHTTPServer(t, "production")
	firstInitData := signedTelegramInitData(t, "test-token", time.Now(), map[string]any{
		"id":         99112235,
		"first_name": "Aigerim",
		"photo_url":  "https://t.me/i/userpic/320/aigerim.jpg",
	}, nil)
	firstReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	firstReq.Header.Set("X-Telegram-Init-Data", firstInitData)
	firstRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first /api/me expected 200, got %d: %s", firstRec.Code, firstRec.Body.String())
	}

	secondInitData := signedTelegramInitData(t, "test-token", time.Now(), map[string]any{
		"id":         99112235,
		"first_name": "Aigerim",
	}, nil)
	secondReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	secondReq.Header.Set("X-Telegram-Init-Data", secondInitData)
	secondRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("second /api/me expected 200, got %d: %s", secondRec.Code, secondRec.Body.String())
	}
	var body struct {
		User repository.User `json:"user"`
	}
	if err := json.NewDecoder(secondRec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.User.PhotoURL != "https://t.me/i/userpic/320/aigerim.jpg" {
		t.Fatalf("expected existing photo url to be preserved, got %q", body.User.PhotoURL)
	}
}

func TestMiniAppRejectsTamperedTelegramInitData(t *testing.T) {
	srv := newTestHTTPServer(t, "production")
	initData := signedTelegramInitData(t, "test-token", time.Now(), map[string]any{
		"id":         99112234,
		"first_name": "Aruzhan",
	}, nil)
	initData = strings.Replace(initData, "Aruzhan", "Other", 1)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("X-Telegram-Init-Data", initData)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBrowserAdminAuthAndStats(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	loginBody := bytes.NewBufferString(`{"password":"admin"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/browser-auth/login", loginBody)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}

	statsReq := httptest.NewRequest(http.MethodGet, "/api/admin/stats", nil)
	statsReq.AddCookie(cookies[0])
	statsRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(statsRec, statsReq)
	if statsRec.Code != http.StatusOK {
		t.Fatalf("stats expected 200, got %d: %s", statsRec.Code, statsRec.Body.String())
	}
}

func TestAdminBlockUnblockUserPreservesPurchasedAccess(t *testing.T) {
	srv, store := newTestHTTPServerWithStoreAndConfig(t, "development", func(cfg *config.Config) {
		cfg.AdminIDs = []int64{111222}
	})
	cookie := loginAdminCookie(t, srv)
	bot := &testInviteBot{links: []string{"https://t.me/+rejoin"}}
	srv.SetBot(bot)

	ctx := context.Background()
	user, _, err := store.RegisterOrUpdateTelegramUser(ctx, repository.TelegramUserInput{
		TelegramID: 9401,
		Username:   "blocked_user",
		FirstName:  "Blocked User",
		Language:   "kk",
	})
	if err != nil {
		t.Fatal(err)
	}
	var levelID string
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM levels WHERE number = 1`).Scan(&levelID); err != nil {
		t.Fatal(err)
	}
	lesson, err := store.UpsertLesson(ctx, repository.Lesson{
		LevelID:   levelID,
		TitleKK:   "Protected lesson",
		TitleRU:   "Protected lesson",
		VideoURL:  "https://example.test/video",
		SortOrder: 1,
		IsActive:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertTest(ctx, repository.Test{
		LessonID:    lesson.ID,
		Title:       "Protected test",
		PassPercent: 50,
		IsActive:    true,
		Questions: []repository.TestQuestion{{
			QuestionTextKK: "Question",
			QuestionTextRU: "Question",
			Options: []repository.TestOption{
				{OptionTextKK: "Yes", OptionTextRU: "Yes", IsCorrect: true, SortOrder: 1},
				{OptionTextKK: "No", OptionTextRU: "No", SortOrder: 2},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	payment, err := store.CreatePayment(ctx, user.ID, "BASIC", "kaspi_qr", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApprovePayment(ctx, payment.ID, 111222, 30); err != nil {
		t.Fatal(err)
	}
	var courseID string
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM premium_courses WHERE status = 'active' ORDER BY sort_order ASC LIMIT 1`).Scan(&courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GrantPremiumCourseAccess(ctx, user.ID, courseID, repository.PremiumAccessSourceManual, 111222, nil, repository.PremiumAccessDurationLifetime, nil); err != nil {
		t.Fatal(err)
	}
	chatID, err := repository.NormalizeTelegramChatID("2351826422")
	if err != nil {
		t.Fatal(err)
	}
	channel, err := store.UpsertChannel(ctx, repository.Channel{
		Title:             "Paid channel",
		TelegramChatID:    chatID,
		InviteLinkType:    "bot",
		TariffRequirement: "BASIC",
		LevelRequirement:  1,
		IsActive:          true,
	})
	if err != nil {
		t.Fatal(err)
	}

	unauthReq := httptest.NewRequest(http.MethodPost, "/api/admin/users/"+user.ID+"/block", bytes.NewBufferString(`{"reason":"test"}`))
	unauthRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated block expected 401, got %d: %s", unauthRec.Code, unauthRec.Body.String())
	}

	emptyReq := httptest.NewRequest(http.MethodPost, "/api/admin/users/"+user.ID+"/block", bytes.NewBufferString(`{"reason":" "}`))
	emptyReq.AddCookie(cookie)
	emptyRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(emptyRec, emptyReq)
	if emptyRec.Code != http.StatusBadRequest {
		t.Fatalf("empty reason block expected 400, got %d: %s", emptyRec.Code, emptyRec.Body.String())
	}

	blockReq := httptest.NewRequest(http.MethodPost, "/api/admin/users/"+user.ID+"/block", bytes.NewBufferString(`{"reason":"moderation check"}`))
	blockReq.AddCookie(cookie)
	blockRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(blockRec, blockReq)
	if blockRec.Code != http.StatusOK {
		t.Fatalf("block expected 200, got %d: %s", blockRec.Code, blockRec.Body.String())
	}
	blocked, err := store.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !blocked.AccessClosed || blocked.BlockedReason != "moderation check" || blocked.BlockedAt == nil || blocked.BlockedByAdminID == nil || *blocked.BlockedByAdminID != 111222 {
		t.Fatalf("block metadata was not saved: %#v", blocked)
	}
	if len(bot.messages) != 1 || bot.messages[0].chatID != 9401 || !strings.Contains(bot.messages[0].text, "moderation check") {
		t.Fatalf("block notification missing reason: %#v", bot.messages)
	}
	removedChannel := false
	for _, removed := range bot.removed {
		if removed.chatID == channel.TelegramChatID && removed.userID == 9401 {
			removedChannel = true
		}
	}
	if !removedChannel {
		t.Fatalf("block did not remove user from paid channel, removals: %#v", bot.removed)
	}
	sub, err := store.GetActiveSubscription(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sub == nil {
		t.Fatal("block deleted or hid active subscription")
	}
	var activeCourseRows int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM user_course_access WHERE user_id = ? AND course_id = ? AND access_status = 'active'`, user.ID, courseID).Scan(&activeCourseRows); err != nil {
		t.Fatal(err)
	}
	if activeCourseRows != 1 {
		t.Fatalf("block changed premium course access rows, got %d", activeCourseRows)
	}
	if ok, err := store.CanAccessLevel(ctx, user.ID, 1); err != nil || ok {
		t.Fatalf("blocked user should not access level, ok=%v err=%v", ok, err)
	}
	if ok, err := store.HasPremiumCourseAccess(ctx, user.ID, courseID, time.Now()); err != nil || ok {
		t.Fatalf("blocked user should not use premium access, ok=%v err=%v", ok, err)
	}
	testReq := httptest.NewRequest(http.MethodGet, "/api/tests/1?miniapp_dev=1&telegram_id=9401", nil)
	testRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(testRec, testReq)
	if testRec.Code != http.StatusForbidden {
		t.Fatalf("blocked test access expected 403, got %d: %s", testRec.Code, testRec.Body.String())
	}

	unblockReq := httptest.NewRequest(http.MethodPost, "/api/admin/users/"+user.ID+"/unblock", bytes.NewBufferString(`{"reason":"restored"}`))
	unblockReq.AddCookie(cookie)
	unblockRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(unblockRec, unblockReq)
	if unblockRec.Code != http.StatusOK {
		t.Fatalf("unblock expected 200, got %d: %s", unblockRec.Code, unblockRec.Body.String())
	}
	unblocked, err := store.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unblocked.AccessClosed || unblocked.UnblockedAt == nil || unblocked.UnblockedByAdminID == nil || *unblocked.UnblockedByAdminID != 111222 {
		t.Fatalf("unblock metadata was not saved: %#v", unblocked)
	}
	if len(bot.messages) < 2 || !strings.Contains(bot.messages[len(bot.messages)-1].text, "https://t.me/+rejoin") {
		t.Fatalf("unblock notification did not include rejoin link: %#v", bot.messages)
	}
	if ok, err := store.CanAccessLevel(ctx, user.ID, 1); err != nil || !ok {
		t.Fatalf("unblocked user should regain level access, ok=%v err=%v", ok, err)
	}
	if ok, err := store.HasPremiumCourseAccess(ctx, user.ID, courseID, time.Now()); err != nil || !ok {
		t.Fatalf("unblocked user should regain premium access, ok=%v err=%v", ok, err)
	}
	testReq = httptest.NewRequest(http.MethodGet, "/api/tests/1?miniapp_dev=1&telegram_id=9401", nil)
	testRec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(testRec, testReq)
	if testRec.Code != http.StatusOK {
		t.Fatalf("unblocked test access expected 200, got %d: %s", testRec.Code, testRec.Body.String())
	}
}

func loginAdminCookie(t *testing.T, srv *handler.Server) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/browser-auth/login", bytes.NewBufferString(`{"password":"admin"}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected admin session cookie")
	}
	return cookies[0]
}

func multipartBody(t *testing.T, fieldName, fileName string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, writer.FormDataContentType()
}

func tinyPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
		0x42, 0x60, 0x82,
	}
}

func setLevelTelegramChat(t *testing.T, srv *handler.Server, cookie *http.Cookie, levelNumber int, chatID string) repository.Level {
	t.Helper()
	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/levels", nil)
	listReq.AddCookie(cookie)
	listRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("levels expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}
	var listBody struct {
		Levels []repository.Level `json:"levels"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&listBody); err != nil {
		t.Fatal(err)
	}
	var level repository.Level
	for _, item := range listBody.Levels {
		if item.Number == levelNumber {
			level = item
			break
		}
	}
	if level.ID == "" {
		t.Fatalf("level %d not found", levelNumber)
	}
	payload := map[string]any{
		"number":           level.Number,
		"title_kk":         level.TitleKK,
		"title_ru":         level.TitleRU,
		"description_kk":   level.DescriptionKK,
		"description_ru":   level.DescriptionRU,
		"telegram_chat_id": chatID,
		"sort_order":       level.SortOrder,
		"is_active":        level.IsActive,
	}
	raw, _ := json.Marshal(payload)
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/admin/levels/"+level.ID, bytes.NewReader(raw))
	patchReq.AddCookie(cookie)
	patchRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch level expected 200, got %d: %s", patchRec.Code, patchRec.Body.String())
	}
	var patchBody struct {
		Level repository.Level `json:"level"`
	}
	if err := json.NewDecoder(patchRec.Body).Decode(&patchBody); err != nil {
		t.Fatal(err)
	}
	return patchBody.Level
}

func TestAdminStatsRequiresAuth(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/stats", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAdminBooksCRUDAndPublicVisibility(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	cookie := loginAdminCookie(t, srv)

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/books", bytes.NewBufferString(`{
		"title": "Ақша формуласы",
		"description": "Авторлық кітап сипаттамасы",
		"price_kzt": 12000,
		"image_url": "https://example.com/book.webp",
		"is_active": true
	}`))
	createReq.AddCookie(cookie)
	createRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create book expected 200, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Book repository.Book `json:"book"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Book.ID == "" || !created.Book.IsActive {
		t.Fatalf("unexpected created book: %#v", created.Book)
	}

	publicReq := httptest.NewRequest(http.MethodGet, "/api/books?miniapp_dev=1", nil)
	publicRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(publicRec, publicReq)
	if publicRec.Code != http.StatusOK {
		t.Fatalf("public books expected 200, got %d: %s", publicRec.Code, publicRec.Body.String())
	}
	var publicBody struct {
		Books              []repository.Book `json:"books"`
		WhatsAppSalesPhone string            `json:"whatsapp_sales_phone"`
	}
	if err := json.NewDecoder(publicRec.Body).Decode(&publicBody); err != nil {
		t.Fatal(err)
	}
	if len(publicBody.Books) != 1 || publicBody.Books[0].ID != created.Book.ID {
		t.Fatalf("expected one active public book, got %#v", publicBody.Books)
	}
	if publicBody.WhatsAppSalesPhone != "77476823396" {
		t.Fatalf("whatsapp phone = %q", publicBody.WhatsAppSalesPhone)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/books/"+created.Book.ID, nil)
	deleteReq.AddCookie(cookie)
	deleteRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete book expected 200, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}

	publicAfterReq := httptest.NewRequest(http.MethodGet, "/api/books?miniapp_dev=1", nil)
	publicAfterRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(publicAfterRec, publicAfterReq)
	if publicAfterRec.Code != http.StatusOK {
		t.Fatalf("public books after delete expected 200, got %d: %s", publicAfterRec.Code, publicAfterRec.Body.String())
	}
	publicBody.Books = nil
	if err := json.NewDecoder(publicAfterRec.Body).Decode(&publicBody); err != nil {
		t.Fatal(err)
	}
	if len(publicBody.Books) != 0 {
		t.Fatalf("expected inactive book hidden from public API, got %#v", publicBody.Books)
	}
}

func TestAdminBookImageUploadValidationAndServing(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	cookie := loginAdminCookie(t, srv)

	unauthReq := httptest.NewRequest(http.MethodPost, "/api/admin/books/upload-image", nil)
	unauthRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected upload auth 401, got %d", unauthRec.Code)
	}

	badBuf, badType := multipartBody(t, "image", "bad.txt", []byte("not an image"))
	badReq := httptest.NewRequest(http.MethodPost, "/api/admin/books/upload-image", badBuf)
	badReq.Header.Set("Content-Type", badType)
	badReq.AddCookie(cookie)
	badRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("bad image expected 400, got %d: %s", badRec.Code, badRec.Body.String())
	}

	imageBuf, imageType := multipartBody(t, "image", "cover.png", tinyPNG())
	imageReq := httptest.NewRequest(http.MethodPost, "/api/admin/books/upload-image", imageBuf)
	imageReq.Header.Set("Content-Type", imageType)
	imageReq.AddCookie(cookie)
	imageRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(imageRec, imageReq)
	if imageRec.Code != http.StatusOK {
		t.Fatalf("image upload expected 200, got %d: %s", imageRec.Code, imageRec.Body.String())
	}
	var body struct {
		ImageFilePath string `json:"image_file_path"`
		ImageSource   string `json:"image_source"`
	}
	if err := json.NewDecoder(imageRec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body.ImageFilePath, "/uploads/books/") || body.ImageSource != "uploaded" {
		t.Fatalf("unexpected upload payload: %#v", body)
	}
	staticReq := httptest.NewRequest(http.MethodGet, body.ImageFilePath, nil)
	staticRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(staticRec, staticReq)
	if staticRec.Code != http.StatusOK {
		t.Fatalf("uploaded image static serve expected 200, got %d", staticRec.Code)
	}
}

func TestAdminFreeLessonsCRUDAndPublicVisibility(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	cookie := loginAdminCookie(t, srv)

	badReq := httptest.NewRequest(http.MethodPost, "/api/admin/free-lessons", bytes.NewBufferString(`{
		"title": "Тегін сабақ",
		"description": "Ашық сабақ сипаттамасы",
		"image_url": "https://example.com/free.webp",
		"youtube_url": "https://example.com/watch?v=dQw4w9WgXcQ",
		"is_active": true
	}`))
	badReq.AddCookie(cookie)
	badRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid youtube expected 400, got %d: %s", badRec.Code, badRec.Body.String())
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/free-lessons", bytes.NewBufferString(`{
		"title": "Тегін сабақ",
		"short_description": "Қысқаша",
		"description": "Ашық сабақ сипаттамасы",
		"image_url": "https://example.com/free.webp",
		"youtube_url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"is_active": true
	}`))
	createReq.AddCookie(cookie)
	createRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create free lesson expected 200, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		FreeLesson repository.FreeLesson `json:"free_lesson"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	expectedEmbedURL := "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?controls=1&disablekb=1&fs=0&iv_load_policy=3&modestbranding=1&playsinline=1&rel=0"
	if created.FreeLesson.ID == "" || created.FreeLesson.YouTubeVideoID != "dQw4w9WgXcQ" || created.FreeLesson.YouTubeEmbedURL != expectedEmbedURL {
		t.Fatalf("unexpected created free lesson: %#v", created.FreeLesson)
	}

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/admin/free-lessons/"+created.FreeLesson.ID, bytes.NewBufferString(`{
		"title": "Тегін сабақ 2",
		"short_description": "Қысқаша",
		"description": "Ашық сабақ сипаттамасы",
		"image_url": "https://example.com/free.webp",
		"youtube_url": "https://youtu.be/dQw4w9WgXcQ?si=test",
		"is_active": true
	}`))
	patchReq.AddCookie(cookie)
	patchRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch youtu.be expected 200, got %d: %s", patchRec.Code, patchRec.Body.String())
	}
	var patched struct {
		FreeLesson repository.FreeLesson `json:"free_lesson"`
	}
	if err := json.NewDecoder(patchRec.Body).Decode(&patched); err != nil {
		t.Fatal(err)
	}
	if patched.FreeLesson.YouTubeVideoID != "dQw4w9WgXcQ" || patched.FreeLesson.YouTubeEmbedURL != expectedEmbedURL {
		t.Fatalf("unexpected youtu.be parsed free lesson: %#v", patched.FreeLesson)
	}

	shortsReq := httptest.NewRequest(http.MethodPatch, "/api/admin/free-lessons/"+created.FreeLesson.ID, bytes.NewBufferString(`{
		"title": "Тегін сабақ 2",
		"short_description": "Қысқаша",
		"description": "Ашық сабақ сипаттамасы",
		"image_url": "https://example.com/free.webp",
		"youtube_url": "https://www.youtube.com/shorts/dQw4w9WgXcQ",
		"is_active": true
	}`))
	shortsReq.AddCookie(cookie)
	shortsRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(shortsRec, shortsReq)
	if shortsRec.Code != http.StatusOK {
		t.Fatalf("patch shorts expected 200, got %d: %s", shortsRec.Code, shortsRec.Body.String())
	}

	publicReq := httptest.NewRequest(http.MethodGet, "/api/free-lessons?miniapp_dev=1", nil)
	publicRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(publicRec, publicReq)
	if publicRec.Code != http.StatusOK {
		t.Fatalf("public free lessons expected 200, got %d: %s", publicRec.Code, publicRec.Body.String())
	}
	var publicBody struct {
		FreeLessons []repository.FreeLesson `json:"free_lessons"`
	}
	if err := json.NewDecoder(publicRec.Body).Decode(&publicBody); err != nil {
		t.Fatal(err)
	}
	if len(publicBody.FreeLessons) != 1 || publicBody.FreeLessons[0].ID != created.FreeLesson.ID {
		t.Fatalf("expected one active public free lesson, got %#v", publicBody.FreeLessons)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/free-lessons/"+created.FreeLesson.ID+"?miniapp_dev=1", nil)
	detailRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("public free lesson detail expected 200, got %d: %s", detailRec.Code, detailRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/free-lessons/"+created.FreeLesson.ID, nil)
	deleteReq.AddCookie(cookie)
	deleteRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("archive free lesson expected 200, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}

	publicAfterReq := httptest.NewRequest(http.MethodGet, "/api/free-lessons?miniapp_dev=1", nil)
	publicAfterRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(publicAfterRec, publicAfterReq)
	if publicAfterRec.Code != http.StatusOK {
		t.Fatalf("public free lessons after delete expected 200, got %d: %s", publicAfterRec.Code, publicAfterRec.Body.String())
	}
	publicBody.FreeLessons = nil
	if err := json.NewDecoder(publicAfterRec.Body).Decode(&publicBody); err != nil {
		t.Fatal(err)
	}
	if len(publicBody.FreeLessons) != 0 {
		t.Fatalf("expected inactive free lesson hidden from public API, got %#v", publicBody.FreeLessons)
	}
}

func TestAdminFreeLessonImageUploadValidationAndServing(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	cookie := loginAdminCookie(t, srv)

	unauthReq := httptest.NewRequest(http.MethodPost, "/api/admin/free-lessons/upload-image", nil)
	unauthRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected upload auth 401, got %d", unauthRec.Code)
	}

	badBuf, badType := multipartBody(t, "image", "bad.txt", []byte("not an image"))
	badReq := httptest.NewRequest(http.MethodPost, "/api/admin/free-lessons/upload-image", badBuf)
	badReq.Header.Set("Content-Type", badType)
	badReq.AddCookie(cookie)
	badRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("bad free lesson image expected 400, got %d: %s", badRec.Code, badRec.Body.String())
	}

	imageBuf, imageType := multipartBody(t, "image", "cover.png", tinyPNG())
	imageReq := httptest.NewRequest(http.MethodPost, "/api/admin/free-lessons/upload-image", imageBuf)
	imageReq.Header.Set("Content-Type", imageType)
	imageReq.AddCookie(cookie)
	imageRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(imageRec, imageReq)
	if imageRec.Code != http.StatusOK {
		t.Fatalf("free lesson image upload expected 200, got %d: %s", imageRec.Code, imageRec.Body.String())
	}
	var body struct {
		ImageFilePath string `json:"image_file_path"`
		ImageSource   string `json:"image_source"`
	}
	if err := json.NewDecoder(imageRec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body.ImageFilePath, "/uploads/free-lessons/") || body.ImageSource != "uploaded" {
		t.Fatalf("unexpected free lesson upload payload: %#v", body)
	}
	staticReq := httptest.NewRequest(http.MethodGet, body.ImageFilePath, nil)
	staticRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(staticRec, staticReq)
	if staticRec.Code != http.StatusOK {
		t.Fatalf("uploaded free lesson image static serve expected 200, got %d", staticRec.Code)
	}
}

func TestUUIDRouteRejectsBadPaymentID(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	req := httptest.NewRequest(http.MethodGet, "/api/payments/not-a-uuid?miniapp_dev=1", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMiniAppReceiptUpload(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	acceptLatestLegalAgreement(t, srv, 0)
	createReq := httptest.NewRequest(http.MethodPost, "/api/payments?miniapp_dev=1", bytes.NewBufferString(`{"tariff_code":"BASIC","provider":"kaspi_qr","contact_phone":"+7 700 100 20 30"}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, createReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create payment expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Payment repository.Payment `json:"payment"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("receipt", "receipt.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("Kaspi чек transaction XYZ999 Получатель БИН 830520499025 amount 9 900,00 ₸")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	uploadReq := httptest.NewRequest(http.MethodPost, "/api/payments/"+created.Payment.ID+"/receipt?miniapp_dev=1", &buf)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("upload expected 200, got %d: %s", uploadRec.Code, uploadRec.Body.String())
	}
	var uploaded struct {
		Payment repository.Payment `json:"payment"`
		Receipt repository.Receipt `json:"receipt"`
	}
	if err := json.NewDecoder(uploadRec.Body).Decode(&uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.Payment.Status != repository.PaymentStatusApproved {
		t.Fatalf("expected auto-approved payment, got %s with receipt %#v", uploaded.Payment.Status, uploaded.Receipt)
	}
}

func TestMiniAppReceiptUploadRejectsWrongAmountNoAccess(t *testing.T) {
	srv, store := newTestHTTPServerWithStoreAndConfig(t, "development", func(cfg *config.Config) {
		cfg.PaymentAmountToleranceKZT = 500
	})
	ctx := context.Background()
	if _, err := store.DB().ExecContext(ctx, `UPDATE tariffs SET price_kzt = 9500 WHERE code = 'BASIC'`); err != nil {
		t.Fatal(err)
	}
	acceptLatestLegalAgreement(t, srv, 0)
	createReq := httptest.NewRequest(http.MethodPost, "/api/payments?miniapp_dev=1", bytes.NewBufferString(`{"tariff_code":"BASIC","provider":"kaspi_qr","contact_phone":"+7 700 100 20 30"}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, createReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create payment expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Payment repository.Payment `json:"payment"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Payment.AmountKZT != 9500 {
		t.Fatalf("payment amount = %d", created.Payment.AmountKZT)
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("receipt", "receipt.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("Kaspi OFD чек Номер чека QR15625065508 ИИН/БИН продавца 830520499025 Сумма 100 ₸")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	uploadReq := httptest.NewRequest(http.MethodPost, "/api/payments/"+created.Payment.ID+"/receipt?miniapp_dev=1", &buf)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("upload expected 200, got %d: %s", uploadRec.Code, uploadRec.Body.String())
	}
	var uploaded struct {
		Payment repository.Payment `json:"payment"`
		Receipt repository.Receipt `json:"receipt"`
		Message string             `json:"message"`
	}
	if err := json.NewDecoder(uploadRec.Body).Decode(&uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.Payment.Status != repository.PaymentStatusRejected {
		t.Fatalf("expected rejected payment, got %s with receipt %#v", uploaded.Payment.Status, uploaded.Receipt)
	}
	if !strings.Contains(uploaded.Message, "Чектегі сома сәйкес емес") || !strings.Contains(uploaded.Message, "Чектегі сома: 100 ₸") {
		t.Fatalf("unexpected user message: %q", uploaded.Message)
	}
	sub, err := store.GetActiveSubscription(ctx, created.Payment.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if sub != nil {
		t.Fatalf("wrong amount opened subscription: %#v", sub)
	}
	user, err := store.GetUserByID(ctx, created.Payment.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if user.CurrentLevel != 0 {
		t.Fatalf("wrong amount changed level to %d", user.CurrentLevel)
	}
}

func TestMiniAppReceiptUploadRejectsNonPDF(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	acceptLatestLegalAgreement(t, srv, 0)
	createReq := httptest.NewRequest(http.MethodPost, "/api/payments?miniapp_dev=1", bytes.NewBufferString(`{"tariff_code":"BASIC","provider":"kaspi_qr","contact_phone":"+7 700 100 20 30"}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, createReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create payment expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Payment repository.Payment `json:"payment"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("receipt", "receipt.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("not a pdf")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	uploadReq := httptest.NewRequest(http.MethodPost, "/api/payments/"+created.Payment.ID+"/receipt?miniapp_dev=1", &buf)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusBadRequest {
		t.Fatalf("non-PDF upload expected 400, got %d: %s", uploadRec.Code, uploadRec.Body.String())
	}
}

func TestMiniAppSupportNotifiesAdmins(t *testing.T) {
	srv := newTestHTTPServerWithConfig(t, "development", func(cfg *config.Config) {
		cfg.AdminIDs = []int64{111222}
	})
	bot := &testInviteBot{}
	srv.SetBot(bot)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/support?miniapp_dev=1&telegram_id=555777&username=aliya&first_name="+url.QueryEscape("Әлия"),
		bytes.NewBufferString(`{"body":"Сәлем, көмек керек"}`),
	)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("support expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	want := "Хабарламаңыз әкімшіге жіберілді. Жауапты осы чаттан күтіңіз."
	if body.Message != want {
		t.Fatalf("support message = %q, want %q", body.Message, want)
	}
	if len(bot.messages) != 1 {
		t.Fatalf("expected 1 admin notification, got %d", len(bot.messages))
	}
	if bot.messages[0].chatID != 111222 {
		t.Fatalf("admin chat id = %d", bot.messages[0].chatID)
	}
	adminText := bot.messages[0].text
	for _, fragment := range []string{
		"Source: ZHENIS ORDA Mini App support",
		"User ID: 555777",
		"Username: @aliya",
		"Аты: Әлия",
		"Хабарлама:",
		"Сәлем, көмек керек",
	} {
		if !strings.Contains(adminText, fragment) {
			t.Fatalf("admin notification missing %q in %s", fragment, adminText)
		}
	}
}

func TestLevelTelegramInviteRequiresUnlockedLevel(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	cookie := loginAdminCookie(t, srv)
	setLevelTelegramChat(t, srv, cookie, 1, "2351826422")

	req := httptest.NewRequest(http.MethodPost, "/api/levels/1/telegram-invite?miniapp_dev=1&telegram_id=9001", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for locked level, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLevelTelegramInviteCreatesAndReusesLink(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	cookie := loginAdminCookie(t, srv)
	level := setLevelTelegramChat(t, srv, cookie, 1, "2351826422")
	if level.TelegramChatID != "-1002351826422" {
		t.Fatalf("expected normalized chat id, got %q", level.TelegramChatID)
	}
	bot := &testInviteBot{links: []string{"https://t.me/+level-one"}}
	srv.SetBot(bot)

	acceptLatestLegalAgreement(t, srv, 9002)
	createReq := httptest.NewRequest(http.MethodPost, "/api/payments?miniapp_dev=1&telegram_id=9002", bytes.NewBufferString(`{"tariff_code":"BASIC","provider":"kaspi_qr","contact_phone":"+7 700 200 30 40"}`))
	createRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create payment expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Payment repository.Payment `json:"payment"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/api/admin/payments/"+created.Payment.ID+"/approve", bytes.NewBufferString(`{"days":30}`))
	approveReq.AddCookie(cookie)
	approveRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve expected 200, got %d: %s", approveRec.Code, approveRec.Body.String())
	}

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/levels/1/telegram-invite?miniapp_dev=1&telegram_id=9002", bytes.NewBufferString(`{}`))
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("invite %d expected 200, got %d: %s", i+1, rec.Code, rec.Body.String())
		}
		var body struct {
			InviteLink string `json:"invite_link"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.InviteLink != "https://t.me/+level-one" {
			t.Fatalf("invite link = %q", body.InviteLink)
		}
	}
	if bot.calls != 1 {
		t.Fatalf("expected one Telegram API call, got %d", bot.calls)
	}
	if bot.chatID != "-1002351826422" {
		t.Fatalf("bot chat id = %q", bot.chatID)
	}
	if !strings.Contains(bot.name, "user:9002") || !strings.Contains(bot.name, "level:1") {
		t.Fatalf("unexpected invite name %q", bot.name)
	}
}

func TestLevelTelegramInviteFailureIsSafe(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	cookie := loginAdminCookie(t, srv)
	setLevelTelegramChat(t, srv, cookie, 1, "-1002351826422")
	srv.SetBot(failingInviteBot{})

	acceptLatestLegalAgreement(t, srv, 9003)
	createReq := httptest.NewRequest(http.MethodPost, "/api/payments?miniapp_dev=1&telegram_id=9003", bytes.NewBufferString(`{"tariff_code":"BASIC","provider":"kaspi_qr","contact_phone":"+7 700 300 40 50"}`))
	createRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create payment expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Payment repository.Payment `json:"payment"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/api/admin/payments/"+created.Payment.ID+"/approve", bytes.NewBufferString(`{"days":30}`))
	approveReq.AddCookie(cookie)
	approveRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve expected 200, got %d: %s", approveRec.Code, approveRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/levels/1/telegram-invite?miniapp_dev=1&telegram_id=9003", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "bot is not admin") {
		t.Fatalf("unsafe Telegram error leaked to client: %s", rec.Body.String())
	}
}

func TestFinancialIQResultSavedToMe(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	req := httptest.NewRequest(http.MethodPost, "/api/financial-iq?miniapp_dev=1&telegram_id=9010", bytes.NewBufferString(`{
		"score": 88,
		"result_title": "81-140 балл аралығы",
		"result_level": "Қаржылық IQ деңгейі — жоғары",
		"result_text": "Жақсы нәтиже",
		"answers": {"q1":"2"}
	}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("financial iq save expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	meReq := httptest.NewRequest(http.MethodGet, "/api/me?miniapp_dev=1&telegram_id=9010", nil)
	meRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me expected 200, got %d: %s", meRec.Code, meRec.Body.String())
	}
	var body struct {
		FinancialIQ *repository.FinancialIQResult `json:"financial_iq"`
	}
	if err := json.NewDecoder(meRec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.FinancialIQ == nil || body.FinancialIQ.Score != 88 {
		t.Fatalf("unexpected financial iq result: %#v", body.FinancialIQ)
	}
}

func signedTelegramInitData(t *testing.T, token string, authDate time.Time, user map[string]any, extra map[string]string) string {
	t.Helper()
	rawUser, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{
		"auth_date": strconv.FormatInt(authDate.Unix(), 10),
		"user":      string(rawUser),
	}
	for key, value := range extra {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretMAC.Write([]byte(token))
	checkMAC := hmac.New(sha256.New, secretMAC.Sum(nil))
	_, _ = checkMAC.Write([]byte(strings.Join(parts, "\n")))

	query := url.Values{}
	for key, value := range values {
		query.Set(key, value)
	}
	query.Set("hash", hex.EncodeToString(checkMAC.Sum(nil)))
	return query.Encode()
}
