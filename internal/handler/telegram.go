package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
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
	token               string
	apiBase             string
	fileBase            string
	store               *repository.Store
	kv                  KV
	cfgUpload           string
	miniAppURL          string
	adminIDs            []int64
	logger              *zap.Logger
	client              *http.Client
	maxReceipt          int64
	receiptValidation   repository.ReceiptValidationOptions
	testCommandsEnabled bool
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

func (b *TelegramBot) SetTemporaryTestCommandsEnabled(enabled bool) {
	b.testCommandsEnabled = enabled
}

func (b *TelegramBot) SetReceiptValidationOptions(opts repository.ReceiptValidationOptions) {
	b.receiptValidation = opts
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

func (b *TelegramBot) SendDocument(ctx context.Context, chatID int64, filePath, fileName, caption string) error {
	return b.sendDocument(ctx, chatID, filePath, fileName, caption)
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
		if err := b.store.SetLanguage(ctx, user.ID, "kk"); err != nil {
			return err
		}
		return b.sendMainMenu(ctx, msg.Chat.ID, "kk")
	}

	if telegramCommand(text) == "test" {
		return b.handleTemporaryTestInviteCommand(ctx, user, msg.Chat.ID)
	}

	if action := matchMainMenuAction(text, user.Language); action != "" {
		return b.handleMainMenuAction(ctx, user, msg.Chat.ID, action)
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
	default:
		return b.sendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "start"), b.inlineMiniAppMarkup(user.Language))
	}
}

const temporaryTestInviteChatID = "2351826422"

func (b *TelegramBot) handleTemporaryTestInviteCommand(ctx context.Context, user repository.User, replyChatID int64) error {
	// TODO: remove or disable this temporary invite test command before production rollout.
	if !b.testCommandsEnabled || !b.isAdminTelegramUser(user.TelegramID) {
		return b.sendMessage(ctx, replyChatID, "Бұл тест команда тек админдерге арналған.", nil)
	}

	inviteChatID := normalizeTelegramSupergroupChatID(temporaryTestInviteChatID)
	name := fmt.Sprintf("test:%d", user.TelegramID)
	link, err := b.CreateInviteLink(ctx, inviteChatID, name, time.Now().Add(time.Hour))
	if err != nil {
		if b.logger != nil {
			b.logger.Warn(
				"temporary test invite create failed",
				zap.Int64("telegram_id", user.TelegramID),
				zap.String("chat_id", inviteChatID),
				zap.String("error", redactTelegramToken(err.Error(), b.token)),
			)
		}
		return b.sendMessage(ctx, replyChatID, "Сілтеме жасау мүмкін болмады. Бот канал/топта админ екенін тексеріңіз.", nil)
	}

	return b.sendMessage(ctx, replyChatID, "Тестілік бір реттік сілтеме дайын: "+link, nil)
}

func (b *TelegramBot) isAdminTelegramUser(telegramID int64) bool {
	for _, adminID := range b.adminIDs {
		if telegramID == adminID {
			return true
		}
	}
	return false
}

func telegramCommand(text string) string {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return ""
	}
	command := strings.TrimPrefix(fields[0], "/")
	if at := strings.Index(command, "@"); at >= 0 {
		command = command[:at]
	}
	return strings.ToLower(command)
}

func normalizeTelegramSupergroupChatID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "-") {
		return raw
	}
	return "-100" + raw
}

func redactTelegramToken(value, token string) string {
	if token == "" {
		return value
	}
	return strings.ReplaceAll(value, token, "[redacted]")
}

func (b *TelegramBot) sendLanguageSelection(ctx context.Context, chatID int64) error {
	return b.sendMessage(ctx, chatID, i18n.T("kk", "choose_language"), map[string]any{
		"keyboard":          [][]map[string]string{{{"text": "Қазақша"}}},
		"resize_keyboard":   true,
		"one_time_keyboard": true,
	})
}

