package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"zhenis-orda-service/internal/i18n"
	"zhenis-orda-service/internal/repository"

	"go.uber.org/zap"
)

type TelegramBot struct {
	token      string
	apiBase    string
	fileBase   string
	store      *repository.Store
	kv         KV
	cfgUpload  string
	miniAppURL string
	adminIDs   []int64
	logger     *zap.Logger
	client     *http.Client
	maxReceipt int64
}

func NewTelegramBot(token string, store *repository.Store, kv KV, uploadDir, miniAppURL string, adminIDs []int64, maxReceipt int64, logger *zap.Logger) *TelegramBot {
	return &TelegramBot{
		token:      token,
		apiBase:    "https://api.telegram.org/bot" + token,
		fileBase:   "https://api.telegram.org/file/bot" + token,
		store:      store,
		kv:         kv,
		cfgUpload:  uploadDir,
		miniAppURL: miniAppURL,
		adminIDs:   adminIDs,
		logger:     logger,
		client:     &http.Client{Timeout: 60 * time.Second},
		maxReceipt: maxReceipt,
	}
}

func (b *TelegramBot) StartLongPolling(ctx context.Context) {
	if b.token == "" {
		b.logger.Warn("telegram bot token is empty; long polling skipped")
		return
	}
	go func() {
		b.logger.Info("telegram long polling started")
		var offset int64
		for ctx.Err() == nil {
			updates, err := b.getUpdates(ctx, offset)
			if err != nil {
				b.logger.Warn("telegram getUpdates failed", zap.Error(err))
				select {
				case <-time.After(3 * time.Second):
				case <-ctx.Done():
				}
				continue
			}
			for _, update := range updates {
				if update.UpdateID >= offset {
					offset = update.UpdateID + 1
				}
				if err := b.handleUpdate(ctx, update); err != nil {
					b.logger.Warn("telegram update failed", zap.Int64("update_id", update.UpdateID), zap.Error(err))
				}
			}
		}
		b.logger.Info("telegram long polling stopped")
	}()
}

func (b *TelegramBot) SendMessage(ctx context.Context, chatID int64, text string) error {
	return b.sendMessage(ctx, chatID, text, nil)
}

func (b *TelegramBot) CreateInviteLink(ctx context.Context, chatID, name string, expiresAt time.Time) (string, error) {
	var resp struct {
		OK     bool `json:"ok"`
		Result struct {
			InviteLink string `json:"invite_link"`
		} `json:"result"`
		Description string `json:"description"`
	}
	payload := map[string]any{
		"chat_id":              chatID,
		"name":                 name,
		"expire_date":          expiresAt.Unix(),
		"member_limit":         1,
		"creates_join_request": false,
	}
	if err := b.postJSON(ctx, "createChatInviteLink", payload, &resp); err != nil {
		return "", err
	}
	if !resp.OK || resp.Result.InviteLink == "" {
		return "", fmt.Errorf("telegram create invite failed: %s", resp.Description)
	}
	return resp.Result.InviteLink, nil
}

func (b *TelegramBot) getUpdates(ctx context.Context, offset int64) ([]telegramUpdate, error) {
	var resp struct {
		OK     bool             `json:"ok"`
		Result []telegramUpdate `json:"result"`
	}
	payload := map[string]any{"offset": offset, "timeout": 45, "allowed_updates": []string{"message"}}
	if err := b.postJSON(ctx, "getUpdates", payload, &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("telegram getUpdates not ok")
	}
	return resp.Result, nil
}

