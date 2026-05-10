package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"zhenis-orda-service/internal/i18n"
	"zhenis-orda-service/internal/repository"
	"zhenis-orda-service/internal/service"
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
			"text":               "Kaspi QR немесе Kaspi Pay арқылы төлем жасап, чекті Telegram ботқа PDF/image ретінде жіберіңіз.",
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
		Answers map[string]int64 `json:"answers"`
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
			{"count": 1, "reward": "7 days free"},
			{"count": 3, "reward": "1 month free"},
			{"count": 5, "reward": "closed VIP stream"},
			{"count": 10, "reward": "personal mini-review"},
			{"count": 20, "reward": "1 month VIP tariff access"},
			{"count": 50, "reward": "personal Zoom with mentor"},
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
		if generated, err := s.bot.CreateInviteLink(r.Context(), channel.TelegramChatID, fmt.Sprintf("user-%d-channel-%d", user.ID, channel.ID), expiresAt); err == nil {
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
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Body) == "" {
		writeError(w, http.StatusBadRequest, "empty support message")
		return
	}
	if err := s.store.CreateSupportMessage(r.Context(), user.ID, req.Body); mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": i18n.T(user.Language, "support_received")})
}

func miniMenu(language string) []string {
	keys := []string{"menu_level", "menu_lessons", "menu_test", "menu_assignments", "menu_stream", "menu_referral", "menu_bonuses", "menu_payment", "menu_support", "open_mini_app"}
	items := make([]string, 0, len(keys))
	for _, key := range keys {
		items = append(items, i18n.T(language, key))
	}
	return items
}