func (b *TelegramBot) sendMainMenu(ctx context.Context, chatID int64, language string) error {
	text := i18n.T(language, "start")
	reply := map[string]any{
		"keyboard": [][]map[string]any{
			{{"text": menuButtonLabel(language, "menu_level")}, {"text": menuButtonLabel(language, "menu_lessons")}},
			{{"text": menuButtonLabel(language, "menu_test")}, {"text": menuButtonLabel(language, "menu_assignments")}},
			{{"text": menuButtonLabel(language, "menu_stream")}, {"text": menuButtonLabel(language, "menu_referral")}},
			{{"text": menuButtonLabel(language, "menu_bonuses")}, {"text": menuButtonLabel(language, "menu_payment")}},
			{{"text": menuButtonLabel(language, "menu_support")}},
		},
		"resize_keyboard": true,
		"is_persistent":   true,
	}
	if err := b.sendMessage(ctx, chatID, text, reply); err != nil {
		return err
	}
	return b.sendMessage(ctx, chatID, i18n.T(language, "open_mini_app"), b.inlineMiniAppMarkup(language))
}

func (b *TelegramBot) handleMainMenuAction(ctx context.Context, user repository.User, chatID int64, action string) error {
	switch action {
	case "level":
		progress, _ := b.store.CurrentProgress(ctx, user.ID)
		text := fmt.Sprintf("Сіздің деңгейіңіз: Деңгей %d", user.CurrentLevel)
		if progress.NextRequirement != "" {
			text += "\nКелесі талап: " + progress.NextRequirement
		}
		return b.sendMessage(ctx, chatID, text, b.inlineMiniAppMarkup(user.Language))
	case "lessons":
		return b.sendMessage(ctx, chatID, "Сабақтарыңызды Mini App ішінен көріңіз.", b.inlineMiniAppMarkup(user.Language))
	case "test":
		return b.sendMessage(ctx, chatID, "Тест Mini App ішінде ашылады.", b.inlineMiniAppMarkup(user.Language))
	case "assignments":
		return b.sendMessage(ctx, chatID, "Тапсырмаларыңызды Mini App ішінен көріңіз.", b.inlineMiniAppMarkup(user.Language))
	case "stream":
		return b.sendMessage(ctx, chatID, "Жабық эфирлер Mini App ішінде көрсетіледі.", b.inlineMiniAppMarkup(user.Language))
	case "referral":
		summary, _ := b.store.ReferralSummary(ctx, user.ID, "zhenisOrdaFinanceBot")
		return b.sendMessage(ctx, chatID, summary.ReferralLink, b.inlineMiniAppMarkup(user.Language))
	case "bonuses":
		balance, _ := b.store.CoinBalance(ctx, user.ID)
		return b.sendMessage(ctx, chatID, fmt.Sprintf("ZHENIS Coin балансыңыз: %d", balance), b.inlineMiniAppMarkup(user.Language))
	case "payment":
		sub, _ := b.store.GetActiveSubscription(ctx, user.ID)
		if sub == nil {
			return b.sendMessage(ctx, chatID, "Белсенді жазылым жоқ.", b.inlineMiniAppMarkup(user.Language))
		}
		return b.sendMessage(ctx, chatID, fmt.Sprintf("Жазылым: %s\nМерзімі: %s", sub.TariffCode, sub.ExpiresAt.Format("2006-01-02")), b.inlineMiniAppMarkup(user.Language))
	case "support":
		return b.sendMessage(ctx, chatID, "Қолдау қызметіне сұрағыңызды Mini App арқылы жіберіңіз.", b.inlineMiniAppMarkup(user.Language))
	case "miniapp":
		return b.sendMessage(ctx, chatID, i18n.T(user.Language, "open_mini_app"), b.inlineMiniAppMarkup(user.Language))
	default:
		return b.sendMessage(ctx, chatID, i18n.T(user.Language, "start"), b.inlineMiniAppMarkup(user.Language))
	}
}

func menuButtonLabel(language, key string) string {
	if icon := menuButtonIcon(key); icon != "" {
		return icon + " " + i18n.T(language, key)
	}
	return i18n.T(language, key)
}

func menuButtonIcon(key string) string {
	switch key {
	case "menu_level":
		return "📍"
	case "menu_lessons":
		return "📚"
	case "menu_test":
		return "📝"
	case "menu_assignments":
		return "✅"
	case "menu_stream":
		return "🎥"
	case "menu_referral":
		return "🔗"
	case "menu_bonuses":
		return "🪙"
	case "menu_payment":
		return "⏳"
	case "menu_support":
		return "💬"
	case "open_mini_app":
		return "🚀"
	default:
		return ""
	}
}

