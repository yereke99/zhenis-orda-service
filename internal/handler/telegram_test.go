package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