func (b *TelegramBot) handleUpdate(ctx context.Context, update telegramUpdate) error {
	if update.Message == nil || update.Message.From.ID == 0 {
		return nil
	}
	msg := update.Message
	startParam := ""
	text := strings.TrimSpace(msg.Text)
	if strings.HasPrefix(text, "/start") {
		parts := strings.Fields(text)
		if len(parts) > 1 {
			startParam = parts[1]
		}
	}
	language := "kk"
	if strings.HasPrefix(msg.From.LanguageCode, "ru") {
		language = "ru"
	}
	user, created, err := b.store.RegisterOrUpdateTelegramUser(ctx, repository.TelegramUserInput{
		TelegramID: msg.From.ID,
		Username:   msg.From.Username,
		FirstName:  msg.From.FirstName,
		LastName:   msg.From.LastName,
		Language:   language,
		StartParam: startParam,
	})
	if err != nil {
		return err
	}
	if user.Language == "" {
		user.Language = language
	}

	if msg.Document != nil || len(msg.Photo) > 0 {
		return b.handleReceiptUpload(ctx, user, msg)
	}

	if strings.EqualFold(text, "Қазақша") {
		if err := b.store.SetLanguage(ctx, user.ID, "kk"); err != nil {
			return err
		}
		return b.sendMainMenu(ctx, msg.Chat.ID, "kk")
	}
	if strings.EqualFold(text, "Русский") {
		if err := b.store.SetLanguage(ctx, user.ID, "ru"); err != nil {
			return err
		}
		return b.sendMainMenu(ctx, msg.Chat.ID, "ru")
	}

	if handled, err := b.handleDiagnosticsText(ctx, user, msg); handled || err != nil {
		return err
	}

	if strings.HasPrefix(text, "/start") || created {
		if user.Language == "" {
			return b.sendLanguageSelection(ctx, msg.Chat.ID)
		}
		return b.sendMainMenu(ctx, msg.Chat.ID, user.Language)
	}

	switch text {
	case "Start", "Бастау", "Старт":
		return b.sendMainMenu(ctx, msg.Chat.ID, user.Language)
	case "About platform", "Платформа туралы", "О платформе":
		return b.sendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "about"), b.inlineMiniAppMarkup(user.Language))
	case "Tariffs", "Тарифтер", "Тарифы":
		return b.sendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "tariffs"), b.inlineMiniAppMarkup(user.Language))
	case "Free diagnostics", "Тегін диагностика", "Бесплатная диагностика":
		return b.startDiagnostics(ctx, user, msg.Chat.ID)
	case "Login / Make payment", "Кіру / Төлем жасау", "Войти / Оплатить":
		return b.sendMessage(ctx, msg.Chat.ID, "Mini App арқылы тариф таңдаңыз.", b.inlineMiniAppMarkup(user.Language))
	case i18n.T(user.Language, "menu_referral"):
		summary, _ := b.store.ReferralSummary(ctx, user.ID, "zhenisorda_bot")
		return b.sendMessage(ctx, msg.Chat.ID, summary.ReferralLink, b.inlineMiniAppMarkup(user.Language))
	case i18n.T(user.Language, "menu_payment"):
		sub, _ := b.store.GetActiveSubscription(ctx, user.ID)
		if sub == nil {
			return b.sendMessage(ctx, msg.Chat.ID, "Актив подписка жоқ.", b.inlineMiniAppMarkup(user.Language))
		}
		return b.sendMessage(ctx, msg.Chat.ID, fmt.Sprintf("Подписка: %s\nМерзімі: %s", sub.TariffCode, sub.ExpiresAt.Format("2006-01-02")), b.inlineMiniAppMarkup(user.Language))
	default:
		return b.sendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "start"), b.inlineMiniAppMarkup(user.Language))
	}
}

func (b *TelegramBot) sendLanguageSelection(ctx context.Context, chatID int64) error {
	return b.sendMessage(ctx, chatID, i18n.T("kk", "choose_language"), map[string]any{
		"keyboard":          [][]map[string]string{{{"text": "Қазақша"}, {"text": "Русский"}}},
		"resize_keyboard":   true,
		"one_time_keyboard": true,
	})
}

func (b *TelegramBot) sendMainMenu(ctx context.Context, chatID int64, language string) error {
	text := i18n.T(language, "start")
	reply := map[string]any{
		"keyboard": [][]map[string]string{
			{{"text": i18n.T(language, "menu_level")}, {"text": i18n.T(language, "menu_lessons")}},
			{{"text": i18n.T(language, "menu_test")}, {"text": i18n.T(language, "menu_assignments")}},
			{{"text": i18n.T(language, "menu_stream")}, {"text": i18n.T(language, "menu_referral")}},
			{{"text": i18n.T(language, "menu_bonuses")}, {"text": i18n.T(language, "menu_payment")}},
			{{"text": i18n.T(language, "menu_support")}, {"text": "Open Mini App"}},
		},
		"resize_keyboard": true,
	}
	if err := b.sendMessage(ctx, chatID, text, reply); err != nil {
		return err
	}
	return b.sendMessage(ctx, chatID, i18n.T(language, "open_mini_app"), b.inlineMiniAppMarkup(language))
}