func matchMainMenuAction(text, language string) string {
	actions := map[string][]string{
		"level":       menuAliases(language, "menu_level"),
		"lessons":     menuAliases(language, "menu_lessons"),
		"test":        menuAliases(language, "menu_test"),
		"assignments": menuAliases(language, "menu_assignments"),
		"stream":      menuAliases(language, "menu_stream"),
		"referral":    menuAliases(language, "menu_referral"),
		"bonuses":     menuAliases(language, "menu_bonuses"),
		"payment":     menuAliases(language, "menu_payment"),
		"support":     menuAliases(language, "menu_support"),
		"miniapp": append(
			menuAliases(language, "open_mini_app"),
			"Open Mini App",
			"Mini App ашу",
			"🚀 Mini App ашу",
		),
	}
	normalized := normalizeMenuText(text)
	for action, aliases := range actions {
		for _, alias := range aliases {
			if normalized == normalizeMenuText(alias) {
				return action
			}
		}
	}
	return ""
}

func menuAliases(language, key string) []string {
	aliases := []string{
		i18n.T(language, key),
		menuButtonLabel(language, key),
		i18n.T("kk", key),
		menuButtonLabel("kk", key),
		i18n.T("ru", key),
		menuButtonLabel("ru", key),
	}
	return append(aliases, legacyMenuAliases(key)...)
}

func legacyMenuAliases(key string) []string {
	switch key {
	case "menu_level":
		return []string{"Мой уровень", "📍 Мой уровень"}
	case "menu_lessons":
		return []string{"Мои уроки", "📚 Мои уроки"}
	case "menu_test":
		return []string{"Пройти тест", "📝 Пройти тест"}
	case "menu_assignments":
		return []string{"Мои задания", "✅ Мои задания"}
	case "menu_stream":
		return []string{"Закрытый эфир", "🎥 Закрытый эфир"}
	case "menu_referral":
		return []string{"Реферальная ссылка", "🔗 Реферальная ссылка", "Реферал сілтемем", "🔗 Реферал сілтемем"}
	case "menu_bonuses":
		return []string{"Бонусы", "🪙 Бонусы", "Бонустарым", "🪙 Бонустарым"}
	case "menu_payment":
		return []string{"Срок оплаты", "⏳ Срок оплаты"}
	case "menu_support":
		return []string{"Поддержка", "💬 Поддержка", "Қолдау қызметі", "💬 Қолдау қызметі"}
	case "open_mini_app":
		return []string{"Открыть Mini App", "🚀 Открыть Mini App", "Mini App ашу", "🚀 Mini App ашу"}
	default:
		return nil
	}
}

func normalizeMenuText(text string) string {
	normalized := strings.TrimSpace(text)
	for _, icon := range []string{"📍", "📚", "📝", "✅", "🎥", "🔗", "🪙", "⏳", "💬", "🚀"} {
		normalized = strings.TrimSpace(strings.TrimPrefix(normalized, icon))
	}
	return strings.ToLower(normalized)
}

