package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
