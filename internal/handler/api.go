package handler

import (
	"context"
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
	"zhenis-orda-service/internal/service"

	"go.uber.org/zap"
)

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	sub, _ := s.store.GetActiveSubscription(r.Context(), user.ID)
	progress, _ := s.store.CurrentProgress(r.Context(), user.ID)
	balance, _ := s.store.CoinBalance(r.Context(), user.ID)
	user.Subscription = sub
	user.CoinBalance = balance
	writeJSON(w, http.StatusOK, map[string]any{
		"user":       user,
		"progress":   progress,
		"texts":      service.BrandTexts(user.Language),
		"menu":       miniMenu(user.Language),
		"serverTime": time.Now().UTC(),
	})
}

func (s *Server) handlePlatform(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	tariffs, _ := s.store.ListTariffs(r.Context(), true)
	writeJSON(w, http.StatusOK, map[string]any{
		"name":      "ZHENIS ORDA INSIDE",
		"brand":     "Genius Orda",
		"line":      "Жүйелі өсу ордасы.",
		"status":    "Статус. Мақтаныш. Мотивация.",
		"texts":     service.BrandTexts(user.Language),
		"tariffs":   tariffs,
		"providers": service.SupportedPaymentProviders(),
	})
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	var req struct {
		Answers map[string]string `json:"answers"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.Answers) == 0 {
		writeError(w, http.StatusBadRequest, "invalid diagnostics")
		return
	}
	if err := s.store.SaveDiagnostics(r.Context(), user.ID, req.Answers); mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": i18n.T(user.Language, "diagnostics_done")})
}

func (s *Server) handleTariffs(w http.ResponseWriter, r *http.Request) {
	tariffs, err := s.store.ListTariffs(r.Context(), true)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tariffs": tariffs})
}

func (s *Server) handleCreatePayment(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	var req struct {
		TariffCode string `json:"tariff_code"`
		Provider   string `json:"provider"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	payment, err := s.store.CreatePayment(r.Context(), user.ID, strings.ToUpper(req.TariffCode), req.Provider, s.cfg.PaymentPendingTTL)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"payment": payment,
		"instructions": map[string]any{
			"kaspi_qr_image_url": s.cfg.KaspiQRImageURL,
			"kaspi_pay_url":      s.cfg.KaspiPayURL,
			"halyk_payment_url":  s.cfg.HalykPaymentURL,
			"bank_card_url":      s.cfg.BankCardPaymentURL,
			"text":               "Kaspi QR немесе Kaspi Pay арқылы төлем жасап, түбіртекті Telegram ботқа PDF/image ретінде жіберіңіз.",
		},
	})
}

func (s *Server) handlePayment(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	payment, err := s.store.GetPayment(r.Context(), id)
	if mapRepoError(w, err) {
		return
	}
	if payment.UserID != user.ID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"payment": payment})
}

func (s *Server) handlePaymentReceiptUpload(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	paymentID, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad payment")
		return
	}
	payment, err := s.store.GetPayment(r.Context(), paymentID)
	if mapRepoError(w, err) {
		return
	}
	if payment.UserID != user.ID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if payment.Status != repository.PaymentStatusPending && payment.Status != repository.PaymentStatusUploadedReceipt {
		writeError(w, http.StatusConflict, "payment cannot accept receipt")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxReceiptBytes+1024)
	if err := r.ParseMultipartForm(s.cfg.MaxReceiptBytes + 1024); err != nil {
		writeError(w, http.StatusBadRequest, "receipt file is too large or invalid")
		return
	}
	file, header, err := r.FormFile("receipt")
	if err != nil {
		writeError(w, http.StatusBadRequest, "receipt file is required")
		return
	}
	defer file.Close()

	fileName := filepath.Base(header.Filename)
	ext := strings.ToLower(filepath.Ext(fileName))
	mimeType := header.Header.Get("Content-Type")
	if !allowedReceiptExt(ext) {
		if guessed, _ := mime.ExtensionsByType(mimeType); len(guessed) > 0 {
			ext = guessed[0]
		}
	}
	if !allowedReceiptExt(ext) {
		writeError(w, http.StatusBadRequest, "unsupported receipt file type")
		return
	}
	now := time.Now()
	dir := filepath.Join(s.cfg.PaymentDir, "receipts", now.Format("2006"), now.Format("01"))
	if err := ensureUploadDir(dir); err != nil {
		writeError(w, http.StatusInternalServerError, "receipt directory error")
		return
	}
	if fileName == "." || fileName == string(filepath.Separator) || fileName == "" {
		fileName = "receipt" + ext
	}
	path := filepath.Join(dir, fmt.Sprintf("%s_%d%s", user.ID, now.UnixNano(), ext))
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "receipt save error")
		return
	}
	limited := io.LimitReader(file, s.cfg.MaxReceiptBytes+1)
	written, copyErr := io.Copy(out, limited)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		writeError(w, http.StatusInternalServerError, "receipt save error")
		return
	}
	if written > s.cfg.MaxReceiptBytes {
		_ = os.Remove(path)
		writeError(w, http.StatusBadRequest, "receipt file is too large")
		return
	}
	updated, receipt, err := s.store.AttachReceiptToPayment(r.Context(), user.ID, paymentID, path, fileName, mimeType, written)
	if mapRepoError(w, err) {
		_ = os.Remove(path)
		return
	}
	if s.bot != nil {
		for _, adminID := range s.cfg.AdminIDs {
			text := fmt.Sprintf("Жаңа чек жүктелді\nТөлем: %s\nҚолданушы: %s @%s\nСома: %d KZT\nСтатус: %s", updated.ID, strings.TrimSpace(user.FirstName+" "+user.LastName), user.Username, updated.AmountKZT, receipt.ValidationStatus)
			_ = s.bot.SendMessage(r.Context(), adminID, text)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"payment": updated, "receipt": receipt})
}