func (b *TelegramBot) inlineMiniAppMarkup(language string) map[string]any {
	return map[string]any{
		"inline_keyboard": [][]map[string]any{{
			{"text": i18n.T(language, "open_mini_app"), "web_app": map[string]string{"url": b.miniAppURL}},
		}},
	}
}

func (b *TelegramBot) startDiagnostics(ctx context.Context, user repository.User, chatID int64) error {
	state := diagnosticsState{Step: 0, Answers: map[string]string{}}
	if err := b.saveDiagnosticsState(ctx, user.ID, state); err != nil {
		return err
	}
	return b.sendMessage(ctx, chatID, diagnosticsQuestions(user.Language)[0], nil)
}

func (b *TelegramBot) handleDiagnosticsText(ctx context.Context, user repository.User, msg *telegramMessage) (bool, error) {
	state, err := b.getDiagnosticsState(ctx, user.ID)
	if err == ErrCacheMiss {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	questions := diagnosticsQuestions(user.Language)
	keys := []string{"name", "city", "age", "income", "has_debt", "has_business", "main_problem", "growth_area"}
	if state.Answers == nil {
		state.Answers = map[string]string{}
	}
	if state.Step < len(keys) {
		state.Answers[keys[state.Step]] = strings.TrimSpace(msg.Text)
	}
	state.Step++
	if state.Step >= len(questions) {
		_ = b.kv.Del(ctx, diagnosticsKey(user.ID))
		if err := b.store.SaveDiagnostics(ctx, user.ID, state.Answers); err != nil {
			return true, err
		}
		return true, b.sendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "diagnostics_done"), b.inlineMiniAppMarkup(user.Language))
	}
	if err := b.saveDiagnosticsState(ctx, user.ID, state); err != nil {
		return true, err
	}
	return true, b.sendMessage(ctx, msg.Chat.ID, questions[state.Step], nil)
}

func diagnosticsQuestions(language string) []string {
	if language == "ru" {
		return []string{"Ваше имя", "Ваш город", "Ваш возраст", "Ваш текущий доход", "Есть ли долги?", "Есть ли бизнес?", "Какая основная проблема?", "В какой сфере хотите вырасти?"}
	}
	return []string{"Атыңыз", "Қалаңыз", "Жасыңыз", "Қазіргі табысыңыз", "Қарызыңыз бар ма?", "Бизнесіңіз бар ма?", "Негізгі проблемаңыз қандай?", "Қай салада өскіңіз келеді?"}
}

type diagnosticsState struct {
	Step    int               `json:"step"`
	Answers map[string]string `json:"answers"`
}

func diagnosticsKey(userID int64) string {
	return fmt.Sprintf("diag:%d", userID)
}

func (b *TelegramBot) saveDiagnosticsState(ctx context.Context, userID int64, state diagnosticsState) error {
	raw, _ := json.Marshal(state)
	return b.kv.Set(ctx, diagnosticsKey(userID), string(raw), 24*time.Hour)
}

func (b *TelegramBot) getDiagnosticsState(ctx context.Context, userID int64) (diagnosticsState, error) {
	raw, err := b.kv.Get(ctx, diagnosticsKey(userID))
	if err != nil {
		return diagnosticsState{}, err
	}
	var state diagnosticsState
	return state, json.Unmarshal([]byte(raw), &state)
}