func (b *TelegramBot) inlineMiniAppMarkup(language string) map[string]any {
	return map[string]any{
		"inline_keyboard": [][]map[string]any{{
			{"text": menuButtonLabel(language, "open_mini_app"), "web_app": map[string]string{"url": b.miniAppURL}},
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
	return []string{"Атыңыз", "Қалаңыз", "Жасыңыз", "Қазіргі табысыңыз", "Қарызыңыз бар ма?", "Бизнесіңіз бар ма?", "Негізгі проблемаңыз қандай?", "Қай салада өскіңіз келеді?"}
}

type diagnosticsState struct {
	Step    int               `json:"step"`
	Answers map[string]string `json:"answers"`
}

func diagnosticsKey(userID string) string {
	return fmt.Sprintf("diag:%s", userID)
}

func (b *TelegramBot) saveDiagnosticsState(ctx context.Context, userID string, state diagnosticsState) error {
	raw, _ := json.Marshal(state)
	return b.kv.Set(ctx, diagnosticsKey(userID), string(raw), 24*time.Hour)
}

func (b *TelegramBot) getDiagnosticsState(ctx context.Context, userID string) (diagnosticsState, error) {
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
	if msg.Document != nil {
		fileID = msg.Document.FileID
		fileName = msg.Document.FileName
		mimeType = msg.Document.MimeType
		fileSize = msg.Document.FileSize
	} else if len(msg.Photo) > 0 {
		return b.SendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "receipt_bad_file"))
	}
	if fileSize > b.maxReceipt {
		return b.SendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "receipt_too_large"))
	}
	if !isPDFReceipt(fileName, mimeType) {
		return b.SendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "receipt_bad_file"))
	}
	filePath, err := b.downloadTelegramFile(ctx, fileID, user.ID, ".pdf")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "too large") {
			return b.SendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "receipt_too_large"))
		}
		return err
	}
	payment, receipt, err := b.store.AttachReceiptWithValidation(ctx, user.ID, filePath, fileName, mimeType, fileSize, b.receiptValidation)
	if err != nil {
		_ = os.Remove(filePath)
		switch {
		case errors.Is(err, repository.ErrReceiptAlreadyApproved):
			return b.SendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "receipt_already_approved"))
		case errors.Is(err, repository.ErrReceiptDuplicate):
			b.NotifyReceiptAdmins(ctx, user, payment, duplicateReceiptAttempt(receipt))
			return b.SendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "receipt_duplicate"))
		case errors.Is(err, repository.ErrPaymentExpired):
			return b.SendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "payment_expired"))
		case errors.Is(err, repository.ErrPaymentCancelled):
			return b.SendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "payment_cancelled"))
		case errors.Is(err, repository.ErrAmbiguousPayment):
			return b.SendMessage(ctx, msg.Chat.ID, "Сізде бірнеше күту төлем бар. Mini App ішіндегі нақты төлем экранында түбіртекті жүктеңіз.")
		case errors.Is(err, repository.ErrNotFound):
			return b.SendMessage(ctx, msg.Chat.ID, i18n.T(user.Language, "payment_no_pending"))
		default:
			return err
		}
	}
	userMessage := receiptUserMessage(user.Language, payment, receipt)
	if err := b.SendMessage(ctx, msg.Chat.ID, userMessage); err != nil {
		return err
	}
	b.NotifyReceiptAdmins(ctx, user, payment, receipt)
	return nil
}

func isPDFReceipt(fileName, mimeType string) bool {
	ext := strings.ToLower(filepath.Ext(fileName))
	return ext == ".pdf" || strings.EqualFold(strings.TrimSpace(mimeType), "application/pdf")
}

func receiptUserMessage(language string, payment repository.Payment, receipt repository.Receipt) string {
	switch {
	case payment.Status == repository.PaymentStatusApproved:
		return formatPaymentApprovedMessage(language, payment)
	case receiptHasValidationError(receipt, "duplicate_identity_found") || receipt.ValidationStatus == repository.ReceiptStatusDuplicate:
		return i18n.T(language, "receipt_duplicate")
	case receiptHasValidationError(receipt, "amount_mismatch"):
		return formatReceiptWrongAmountMessage(payment, receipt)
	case receiptHasValidationError(receipt, "currency_missing") || receiptHasValidationError(receipt, "currency_mismatch"):
		return "Чектегі валюта сәйкес келмейді.\nҚолжетімділік берілмеді.\nДұрыс чек жіберіңіз немесе қолдауға жазыңыз."
	case receiptHasValidationError(receipt, "recipient_bin_mismatch") || receiptHasValidationError(receipt, "recipient_bin_missing"):
		return "Чектегі сатушы деректері сәйкес келмейді.\nҚолжетімділік берілмеді.\nДұрыс чек жіберіңіз немесе қолдауға жазыңыз."
	case payment.Status == repository.PaymentStatusRejected || receipt.ValidationStatus == repository.ReceiptStatusRejected:
		return "Төлем түбіртегі қабылданбады.\nҚолжетімділік берілмеді.\nДұрыс чек жіберіңіз немесе қолдауға жазыңыз."
	default:
		return i18n.T(language, "payment_uploaded")
	}
}