func (s *Server) handleSubscription(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	sub, err := s.store.GetActiveSubscription(r.Context(), user.ID)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscription": sub})
}

func (s *Server) handleLevels(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	levels, err := s.store.ListLevels(r.Context(), user.ID)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"levels": levels})
}

func (s *Server) handleLevel(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	levelNumber, err := parsePathInt(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad level")
		return
	}
	level, err := s.store.GetLevel(r.Context(), user.ID, levelNumber)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"level": level})
}

func (s *Server) handleLessons(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	level := 0
	if raw := r.URL.Query().Get("level"); raw != "" {
		level, _ = strconv.Atoi(raw)
	}
	lessons, err := s.store.ListLessons(r.Context(), user.ID, level)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lessons": lessons})
}

func (s *Server) handleLesson(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad lesson")
		return
	}
	lesson, err := s.store.GetLesson(r.Context(), user.ID, id)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lesson": lesson})
}

func (s *Server) handleLessonWatched(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad lesson")
		return
	}
	progress, err := s.store.MarkLessonWatched(r.Context(), user.ID, id)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"progress": progress})
}

func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	level, err := parsePathInt(r, "level_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad level")
		return
	}
	test, err := s.store.GetTestByLevel(r.Context(), user.ID, level)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"test": test})
}

func (s *Server) handleSubmitTest(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	level, err := parsePathInt(r, "level_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad level")
		return
	}
	var req struct {
		Answers map[string]string `json:"answers"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	attempt, progress, err := s.store.SubmitTest(r.Context(), user.ID, level, repository.ParseSelectedAnswers(req.Answers))
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attempt": attempt, "progress": progress})
}

func (s *Server) handleAssignment(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	level, err := parsePathInt(r, "level_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad level")
		return
	}
	assignment, err := s.store.GetAssignmentByLevel(r.Context(), user.ID, level)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"assignment": assignment})
}

func (s *Server) handleSubmitAssignment(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	level, err := parsePathInt(r, "level_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad level")
		return
	}
	var req struct {
		AnswerText string `json:"answer_text"`
		FilePath   string `json:"file_path"`
		LinkURL    string `json:"link_url"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.AnswerText) == "" && strings.TrimSpace(req.FilePath) == "" && strings.TrimSpace(req.LinkURL) == "" {
		writeError(w, http.StatusBadRequest, "empty assignment")
		return
	}
	if err := s.store.SubmitAssignment(r.Context(), user.ID, level, req.AnswerText, req.FilePath, req.LinkURL); mapRepoError(w, err) {
		return
	}
	progress, _ := s.store.RecalculateUserProgress(r.Context(), user.ID)
	writeJSON(w, http.StatusOK, map[string]any{"status": "submitted", "progress": progress})
}

func (s *Server) handleReferral(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	summary, err := s.store.ReferralSummary(r.Context(), user.ID, "zhenisorda_bot")
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"referral": summary})
}

func (s *Server) handleBonuses(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	summary, err := s.store.ReferralSummary(r.Context(), user.ID, "zhenisorda_bot")
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"paid_referrals": summary.PaidCount,
		"rewards":        summary.Rewards,
		"plan": []map[string]any{
			{"count": 1, "reward": "7 күн тегін"},
			{"count": 3, "reward": "1 ай тегін"},
			{"count": 5, "reward": "жабық VIP эфир"},
			{"count": 10, "reward": "жеке мини-талдау"},
			{"count": 20, "reward": "VIP тарифіне 1 ай қолжетімділік"},
			{"count": 50, "reward": "ментормен жеке Zoom"},
		},
	})
}

func (s *Server) handleCoins(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	balance, err := s.store.CoinBalance(r.Context(), user.ID)
	if mapRepoError(w, err) {
		return
	}
	items, _ := s.store.PlaceholderList(r.Context(), "coins")
	writeJSON(w, http.StatusOK, map[string]any{"balance": balance, "transactions": items})
}

