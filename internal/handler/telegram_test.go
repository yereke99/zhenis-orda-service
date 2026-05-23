package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"zhenis-orda-service/internal/i18n"
	"zhenis-orda-service/internal/repository"
	"zhenis-orda-service/traits/database"
)

func TestTelegramStartDefaultsToKazakhWhenTelegramClientIsRussian(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	store := repository.New(db)
	bot := NewTelegramBot("test-token", store, NewMemoryKV(), t.TempDir(), "https://mini.example/app", nil, 1024*1024, zap.NewNop())
	var payloads []map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendMessage" {
			t.Errorf("unexpected telegram method %q", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode telegram payload: %v", err)
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		payloads = append(payloads, payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	t.Cleanup(api.Close)
	bot.apiBase = api.URL

	err = bot.handleUpdate(ctx, telegramUpdate{
		UpdateID: 1,
		Message: &telegramMessage{
			From: telegramUser{
				ID:           123456,
				FirstName:    "Test",
				LanguageCode: "ru",
			},
			Chat: telegramChat{ID: 987654},
			Text: "/start",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	user, err := store.GetUserByTelegramID(ctx, 123456)
	if err != nil {
		t.Fatal(err)
	}
	if user.Language != "kk" {
		t.Fatalf("expected kk language, got %q", user.Language)
	}
	if len(payloads) == 0 {
		t.Fatal("expected telegram sendMessage payload")
	}
	if text, _ := payloads[0]["text"].(string); text != i18n.T("kk", "start") {
		t.Fatalf("unexpected start text: %q", text)
	}
	rawMenu, err := json.Marshal(payloads[0])
	if err != nil {
		t.Fatal(err)
	}
	menu := string(rawMenu)
	for _, expected := range []string{
		"📍 Менің деңгейім",
		"📚 Сабақтарым",
		"📝 Тест тапсыру",
		"✅ Тапсырмаларым",
		"🎥 Жабық эфир",
		"🔗 Дос шақыру",
		"🪙 Бонустар",
		"⏳ Төлем мерзімі",
		"💬 Қолдау",
	} {
		if !strings.Contains(menu, expected) {
			t.Fatalf("expected Kazakh menu button %q, got %s", expected, menu)
		}
	}
	if strings.Contains(menu, "Мой уровень") {
		t.Fatalf("expected no Russian menu button, got %s", menu)
	}
	if strings.Contains(menu, "web_app") || strings.Contains(menu, "Қолданбаны ашу") {
		t.Fatalf("expected Mini App launch outside reply keyboard, got %s", menu)
	}
}

func TestTelegramTemporaryTestCommandCreatesOneTimeInviteLink(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	store := repository.New(db)
	bot := NewTelegramBot("test-token", store, NewMemoryKV(), t.TempDir(), "https://mini.example/app", []int64{123456}, 1024*1024, zap.NewNop())
	bot.SetTemporaryTestCommandsEnabled(true)

	var createPayload map[string]any
	var sent []map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode telegram payload: %v", err)
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/createChatInviteLink":
			createPayload = payload
			_, _ = w.Write([]byte(`{"ok":true,"result":{"invite_link":"https://t.me/+test-once"}}`))
		case "/sendMessage":
			sent = append(sent, payload)
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
		default:
			t.Errorf("unexpected telegram method %q", r.URL.Path)
			http.Error(w, "unexpected method", http.StatusNotFound)
		}
	}))
	t.Cleanup(api.Close)
	bot.apiBase = api.URL

	err = bot.handleUpdate(ctx, telegramUpdate{
		UpdateID: 1,
		Message: &telegramMessage{
			From: telegramUser{ID: 123456, FirstName: "Admin"},
			Chat: telegramChat{ID: 987654},
			Text: "/test",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if createPayload == nil {
		t.Fatal("expected createChatInviteLink payload")
	}
	if got := createPayload["chat_id"]; got != "-1002351826422" {
		t.Fatalf("expected normalized chat_id, got %#v", got)
	}
	if got := createPayload["name"]; got != "test:123456" {
		t.Fatalf("expected invite name, got %#v", got)
	}
	if got := int(createPayload["member_limit"].(float64)); got != 1 {
		t.Fatalf("expected member_limit=1, got %d", got)
	}
	if expiresAt := int64(createPayload["expire_date"].(float64)); expiresAt < time.Now().Add(55*time.Minute).Unix() {
		t.Fatalf("expected short future expire_date, got %d", expiresAt)
	}
	if len(sent) != 1 {
		t.Fatalf("expected exactly one bot message, got %d", len(sent))
	}
	if got := sent[0]["text"]; got != "Тестілік бір реттік сілтеме дайын: https://t.me/+test-once" {
		t.Fatalf("unexpected success message: %#v", got)
	}
}

func TestTelegramTemporaryTestCommandHandlesInviteError(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	store := repository.New(db)
	bot := NewTelegramBot("test-token", store, NewMemoryKV(), t.TempDir(), "https://mini.example/app", []int64{123456}, 1024*1024, zap.NewNop())
	bot.SetTemporaryTestCommandsEnabled(true)

	var sent []map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode telegram payload: %v", err)
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/createChatInviteLink":
			_, _ = w.Write([]byte(`{"ok":false,"description":"bot is not an administrator"}`))
		case "/sendMessage":
			sent = append(sent, payload)
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
		default:
			t.Errorf("unexpected telegram method %q", r.URL.Path)
			http.Error(w, "unexpected method", http.StatusNotFound)
		}
	}))
	t.Cleanup(api.Close)
	bot.apiBase = api.URL

	err = bot.handleUpdate(ctx, telegramUpdate{
		UpdateID: 1,
		Message: &telegramMessage{
			From: telegramUser{ID: 123456, FirstName: "Admin"},
			Chat: telegramChat{ID: 987654},
			Text: "/test",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(sent) != 1 {
		t.Fatalf("expected one friendly error message, got %d", len(sent))
	}
	if got := sent[0]["text"]; got != "Сілтеме жасау мүмкін болмады. Бот канал/топта админ екенін тексеріңіз." {
		t.Fatalf("unexpected friendly error message: %#v", got)
	}
}

func TestTelegramNotifyReceiptAdminsSendsDocumentWithKazakhCaptionToEveryAdmin(t *testing.T) {
	ctx := context.Background()
	receiptPath := filepath.Join(t.TempDir(), "receipt.pdf")
	if err := os.WriteFile(receiptPath, []byte("pdf body"), 0o600); err != nil {
		t.Fatal(err)
	}

	bot := NewTelegramBot("test-token", nil, NewMemoryKV(), t.TempDir(), "https://mini.example/app", []int64{111, 222}, 1024*1024, zap.NewNop())
	type sentDocument struct {
		chatID   string
		caption  string
		fileName string
		body     string
	}
	var documents []sentDocument
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendDocument" {
			t.Errorf("unexpected telegram method %q", r.URL.Path)
			http.Error(w, "unexpected method", http.StatusNotFound)
			return
		}
		if err := r.ParseMultipartForm(1024 * 1024); err != nil {
			t.Errorf("parse multipart: %v", err)
			http.Error(w, "bad multipart", http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("document")
		if err != nil {
			t.Errorf("document file missing: %v", err)
			http.Error(w, "missing document", http.StatusBadRequest)
			return
		}
		defer file.Close()
		raw, _ := io.ReadAll(file)
		chatID := r.FormValue("chat_id")
		documents = append(documents, sentDocument{
			chatID:   chatID,
			caption:  r.FormValue("caption"),
			fileName: header.Filename,
			body:     string(raw),
		})
		w.Header().Set("Content-Type", "application/json")
		if chatID == "111" {
			_, _ = w.Write([]byte(`{"ok":false,"description":"admin blocked bot"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	t.Cleanup(api.Close)
	bot.apiBase = api.URL

	parsedAmount := 8900
	payment := repository.Payment{
		ID:           "payment-1",
		PaymentType:  repository.PaymentTypeSubscription,
		TariffTitle:  "BASIC",
		AmountKZT:    9900,
		Status:       repository.PaymentStatusUploadedReceipt,
		ContactPhone: "+77001002030",
		CreatedAt:    time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC),
	}
	receipt := repository.Receipt{
		ID:                 "receipt-1",
		FilePath:           receiptPath,
		FileName:           "../../bad receipt.pdf",
		FileHash:           "hash",
		ParsedAmountKZT:    &parsedAmount,
		ParsedRecipientBIN: "111111111111",
		ValidationStatus:   repository.ReceiptStatusSuspicious,
		ValidationErrors:   []string{"amount_mismatch", "recipient_bin_mismatch"},
		CreatedAt:          time.Date(2026, 5, 20, 9, 45, 0, 0, time.UTC),
	}
	user := repository.User{
		TelegramID: 777,
		Username:   "aliya",
		FirstName:  "Әлия",
		LastName:   "Нұр",
	}

	bot.NotifyReceiptAdmins(ctx, user, payment, receipt)

	if len(documents) != 2 {
		t.Fatalf("expected sendDocument for both admins, got %d", len(documents))
	}
	if documents[0].chatID != "111" || documents[1].chatID != "222" {
		t.Fatalf("unexpected admin chat ids: %#v", documents)
	}
	for _, doc := range documents {
		if doc.fileName != "bad_receipt.pdf" {
			t.Fatalf("expected safe receipt filename, got %q", doc.fileName)
		}
		if doc.body != "pdf body" {
			t.Fatalf("document body = %q", doc.body)
		}
		if doc.caption == "" {
			t.Fatal("document caption is empty")
		}
		if strings.Contains(doc.caption, receiptPath) || strings.Contains(doc.caption, api.URL) {
			t.Fatalf("caption exposed path or URL: %s", doc.caption)
		}
	}
	caption := documents[1].caption
	for _, fragment := range []string{
		"🧾 Жаңа төлем чегі келді",
		"Қолданушы: Әлия Нұр",
		"Telegram: @aliya",
		"Telegram ID: 777",
		"Байланыс нөмірі: +77001002030",
		"Тариф/курс: BASIC",
		"Күтілген сома: 9 900 ₸",
		"Рұқсат етілген ауытқу: 0 ₸",
		"Статус: қолмен тексеру қажет",
		"Төлем ID: payment-1",
		"Чектегі сома: 8 900 ₸",
		"Сатушы БИН/ИИН: 111111111111",
		"Бірегейлік: бірегей",
		"Тексеру нәтижесі: қолмен тексеру қажет",
		"Себеп: сома сәйкес емес; БИН/ИИН сәйкес емес",
		"Әкімші панелінен тексеріп, төлемді растаңыз немесе қабылдамаңыз.",
	} {
		if !strings.Contains(caption, fragment) {
			t.Fatalf("caption missing %q in:\n%s", fragment, caption)
		}
	}
}

func TestReceiptAdminCaptionUsesFallbacksAndAutoApprovedStatus(t *testing.T) {
	payment := repository.Payment{
		ID:          "payment-2",
		PaymentType: repository.PaymentTypePremiumCourse,
		Status:      repository.PaymentStatusApproved,
		CreatedAt:   time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
	}
	receipt := repository.Receipt{
		ValidationStatus: repository.ReceiptStatusApproved,
		CreatedAt:        time.Date(2026, 5, 20, 10, 5, 0, 0, time.UTC),
	}

	caption := formatReceiptAdminMessage(repository.User{}, payment, receipt)
	for _, fragment := range []string{
		"Қолданушы: Көрсетілмеген",
		"Telegram: Жоқ",
		"Telegram ID: Жоқ",
		"Байланыс нөмірі: Көрсетілмеген",
		"Тариф/курс: Premium курс",
		"Күтілген сома: Анықталмады",
		"Статус: автоматты түрде расталды",
		"Чектегі сома: Анықталмады",
		"Сатушы БИН/ИИН: Анықталмады",
		"Бірегейлік: Анықталмады",
		"Себеп: Қате табылмады",
		"Төлем автоматты түрде расталды.",
	} {
		if !strings.Contains(caption, fragment) {
			t.Fatalf("caption missing %q in:\n%s", fragment, caption)
		}
	}
	for _, raw := range []string{"null", "undefined", "<nil>"} {
		if strings.Contains(caption, raw) {
			t.Fatalf("caption contains raw technical value %q: %s", raw, caption)
		}
	}
}

func TestReceiptAdminCaptionShowsAutoRejectedReason(t *testing.T) {
	parsedAmount := 100
	payment := repository.Payment{
		ID:           "payment-auto-reject",
		PaymentType:  repository.PaymentTypeSubscription,
		TariffTitle:  "BASIC",
		AmountKZT:    9500,
		Status:       repository.PaymentStatusRejected,
		AdminComment: "auto_rejected: amount_mismatch",
		CreatedAt:    time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC),
	}
	receipt := repository.Receipt{
		ParsedAmountKZT:      &parsedAmount,
		ExpectedAmountKZT:    &payment.AmountKZT,
		AmountToleranceKZT:   500,
		ParsedCurrency:       "KZT",
		ParsedRecipientBIN:   "830520499025",
		ExpectedRecipientBIN: "830520499025",
		ParsedCheckID:        "QR15625065508",
		ValidationStatus:     repository.ReceiptStatusRejected,
		ValidationErrors:     []string{"amount_mismatch"},
		CreatedAt:            time.Date(2026, 5, 20, 12, 5, 0, 0, time.UTC),
	}

	caption := formatReceiptAdminMessage(repository.User{TelegramID: 777, FirstName: "Test"}, payment, receipt)
	for _, fragment := range []string{
		"Төлем чегі автоматты түрде қабылданбады",
		"Себеп: сома сәйкес емес",
		"Күтілген сома: 9 500 ₸",
		"Чектегі сома: 100 ₸",
		"Сатушы БИН/ИИН: 830520499025",
		"Чек нөмірі: QR15625065508",
		"Қолжетімділік берілмеді",
	} {
		if !strings.Contains(caption, fragment) {
			t.Fatalf("auto rejected caption missing %q in:\n%s", fragment, caption)
		}
	}
}

func TestReceiptUserMessageExplainsWrongAmount(t *testing.T) {
	parsedAmount := 100
	payment := repository.Payment{
		PaymentType: repository.PaymentTypeSubscription,
		TariffTitle: "BASIC",
		AmountKZT:   9500,
		Status:      repository.PaymentStatusRejected,
	}
	receipt := repository.Receipt{
		ParsedAmountKZT:  &parsedAmount,
		ValidationStatus: repository.ReceiptStatusRejected,
		ValidationErrors: []string{"amount_mismatch"},
	}

	message := receiptUserMessage("kk", payment, receipt)
	for _, fragment := range []string{
		"⚠️ Чектегі сома сәйкес емес.",
		"Күтілетін сома: 9 500 ₸",
		"Чектегі сома: 100 ₸",
		"Дұрыс төлем жасап, жаңа PDF-чекті жіберіңіз.",
	} {
		if !strings.Contains(message, fragment) {
			t.Fatalf("message missing %q in:\n%s", fragment, message)
		}
	}
}

func TestReceiptAdminCaptionMarksDuplicateAttemptForManualReview(t *testing.T) {
	payment := repository.Payment{
		ID:          "payment-3",
		PaymentType: repository.PaymentTypeSubscription,
		TariffTitle: "STANDARD",
		AmountKZT:   24900,
		Status:      repository.PaymentStatusApproved,
		CreatedAt:   time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC),
	}
	receipt := duplicateReceiptAttempt(repository.Receipt{
		ValidationStatus: repository.ReceiptStatusApproved,
		FileHash:         "hash",
		CreatedAt:        time.Date(2026, 5, 20, 11, 5, 0, 0, time.UTC),
	})

	caption := formatReceiptAdminMessage(repository.User{TelegramID: 888}, payment, receipt)
	for _, fragment := range []string{
		"Статус: қолмен тексеру қажет",
		"Бірегейлік: қайталанған чек",
		"Тексеру нәтижесі: қайталанған чек",
		"Себеп: чек бұрын қолданылған",
		"Әкімші панелінен тексеріп, төлемді растаңыз немесе қабылдамаңыз.",
	} {
		if !strings.Contains(caption, fragment) {
			t.Fatalf("duplicate caption missing %q in:\n%s", fragment, caption)
		}
	}
}