func receiptUserNotificationNeeded(payment repository.Payment, receipt repository.Receipt) bool {
	if payment.Status == repository.PaymentStatusRejected || receipt.ValidationStatus == repository.ReceiptStatusRejected || receipt.ValidationStatus == repository.ReceiptStatusDuplicate {
		return true
	}
	for _, code := range receipt.ValidationErrors {
		switch code {
		case "duplicate_identity_found", "amount_mismatch", "currency_missing", "currency_mismatch", "recipient_bin_mismatch", "recipient_bin_missing":
			return true
		}
	}
	return false
}

func formatReceiptWrongAmountMessage(payment repository.Payment, receipt repository.Receipt) string {
	parsedAmount := "Анықталмады"
	if receipt.ParsedAmountKZT != nil {
		parsedAmount = formatKZTAmount(*receipt.ParsedAmountKZT) + " ₸"
	}
	return fmt.Sprintf("Төлем сомасы сәйкес келмейді.\nТаңдалған тариф/курс: %s — %s\nЧектегі сома: %s\n\nҚолжетімділік берілмеді.\nДұрыс чек жіберіңіз немесе қолдауға жазыңыз.",
		paymentDisplayTitle(payment),
		optionalKZTAmount(payment.AmountKZT),
		parsedAmount,
	)
}

func receiptHasValidationError(receipt repository.Receipt, code string) bool {
	for _, value := range receipt.ValidationErrors {
		if value == code {
			return true
		}
	}
	return false
}

func (b *TelegramBot) NotifyReceiptAdmins(ctx context.Context, user repository.User, payment repository.Payment, receipt repository.Receipt) {
	caption := formatReceiptAdminMessage(user, payment, receipt)
	filePath := strings.TrimSpace(receipt.FilePath)
	if filePath == "" {
		filePath = strings.TrimSpace(payment.ReceiptFilePath)
	}
	fileName := safeTelegramDocumentFileName(receipt.FileName)
	for _, adminID := range b.adminIDs {
		if adminID == 0 {
			continue
		}
		if err := b.SendDocument(ctx, adminID, filePath, fileName, caption); err != nil {
			if b.logger != nil {
				b.logger.Warn(
					"receipt admin document notification failed",
					zap.Int64("admin_id", adminID),
					zap.String("payment_id", payment.ID),
					zap.String("receipt_id", receipt.ID),
					zap.Error(err),
				)
			}
			continue
		}
	}
}

func (b *TelegramBot) downloadTelegramFile(ctx context.Context, fileID string, userID string, ext string) (string, error) {
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
	path := filepath.Join(dir, fmt.Sprintf("%s_%d%s", userID, now.UnixNano(), ext))
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	defer out.Close()
	limited := io.LimitReader(resp.Body, b.maxReceipt+1)
	written, err := io.Copy(out, limited)
	if err != nil {
		_ = os.Remove(path)
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

func (b *TelegramBot) sendDocument(ctx context.Context, chatID int64, filePath, fileName, caption string) error {
	if chatID == 0 {
		return fmt.Errorf("telegram chat id is empty")
	}
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return fmt.Errorf("receipt file path is empty")
	}
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("chat_id", strconv.FormatInt(chatID, 10)); err != nil {
		return err
	}
	if strings.TrimSpace(caption) != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile("document", safeTelegramDocumentFileName(fileName))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.apiBase+"/sendDocument", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("telegram sendDocument status %d: %s", resp.StatusCode, string(raw))
	}
	var decoded struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return err
	}
	if !decoded.OK {
		return fmt.Errorf("telegram sendDocument failed: %s", decoded.Description)
	}
	return nil
}

func safeTelegramDocumentFileName(fileName string) string {
	fileName = filepath.Base(strings.TrimSpace(fileName))
	if fileName == "" || fileName == "." || fileName == string(filepath.Separator) {
		fileName = "receipt.pdf"
	}
	ext := strings.ToLower(filepath.Ext(fileName))
	base := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	var clean strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z':
			clean.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			clean.WriteRune(r)
		case r >= '0' && r <= '9':
			clean.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			clean.WriteRune(r)
		default:
			clean.WriteRune('_')
		}
	}
	name := strings.Trim(clean.String(), "._-")
	if name == "" {
		name = "receipt"
	}
	if len(name) > 80 {
		name = strings.Trim(name[:80], "._-")
	}
	if ext != ".pdf" {
		ext = ".pdf"
	}
	return name + ext
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