func (s *Server) handleStreams(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	streams, err := s.store.ListStreams(r.Context(), user.ID, false)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"streams": streams})
}

func (s *Server) handleChannels(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	channels, err := s.store.ListChannels(r.Context(), user.ID, false)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": channels})
}

func (s *Server) handleIssueInvite(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	channelID, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad channel")
		return
	}
	channel, err := s.store.GetChannel(r.Context(), channelID)
	if mapRepoError(w, err) {
		return
	}
	access, err := s.store.CanAccessChannel(r.Context(), user.ID, channel)
	if mapRepoError(w, err) {
		return
	}
	if !access {
		writeError(w, http.StatusForbidden, "channel locked")
		return
	}
	link := channel.ManualInviteLink
	expiresAt := time.Now().Add(24 * time.Hour)
	if channel.InviteLinkType == "bot" && s.bot != nil {
		if generated, err := s.bot.CreateInviteLink(r.Context(), channel.TelegramChatID, fmt.Sprintf("user-%s-channel-%s", user.ID, channel.ID), expiresAt); err == nil {
			link = generated
		}
	}
	if link == "" {
		writeError(w, http.StatusConflict, "invite link is not configured")
		return
	}
	exp := expiresAt.Format(time.RFC3339)
	_ = s.store.RecordInviteLink(r.Context(), user.ID, channel.ID, link, &exp)
	writeJSON(w, http.StatusOK, map[string]any{"invite_link": link, "expires_at": exp})
}

func (s *Server) handleSupport(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	var req struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Хабарлама мәтінін жазыңыз.")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeError(w, http.StatusBadRequest, "Хабарлама мәтінін жазыңыз.")
		return
	}
	if err := s.store.CreateSupportMessage(r.Context(), user.ID, body); err != nil {
		if s.logger != nil {
			s.logger.Error("support message save failed", zap.String("user_id", user.ID), zap.Int64("telegram_id", user.TelegramID), zap.Error(err))
		}
		writeError(w, http.StatusInternalServerError, i18n.T(user.Language, "support_failed"))
		return
	}
	if err := s.notifySupportAdmins(r.Context(), user, body); err != nil {
		writeError(w, http.StatusBadGateway, i18n.T(user.Language, "support_failed"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": i18n.T(user.Language, "support_received")})
}

func (s *Server) notifySupportAdmins(ctx context.Context, user repository.User, body string) error {
	if s.bot == nil {
		err := fmt.Errorf("telegram bot is not configured")
		s.logSupportNotificationError(err, user, 0)
		return err
	}
	if len(s.cfg.AdminIDs) == 0 {
		err := fmt.Errorf("admin ids are not configured")
		s.logSupportNotificationError(err, user, 0)
		return err
	}

	text := formatSupportAdminMessage(user, body)
	sent := 0
	var lastErr error
	for _, adminID := range s.cfg.AdminIDs {
		if adminID == 0 {
			continue
		}
		if err := s.bot.SendMessage(ctx, adminID, text); err != nil {
			lastErr = err
			s.logSupportNotificationError(err, user, adminID)
			continue
		}
		sent++
	}
	if sent == 0 {
		if lastErr == nil {
			lastErr = fmt.Errorf("no valid admin ids configured")
			s.logSupportNotificationError(lastErr, user, 0)
		}
		return lastErr
	}
	return nil
}

func (s *Server) logSupportNotificationError(err error, user repository.User, adminID int64) {
	if s.logger == nil {
		return
	}
	fields := []zap.Field{
		zap.String("user_id", user.ID),
		zap.Int64("telegram_id", user.TelegramID),
		zap.Error(err),
	}
	if adminID != 0 {
		fields = append(fields, zap.Int64("admin_id", adminID))
	}
	s.logger.Error("support admin notification failed", fields...)
}

func formatSupportAdminMessage(user repository.User, body string) string {
	username := "—"
	if strings.TrimSpace(user.Username) != "" {
		username = "@" + strings.TrimPrefix(strings.TrimSpace(user.Username), "@")
	}
	name := strings.TrimSpace(strings.TrimSpace(user.FirstName) + " " + strings.TrimSpace(user.LastName))
	if name == "" {
		name = "—"
	}
	return fmt.Sprintf("📩 Жаңа қолдау хабарламасы\n\nSource: ZHENIS ORDA Mini App support\nUser ID: %d\nUsername: %s\nАты: %s\n\nХабарлама:\n%s", user.TelegramID, username, name, strings.TrimSpace(body))
}

func miniMenu(language string) []string {
	keys := []string{"menu_level", "menu_lessons", "menu_test", "menu_assignments", "menu_stream", "menu_referral", "menu_bonuses", "menu_payment", "menu_support", "open_mini_app"}
	items := make([]string, 0, len(keys))
	for _, key := range keys {
		items = append(items, i18n.T(language, key))
	}
	return items
}