func (b *TelegramBot) handleReceiptUpload(ctx context.Context, user repository.User, msg *telegramMessage) error {
	fileID := ""
	fileName := ""
	mimeType := ""
	fileSize := int64(0)
	ext := ""
	if msg.Document != nil {
		fileID = msg.Document.FileID
		fileName = msg.Document.FileName
		mimeType = msg.Document.MimeType
		fileSize = msg.Document.FileSize
		ext = strings.ToLower(filepath.Ext(fileName))
	} else if len(msg.Photo) > 0 {
		photo := msg.Photo[len(msg.Photo)-1]
		fileID = photo.FileID
		fileSize = photo.FileSize
		fileName = "receipt.jpg"
		mimeType = "image/jpeg"
		ext = ".jpg"
	}
	if fileSize > b.maxReceipt {
		return b.SendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "receipt_too_large"))
	}
	if !allowedReceiptExt(ext) {
		if guessed, _ := mime.ExtensionsByType(mimeType); len(guessed) > 0 {
			ext = guessed[0]
		}
	}
	if !allowedReceiptExt(ext) {
		return b.SendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "receipt_bad_file"))
	}
	filePath, err := b.downloadTelegramFile(ctx, fileID, user.ID, ext)
	if err != nil {
		return err
	}
	payment, err := b.store.AttachReceipt(ctx, user.ID, filePath, fileName, mimeType, fileSize)
	if err != nil {
		if err == repository.ErrNotFound {
			return b.SendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "payment_no_pending"))
		}
		return err
	}
	if err := b.SendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "payment_uploaded")); err != nil {
		return err
	}
	for _, adminID := range b.adminIDs {
		text := fmt.Sprintf("New receipt uploaded\nPayment #%d\nUser: %s @%s\nAmount: %d KZT\nProvider: %s", payment.ID, strings.TrimSpace(user.FirstName+" "+user.LastName), user.Username, payment.AmountKZT, payment.Provider)
		_ = b.SendMessage(ctx, adminID, text)
	}
	return nil
}

func allowedReceiptExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".pdf", ".jpg", ".jpeg", ".png", ".webp":
		return true
	default:
		return false
	}
}

func (b *TelegramBot) downloadTelegramFile(ctx context.Context, fileID string, userID int64, ext string) (string, error) {
	var fileResp struct {
		OK     bool `json:"ok"`
		Result struct {
			FilePath string `json:"file_path"`
			FileSize int64  `json:"file_size"`
		} `json:"result"`
		Description string `json:"description"`
	}
	if err := b.postJSON(ctx, "getFile", map[string]any{"file_id": fileID}, &fileResp); err != nil {
		return "", err
	}
	if !fileResp.OK || fileResp.Result.FilePath == "" {
		return "", fmt.Errorf("telegram getFile failed: %s", fileResp.Description)
	}
	if fileResp.Result.FileSize > b.maxReceipt {
		return "", fmt.Errorf("telegram file too large")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.fileBase+"/"+fileResp.Result.FilePath, nil)
	if err != nil {
		return "", err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("telegram file download status %d", resp.StatusCode)
	}
	now := time.Now()
	dir := filepath.Join(b.cfgUpload, "receipts", now.Format("2006"), now.Format("01"))
	if err := ensureUploadDir(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%d_%d%s", userID, now.UnixNano(), ext))
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	defer out.Close()
	limited := io.LimitReader(resp.Body, b.maxReceipt+1)
	written, err := io.Copy(out, limited)
	if err != nil {
		return "", err
	}
	if written > b.maxReceipt {
		_ = os.Remove(path)
		return "", fmt.Errorf("file too large")
	}
	return path, nil
}

func (b *TelegramBot) sendMessage(ctx context.Context, chatID int64, text string, replyMarkup any) error {
	payload := map[string]any{"chat_id": chatID, "text": text, "disable_web_page_preview": true}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	var resp struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := b.postJSON(ctx, "sendMessage", payload, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("telegram sendMessage failed: %s", resp.Description)
	}
	return nil
}

func (b *TelegramBot) postJSON(ctx context.Context, method string, payload any, dest any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.apiBase+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("telegram api status %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message"`
}

type telegramMessage struct {
	MessageID int64             `json:"message_id"`
	From      telegramUser      `json:"from"`
	Chat      telegramChat      `json:"chat"`
	Text      string            `json:"text"`
	Document  *telegramDocument `json:"document"`
	Photo     []telegramPhoto   `json:"photo"`
}

type telegramUser struct {
	ID           int64  `json:"id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code"`
}

type telegramChat struct {
	ID int64 `json:"id"`
}

type telegramDocument struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type telegramPhoto struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

func parseStartPayload(text string) string {
	parts := strings.Fields(text)
	if len(parts) > 1 {
		return parts[1]
	}
	return ""
}

func chatIDFromString(raw string) (int64, error) {
	return strconv.ParseInt(raw, 10, 64)
}
