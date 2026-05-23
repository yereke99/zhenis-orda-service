package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"zhenis-orda-service/internal/repository"
)

func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.AdminStats(r.Context())
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stats": stats})
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context(), r.URL.Query().Get("q"), 100, 0)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) handleAdminUser(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	user, err := s.store.GetUserByID(r.Context(), id)
	if mapRepoError(w, err) {
		return
	}
	sub, _ := s.store.GetActiveSubscription(r.Context(), id)
	user.Subscription = sub
	user.CoinBalance, _ = s.store.CoinBalance(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) handleAdminUserAccess(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Closed  bool   `json:"closed"`
		Reason  string `json:"reason"`
		Comment string `json:"comment"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.applyAdminUserAccessChange(w, r, id, req.Closed, firstNonEmpty(req.Reason, req.Comment))
}

func (s *Server) handleAdminBlockUser(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.applyAdminUserAccessChange(w, r, id, true, req.Reason)
}

func (s *Server) handleAdminUnblockUser(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.applyAdminUserAccessChange(w, r, id, false, req.Reason)
}

type adminTelegramDestination struct {
	Source           string
	ID               string
	Title            string
	TelegramChatID   string
	InviteLinkType   string
	ManualInviteLink string
	LevelNumber      int
	ExpiresAt        *time.Time
}

func (s *Server) applyAdminUserAccessChange(w http.ResponseWriter, r *http.Request, userID string, closed bool, reason string) {
	actor := adminFromContext(r.Context())
	reason = strings.TrimSpace(reason)
	if closed && reason == "" {
		writeError(w, http.StatusBadRequest, "block reason is required")
		return
	}
	before, err := s.store.GetUserByID(r.Context(), userID)
	if mapRepoError(w, err) {
		return
	}
	if closed && actor.ID != 0 && before.TelegramID == actor.ID {
		writeError(w, http.StatusConflict, "cannot block current admin")
		return
	}

	destinations := []adminTelegramDestination{}
	warnings := []string{}
	if closed {
		destinations, warnings = s.adminUserTelegramDestinations(r.Context(), before)
	}

	user, err := s.store.SetUserAccessState(r.Context(), userID, closed, actor.ID, reason)
	if mapRepoError(w, err) {
		return
	}

	if closed {
		warnings = append(warnings, s.sendAdminAccessTelegramMessage(r.Context(), user, adminBlockMessage(reason))...)
		warnings = append(warnings, s.removeUserFromTelegramDestinations(r.Context(), user, destinations)...)
	} else {
		destinations, warnings = s.adminUserTelegramDestinations(r.Context(), user)
		links, linkWarnings := s.userRejoinLinks(r.Context(), user, destinations)
		warnings = append(warnings, linkWarnings...)
		warnings = append(warnings, s.sendAdminAccessTelegramMessage(r.Context(), user, adminUnblockMessage(links))...)
	}

	action := "user_unblocked"
	if closed {
		action = "user_blocked"
	}
	_ = s.store.Audit(r.Context(), actor, action, "user", userID, map[string]any{
		"reason":                reason,
		"telegram_id":           user.TelegramID,
		"telegram_destinations": len(destinations),
		"warnings":              warnings,
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "user": user, "warnings": warnings})
}

func (s *Server) adminUserTelegramDestinations(ctx context.Context, user repository.User) ([]adminTelegramDestination, []string) {
	destinations := []adminTelegramDestination{}
	warnings := []string{}
	seen := map[string]bool{}
	add := func(item adminTelegramDestination) {
		item.Title = strings.TrimSpace(item.Title)
		item.TelegramChatID = strings.TrimSpace(item.TelegramChatID)
		item.ManualInviteLink = strings.TrimSpace(item.ManualInviteLink)
		if item.Title == "" {
			item.Title = item.Source
		}
		key := item.Source + ":" + item.ID + ":" + item.TelegramChatID + ":" + item.ManualInviteLink
		if seen[key] {
			return
		}
		seen[key] = true
		destinations = append(destinations, item)
	}

	if channels, err := s.store.ListChannels(ctx, user.ID, false); err == nil {
		for _, channel := range channels {
			if !channel.Access {
				continue
			}
			add(adminTelegramDestination{
				Source:           "channel",
				ID:               channel.ID,
				Title:            channel.Title,
				TelegramChatID:   channel.TelegramChatID,
				InviteLinkType:   channel.InviteLinkType,
				ManualInviteLink: channel.ManualInviteLink,
			})
		}
	} else {
		warnings = append(warnings, "Каналдар тізімін тексеру мүмкін болмады")
		s.logAdminUserAccessWarning(ctx, user, "admin channel destination lookup failed", err)
	}

	if levels, err := s.store.ListLevels(ctx, user.ID); err == nil {
		for _, level := range levels {
			if !level.Access || !level.TelegramConfigured {
				continue
			}
			full, err := s.store.GetLevelByNumber(ctx, level.Number)
			if err != nil || strings.TrimSpace(full.TelegramChatID) == "" {
				continue
			}
			add(adminTelegramDestination{
				Source:         "level",
				ID:             full.ID,
				Title:          fmt.Sprintf("Деңгей %d Telegram", full.Number),
				TelegramChatID: full.TelegramChatID,
				InviteLinkType: "bot",
				LevelNumber:    full.Number,
			})
		}
	} else {
		warnings = append(warnings, "Деңгей Telegram топтарын тексеру мүмкін болмады")
		s.logAdminUserAccessWarning(ctx, user, "admin level destination lookup failed", err)
	}

	if courses, err := s.store.ListUserPremiumCourseAccess(ctx, user.ID); err == nil {
		for _, item := range courses {
			if !item.Active {
				continue
			}
			course := item.Course
			if strings.TrimSpace(course.TelegramChatID) == "" && strings.TrimSpace(course.ManualInviteLink) == "" {
				continue
			}
			var expiresAt *time.Time
			if item.Access != nil {
				expiresAt = item.Access.ExpiresAt
			}
			add(adminTelegramDestination{
				Source:           "premium_course",
				ID:               course.ID,
				Title:            course.Title,
				TelegramChatID:   course.TelegramChatID,
				InviteLinkType:   course.InviteLinkType,
				ManualInviteLink: course.ManualInviteLink,
				ExpiresAt:        expiresAt,
			})
		}
	} else {
		warnings = append(warnings, "Premium курс Telegram қолжетімділігін тексеру мүмкін болмады")
		s.logAdminUserAccessWarning(ctx, user, "admin premium destination lookup failed", err)
	}
	return destinations, warnings
}

func (s *Server) removeUserFromTelegramDestinations(ctx context.Context, user repository.User, destinations []adminTelegramDestination) []string {
	if user.TelegramID == 0 {
		return nil
	}
	remover, ok := s.bot.(TelegramMemberRemover)
	if s.bot == nil || !ok {
		if hasTelegramChatDestinations(destinations) {
			return []string{"Telegram арналарынан шығару үшін бот бапталмаған"}
		}
		return nil
	}
	warnings := []string{}
	seenChats := map[string]bool{}
	for _, destination := range destinations {
		chatID := strings.TrimSpace(destination.TelegramChatID)
		if chatID == "" || seenChats[chatID] {
			continue
		}
		seenChats[chatID] = true
		if err := remover.RemoveChatMember(ctx, chatID, user.TelegramID); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s арнасынан шығару мүмкін болмады", destination.Title))
			s.logAdminUserAccessWarning(ctx, user, "telegram member removal failed", err, zap.String("chat_id", chatID), zap.String("destination", destination.Title))
		}
	}
	return warnings
}

func (s *Server) userRejoinLinks(ctx context.Context, user repository.User, destinations []adminTelegramDestination) ([]string, []string) {
	links := []string{}
	warnings := []string{}
	seenLinks := map[string]bool{}
	for _, destination := range destinations {
		link := strings.TrimSpace(destination.ManualInviteLink)
		expiresAt := time.Now().UTC().Add(24 * time.Hour)
		if destination.ExpiresAt != nil && destination.ExpiresAt.Before(expiresAt) {
			expiresAt = *destination.ExpiresAt
		}
		if link == "" && strings.TrimSpace(destination.TelegramChatID) != "" && (destination.InviteLinkType == "" || destination.InviteLinkType == "bot") {
			if s.bot == nil {
				warnings = append(warnings, fmt.Sprintf("%s шақыру сілтемесін жасау үшін бот бапталмаған", destination.Title))
				continue
			}
			generated, err := s.bot.CreateInviteLink(ctx, destination.TelegramChatID, fmt.Sprintf("user:%d %s:%s", user.TelegramID, destination.Source, destination.ID), expiresAt)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s шақыру сілтемесін жасау мүмкін болмады", destination.Title))
				s.logAdminUserAccessWarning(ctx, user, "telegram invite create failed", err, zap.String("chat_id", destination.TelegramChatID), zap.String("destination", destination.Title))
				continue
			}
			link = generated
			s.recordAdminRejoinInvite(ctx, user, destination, link, expiresAt)
		}
		if link == "" || seenLinks[link] {
			continue
		}
		seenLinks[link] = true
		links = append(links, fmt.Sprintf("• %s: %s", destination.Title, link))
	}
	return links, warnings
}

func (s *Server) recordAdminRejoinInvite(ctx context.Context, user repository.User, destination adminTelegramDestination, link string, expiresAt time.Time) {
	switch destination.Source {
	case "channel":
		exp := expiresAt.Format(time.RFC3339)
		if err := s.store.RecordInviteLink(ctx, user.ID, destination.ID, link, &exp); err != nil {
			s.logAdminUserAccessWarning(ctx, user, "channel invite record failed", err, zap.String("destination_id", destination.ID))
		}
	case "level":
		telegramID := user.TelegramID
		_, err := s.store.CreateLevelTelegramInvite(ctx, repository.UserLevelTelegramInvite{
			UserID:         user.ID,
			TelegramUserID: &telegramID,
			LevelID:        destination.ID,
			TelegramChatID: destination.TelegramChatID,
			InviteLink:     link,
			ExpiresAt:      &expiresAt,
			Status:         "issued",
		})
		if err != nil {
			s.logAdminUserAccessWarning(ctx, user, "level invite record failed", err, zap.String("destination_id", destination.ID))
		}
	case "premium_course":
		_, err := s.store.CreatePremiumCourseTelegramInvite(ctx, repository.PremiumCourseTelegramInvite{
			UserID:         user.ID,
			CourseID:       destination.ID,
			TelegramChatID: firstNonEmpty(destination.TelegramChatID, "manual"),
			InviteLink:     link,
			ExpiresAt:      &expiresAt,
			Status:         "issued",
		})
		if err != nil {
			s.logAdminUserAccessWarning(ctx, user, "premium invite record failed", err, zap.String("destination_id", destination.ID))
		}
	}
}

func (s *Server) sendAdminAccessTelegramMessage(ctx context.Context, user repository.User, message string) []string {
	if s.bot == nil || user.TelegramID == 0 {
		return []string{"Telegram хабарлама жіберілмеді: бот немесе Telegram ID жоқ"}
	}
	if err := s.bot.SendMessage(ctx, user.TelegramID, message); err != nil {
		s.logAdminUserAccessWarning(ctx, user, "telegram access notification failed", err)
		return []string{"Telegram хабарлама жіберу мүмкін болмады"}
	}
	return nil
}

func (s *Server) logAdminUserAccessWarning(ctx context.Context, user repository.User, message string, err error, fields ...zap.Field) {
	_ = ctx
	if s.logger == nil || err == nil {
		return
	}
	base := []zap.Field{
		zap.String("user_id", user.ID),
		zap.Int64("telegram_id", user.TelegramID),
		zap.Error(err),
	}
	base = append(base, fields...)
	s.logger.Warn(message, base...)
}

func hasTelegramChatDestinations(destinations []adminTelegramDestination) bool {
	for _, destination := range destinations {
		if strings.TrimSpace(destination.TelegramChatID) != "" {
			return true
		}
	}
	return false
}

func adminBlockMessage(reason string) string {
	return fmt.Sprintf("🚫 Сіздің платформаға қолжетімділігіңіз уақытша шектелді.\n\nСебебі: %s\n\nСіз уақытша жабық Telegram-арналардан шығарылдыңыз. Қосымша ақпарат алу үшін қолдау қызметіне жазыңыз.", strings.TrimSpace(reason))
}

func adminUnblockMessage(links []string) string {
	channelText := "Mini App ішіндегі арналар бөліміне кіріп, өзіңізге қолжетімді арналарға қайта қосылыңыз."
	if len(links) > 0 {
		channelText = strings.Join(links, "\n")
	}
	return fmt.Sprintf("✅ Сіздің аккаунтыңыз қайта ашылды.\n\nПлатформадағы қолжетімділігіңіз қалпына келтірілді.\n\nТөмендегі қолжетімді Telegram-арналарға қайта қосыла аласыз:\n%s", channelText)
}

func (s *Server) handleAdminUserBonus(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Days       int    `json:"days"`
		TariffCode string `json:"tariff_code"`
		Coins      int    `json:"coins"`
		Reason     string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.TariffCode == "" {
		req.TariffCode = "BASIC"
	}
	if req.Days > 0 {
		if err := s.store.ManualAddSubscriptionDays(r.Context(), id, req.Days, req.TariffCode); mapRepoError(w, err) {
			return
		}
	}
	if req.Coins != 0 {
		if req.Reason == "" {
			req.Reason = "manual_bonus"
		}
		if err := s.store.ManualAdjustCoins(r.Context(), id, req.Coins, req.Reason, actor.ID); mapRepoError(w, err) {
			return
		}
	}
	_ = s.store.Audit(r.Context(), actor, "user_bonus", "user", id, req)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminPayments(w http.ResponseWriter, r *http.Request) {
	payments, err := s.store.ListPayments(r.Context(), r.URL.Query().Get("status"), 100, 0)
	if mapRepoError(w, err) {
		return
	}
	for i := range payments {
		if payments[i].Receipt != nil {
			payments[i].Receipt.AmountToleranceKZT = s.cfg.PaymentAmountToleranceKZT
			payments[i].Receipt.FilePath = "/api/admin/receipts/" + payments[i].Receipt.ID + "/file"
			payments[i].ReceiptFilePath = payments[i].Receipt.FilePath
		} else {
			payments[i].ReceiptFilePath = ""
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"payments": payments})
}

func (s *Server) handleAdminPayment(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	payment, err := s.store.GetPayment(r.Context(), id)
	if mapRepoError(w, err) {
		return
	}
	if receipt, _ := s.store.LatestReceiptForPayment(r.Context(), payment.ID); receipt != nil {
		receipt.AmountToleranceKZT = s.cfg.PaymentAmountToleranceKZT
		receipt.FilePath = "/api/admin/receipts/" + receipt.ID + "/file"
		payment.Receipt = receipt
		payment.ReceiptFilePath = receipt.FilePath
	} else {
		payment.ReceiptFilePath = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"payment": payment})
}

func (s *Server) handleAdminReceiptFile(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad receipt")
		return
	}
	receipt, err := s.store.GetReceipt(r.Context(), id)
	if mapRepoError(w, err) {
		return
	}
	http.ServeFile(w, r, receipt.FilePath)
}

func (s *Server) handleAdminApprovePayment(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Days            int    `json:"days"`
		OverrideComment string `json:"override_comment"`
		Comment         string `json:"comment"`
	}
	_ = decodeJSON(r, &req)
	if req.Days == 0 {
		req.Days = s.cfg.SubscriptionDefaultDays
	}
	override := firstNonEmpty(req.OverrideComment, req.Comment)
	payment, err := s.store.ApprovePaymentReviewed(r.Context(), id, actor, req.Days, override)
	if mapRepoError(w, err) {
		return
	}
	action := "payment_approve"
	if payment.PaymentType == repository.PaymentTypePremiumCourse {
		action = "premium_course_payment_approved"
	}
	_ = s.store.Audit(r.Context(), actor, action, "payment", id, req)
	if s.bot != nil {
		if user, err := s.store.GetUserByID(r.Context(), payment.UserID); err == nil {
			_ = s.bot.SendMessage(r.Context(), user.TelegramID, formatPaymentApprovedMessage(user.Language, payment))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"payment": payment})
}

func (s *Server) handleAdminRejectPayment(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Comment string `json:"comment"`
	}
	_ = decodeJSON(r, &req)
	payment, err := s.store.RejectPayment(r.Context(), id, actor.ID, req.Comment)
	if mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "payment_reject", "payment", id, req)
	if s.bot != nil {
		if user, err := s.store.GetUserByID(r.Context(), payment.UserID); err == nil {
			_ = s.bot.SendMessage(r.Context(), user.TelegramID, formatRejectMessage(user.Language, req.Comment))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"payment": payment})
}

func (s *Server) handleAdminSubscriptions(w http.ResponseWriter, r *http.Request) {
	subs, err := s.store.ListSubscriptions(r.Context(), r.URL.Query().Get("status"), 100, 0)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": subs})
}

func (s *Server) handleAdminPatchSubscription(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.UpdateSubscriptionStatus(r.Context(), id, req.Status); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "subscription_update", "subscription", id, req)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminTariffs(w http.ResponseWriter, r *http.Request) {
	tariffs, err := s.store.ListTariffs(r.Context(), false)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tariffs": tariffs})
}

func (s *Server) handleAdminPostTariff(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	var tariff repository.Tariff
	if err := decodeJSON(r, &tariff); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if raw := r.PathValue("id"); raw != "" {
		if !repository.IsUUID(raw) {
			writeError(w, http.StatusBadRequest, "bad id")
			return
		}
		tariff.ID = raw
	}
	if strings.TrimSpace(tariff.ImageURL) != "" {
		if _, err := url.ParseRequestURI(tariff.ImageURL); err != nil {
			writeError(w, http.StatusBadRequest, "invalid image url")
			return
		}
	}
	out, err := s.store.UpsertTariff(r.Context(), tariff)
	if mapRepoError(w, err) {
		return
	}
	action := "tariff_create"
	if r.Method == http.MethodPatch {
		action = "tariff_update"
	}
	_ = s.store.Audit(r.Context(), actor, action, "tariff", out.ID, out)
	writeJSON(w, http.StatusOK, map[string]any{"tariff": out})
}

func (s *Server) handleAdminArchiveTariff(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.store.ArchiveTariff(r.Context(), id); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "tariff_archive", "tariff", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminTariffImageUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 5*1024*1024+1024)
	if err := r.ParseMultipartForm(5*1024*1024 + 1024); err != nil {
		writeError(w, http.StatusBadRequest, "image file is too large or invalid")
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "image file is required")
		return
	}
	defer file.Close()

	fileName := filepath.Base(header.Filename)
	ext := strings.ToLower(filepath.Ext(fileName))
	mimeType := header.Header.Get("Content-Type")
	if !allowedTariffImageExt(ext) {
		if guessed, _ := mime.ExtensionsByType(mimeType); len(guessed) > 0 {
			ext = guessed[0]
		}
	}
	if !allowedTariffImageExt(ext) {
		writeError(w, http.StatusBadRequest, "unsupported image file type")
		return
	}
	now := time.Now()
	dir := filepath.Join(s.cfg.UploadDir, "tariffs", now.Format("2006"), now.Format("01"))
	if err := ensureUploadDir(dir); err != nil {
		writeError(w, http.StatusInternalServerError, "image directory error")
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("%d%s", now.UnixNano(), ext))
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "image save error")
		return
	}
	limited := io.LimitReader(file, 5*1024*1024+1)
	written, copyErr := io.Copy(out, limited)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		writeError(w, http.StatusInternalServerError, "image save error")
		return
	}
	if written > 5*1024*1024 {
		_ = os.Remove(path)
		writeError(w, http.StatusBadRequest, "image file is too large")
		return
	}
	publicPath := safePublicUploadPath(s.cfg.UploadDir, path)
	writeJSON(w, http.StatusOK, map[string]string{
		"image_file_path": publicPath,
		"image_source":    "uploaded",
	})
}

func allowedTariffImageExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	default:
		return false
	}
}

func (s *Server) handleAdminBooks(w http.ResponseWriter, r *http.Request) {
	books, err := s.store.ListBooks(r.Context(), false)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"books": books})
}

func (s *Server) handleAdminPostBook(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	var req struct {
		ID            string `json:"id"`
		Title         string `json:"title"`
		Description   string `json:"description"`
		PriceKZT      int    `json:"price_kzt"`
		ImageURL      string `json:"image_url"`
		ImageFilePath string `json:"image_file_path"`
		ImageSource   string `json:"image_source"`
		SortOrder     int    `json:"sort_order"`
		IsActive      *bool  `json:"is_active"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if raw := r.PathValue("id"); raw != "" {
		if !repository.IsUUID(raw) {
			writeError(w, http.StatusBadRequest, "bad id")
			return
		}
		req.ID = raw
	}
	if strings.TrimSpace(req.ImageURL) != "" && !isHTTPURL(req.ImageURL) {
		writeError(w, http.StatusBadRequest, "invalid image url")
		return
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	book := repository.Book{
		ID:            req.ID,
		Title:         req.Title,
		Description:   req.Description,
		PriceKZT:      req.PriceKZT,
		ImageURL:      req.ImageURL,
		ImageFilePath: req.ImageFilePath,
		ImageSource:   req.ImageSource,
		SortOrder:     req.SortOrder,
		IsActive:      active,
	}
	out, err := s.store.UpsertBook(r.Context(), book)
	if mapRepoError(w, err) {
		return
	}
	action := "book_create"
	if r.Method == http.MethodPatch {
		action = "book_update"
	}
	_ = s.store.Audit(r.Context(), actor, action, "book", out.ID, out)
	writeJSON(w, http.StatusOK, map[string]any{"book": out})
}

func (s *Server) handleAdminArchiveBook(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.store.ArchiveBook(r.Context(), id); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "book_archive", "book", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminBookImageUpload(w http.ResponseWriter, r *http.Request) {
	maxBytes := s.cfg.MaxBookImageBytes
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024)
	if err := r.ParseMultipartForm(maxBytes + 1024); err != nil {
		writeError(w, http.StatusBadRequest, "image file is too large or invalid")
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "image file is required")
		return
	}
	defer file.Close()

	fileName := filepath.Base(header.Filename)
	if ext := strings.ToLower(filepath.Ext(fileName)); ext != "" && !allowedBookImageExt(ext) {
		writeError(w, http.StatusBadRequest, "unsupported image file type")
		return
	}
	head := make([]byte, 512)
	n, readErr := file.Read(head)
	if readErr != nil && readErr != io.EOF {
		writeError(w, http.StatusBadRequest, "image file is invalid")
		return
	}
	ext, ok := detectedBookImageExt(head[:n])
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported image file type")
		return
	}

	now := time.Now()
	dir := filepath.Join(s.cfg.BookUploadDir, now.Format("2006"), now.Format("01"))
	if err := ensureUploadDir(dir); err != nil {
		writeError(w, http.StatusInternalServerError, "image directory error")
		return
	}
	path := filepath.Join(dir, uuid.NewString()+ext)
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "image save error")
		return
	}
	limited := io.LimitReader(io.MultiReader(bytes.NewReader(head[:n]), file), maxBytes+1)
	written, copyErr := io.Copy(out, limited)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		writeError(w, http.StatusInternalServerError, "image save error")
		return
	}
	if written > maxBytes {
		_ = os.Remove(path)
		writeError(w, http.StatusBadRequest, "image file is too large")
		return
	}
	publicPath := safePublicBookUploadPath(s.cfg.BookUploadDir, path)
	if publicPath == "" {
		_ = os.Remove(path)
		writeError(w, http.StatusInternalServerError, "image path error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"image_file_path": publicPath,
		"image_source":    "uploaded",
	})
}

func (s *Server) handleAdminPremiumCourses(w http.ResponseWriter, r *http.Request) {
	courses, err := s.store.ListAdminPremiumCourses(r.Context())
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"premium_courses": courses})
}

func (s *Server) handleAdminPremiumCourse(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad course")
		return
	}
	course, err := s.store.GetPremiumCourse(r.Context(), id, false)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"premium_course": course})
}

func (s *Server) handleAdminPostPremiumCourse(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	var course repository.PremiumCourse
	if err := decodeJSON(r, &course); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if raw := r.PathValue("id"); raw != "" {
		if !repository.IsUUID(raw) {
			writeError(w, http.StatusBadRequest, "bad id")
			return
		}
		course.ID = raw
	}
	if strings.TrimSpace(course.ManualInviteLink) != "" && !isTelegramLink(course.ManualInviteLink) {
		writeError(w, http.StatusBadRequest, "invalid telegram link")
		return
	}
	if strings.TrimSpace(course.CoverImageURL) != "" && !isHTTPURL(course.CoverImageURL) {
		writeError(w, http.StatusBadRequest, "invalid cover image url")
		return
	}
	out, err := s.store.UpsertPremiumCourse(r.Context(), course)
	if mapRepoError(w, err) {
		return
	}
	action := "premium_course_create"
	if r.Method == http.MethodPatch {
		action = "premium_course_update"
	}
	_ = s.store.Audit(r.Context(), actor, action, "premium_course", out.ID, out)
	writeJSON(w, http.StatusOK, map[string]any{"premium_course": out})
}

func (s *Server) handleAdminArchivePremiumCourse(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad course")
		return
	}
	if err := s.store.ArchivePremiumCourse(r.Context(), id); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "premium_course_archive", "premium_course", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminPremiumCourseCoverUpload(w http.ResponseWriter, r *http.Request) {
	maxBytes := s.cfg.MaxBookImageBytes
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024)
	if err := r.ParseMultipartForm(maxBytes + 1024); err != nil {
		writeError(w, http.StatusBadRequest, "image file is too large or invalid")
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "image file is required")
		return
	}
	defer file.Close()

	fileName := filepath.Base(header.Filename)
	if ext := strings.ToLower(filepath.Ext(fileName)); ext != "" && !allowedBookImageExt(ext) {
		writeError(w, http.StatusBadRequest, "unsupported image file type")
		return
	}
	head := make([]byte, 512)
	n, readErr := file.Read(head)
	if readErr != nil && readErr != io.EOF {
		writeError(w, http.StatusBadRequest, "image file is invalid")
		return
	}
	ext, ok := detectedBookImageExt(head[:n])
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported image file type")
		return
	}
	now := time.Now()
	dir := filepath.Join(s.cfg.UploadDir, "premium-courses", now.Format("2006"), now.Format("01"))
	if err := ensureUploadDir(dir); err != nil {
		writeError(w, http.StatusInternalServerError, "image directory error")
		return
	}
	path := filepath.Join(dir, uuid.NewString()+ext)
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "image save error")
		return
	}
	limited := io.LimitReader(io.MultiReader(bytes.NewReader(head[:n]), file), maxBytes+1)
	written, copyErr := io.Copy(out, limited)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		writeError(w, http.StatusInternalServerError, "image save error")
		return
	}
	if written > maxBytes {
		_ = os.Remove(path)
		writeError(w, http.StatusBadRequest, "image file is too large")
		return
	}
	publicPath := safePublicUploadPath(s.cfg.UploadDir, path)
	writeJSON(w, http.StatusOK, map[string]string{
		"cover_image_path":   publicPath,
		"cover_image_source": "uploaded",
	})
}

func (s *Server) handleAdminUserPremiumCourseAccess(w http.ResponseWriter, r *http.Request) {
	userID, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad user")
		return
	}
	items, err := s.store.ListUserPremiumCourseAccess(r.Context(), userID)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"premium_course_access": items})
}

func (s *Server) handleAdminGrantPremiumCourseAccess(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	userID, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad user")
		return
	}
	courseID, err := parsePathID(r, "course_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad course")
		return
	}
	var req struct {
		Active       *bool  `json:"active"`
		DurationType string `json:"duration_type"`
		ExpiresAt    string `json:"expires_at"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Active != nil && !*req.Active {
		if err := s.store.RevokePremiumCourseAccess(r.Context(), userID, courseID, actor.ID); mapRepoError(w, err) {
			return
		}
		_ = s.store.Audit(r.Context(), actor, "premium_course_access_revoked", "premium_course", courseID, map[string]any{"user_id": userID})
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	duration := strings.TrimSpace(req.DurationType)
	if duration == "" {
		duration = repository.PremiumAccessDurationLifetime
	}
	expiresAt, err := parseOptionalAdminTime(req.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad expires_at")
		return
	}
	if duration == repository.PremiumAccessDurationCustom && expiresAt == nil {
		writeError(w, http.StatusBadRequest, "custom date is required")
		return
	}
	access, err := s.store.GrantPremiumCourseAccess(r.Context(), userID, courseID, repository.PremiumAccessSourceManual, actor.ID, nil, duration, expiresAt)
	if mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "premium_course_access_granted", "premium_course", courseID, map[string]any{"user_id": userID, "duration_type": duration, "expires_at": req.ExpiresAt})
	writeJSON(w, http.StatusOK, map[string]any{"access": access})
}

func (s *Server) handleAdminRevokePremiumCourseAccess(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	userID, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad user")
		return
	}
	courseID, err := parsePathID(r, "course_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad course")
		return
	}
	if err := s.store.RevokePremiumCourseAccess(r.Context(), userID, courseID, actor.ID); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "premium_course_access_revoked", "premium_course", courseID, map[string]any{"user_id": userID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func parseOptionalAdminTime(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		value := parsed.UTC()
		return &value, nil
	}
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		value := parsed.UTC()
		return &value, nil
	}
	return nil, fmt.Errorf("bad time")
}

func isHTTPURL(value string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func allowedBookImageExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	default:
		return false
	}
}

func detectedBookImageExt(sample []byte) (string, bool) {
	if len(sample) >= 12 && string(sample[0:4]) == "RIFF" && string(sample[8:12]) == "WEBP" {
		return ".webp", true
	}
	switch http.DetectContentType(sample) {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/webp":
		return ".webp", true
	default:
		return "", false
	}
}

func isTelegramLink(value string) bool {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return true
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Path == "" || parsed.Path == "/" {
		return false
	}
	host := strings.ToLower(parsed.Host)
	return host == "t.me" || host == "telegram.me"
}

func (s *Server) handleAdminLevels(w http.ResponseWriter, r *http.Request) {
	levels, err := s.store.ListAdminLevels(r.Context())
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"levels": levels})
}

func (s *Server) handleAdminPostLevel(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	var req struct {
		ID             string              `json:"id"`
		Number         int                 `json:"number"`
		TitleKK        string              `json:"title_kk"`
		TitleRU        string              `json:"title_ru"`
		DescriptionKK  string              `json:"description_kk"`
		DescriptionRU  string              `json:"description_ru"`
		TelegramChatID string              `json:"telegram_chat_id"`
		SortOrder      int                 `json:"sort_order"`
		IsActive       *bool               `json:"is_active"`
		Access         bool                `json:"access"`
		Completed      bool                `json:"completed"`
		Progress       repository.Progress `json:"progress"`
		Lessons        []repository.Lesson `json:"lessons"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	level := repository.Level{
		ID:             req.ID,
		Number:         req.Number,
		TitleKK:        req.TitleKK,
		TitleRU:        req.TitleRU,
		DescriptionKK:  req.DescriptionKK,
		DescriptionRU:  req.DescriptionRU,
		TelegramChatID: req.TelegramChatID,
		SortOrder:      req.SortOrder,
		IsActive:       active,
	}
	if raw := r.PathValue("id"); raw != "" {
		if !repository.IsUUID(raw) {
			writeError(w, http.StatusBadRequest, "bad id")
			return
		}
		level.ID = raw
	}
	out, err := s.store.UpsertLevel(r.Context(), level)
	if mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "level_upsert", "level", out.ID, out)
	writeJSON(w, http.StatusOK, map[string]any{"level": out})
}

func (s *Server) handleAdminDeleteLevel(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.store.DeleteLevel(r.Context(), id); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "level_delete", "level", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminLessons(w http.ResponseWriter, r *http.Request) {
	level, _ := strconv.Atoi(r.URL.Query().Get("level"))
	lessons, err := s.store.ListAdminLessons(r.Context(), repository.AdminLessonFilter{
		Query:  r.URL.Query().Get("q"),
		Level:  level,
		Status: r.URL.Query().Get("status"),
	})
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lessons": lessons})
}

func (s *Server) handleAdminTests(w http.ResponseWriter, r *http.Request) {
	level, _ := strconv.Atoi(r.URL.Query().Get("level"))
	tests, err := s.store.ListAdminTests(r.Context(), repository.AdminTestFilter{
		Query:  r.URL.Query().Get("q"),
		Level:  level,
		Lesson: r.URL.Query().Get("lesson"),
		Status: r.URL.Query().Get("status"),
	})
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tests": tests})
}

func (s *Server) handleAdminPostLesson(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	var lesson repository.Lesson
	if err := decodeJSON(r, &lesson); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if raw := r.PathValue("id"); raw != "" {
		if !repository.IsUUID(raw) {
			writeError(w, http.StatusBadRequest, "bad id")
			return
		}
		lesson.ID = raw
	}
	out, err := s.store.UpsertLesson(r.Context(), lesson)
	if mapRepoError(w, err) {
		return
	}
	action := "lesson_create"
	if r.Method == http.MethodPatch {
		action = "lesson_update"
	}
	_ = s.store.Audit(r.Context(), actor, action, "lesson", out.ID, out)
	writeJSON(w, http.StatusOK, map[string]any{"lesson": out})
}

func (s *Server) handleAdminDeleteLesson(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.store.DeleteLesson(r.Context(), id); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "lesson_delete", "lesson", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminPostTest(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	var test repository.Test
	if err := decodeJSON(r, &test); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if raw := r.PathValue("id"); raw != "" {
		if !repository.IsUUID(raw) {
			writeError(w, http.StatusBadRequest, "bad id")
			return
		}
		test.ID = raw
	}
	out, err := s.store.UpsertTest(r.Context(), test)
	if mapRepoError(w, err) {
		return
	}
	action := "test_create"
	if r.Method == http.MethodPatch {
		action = "test_update"
	}
	_ = s.store.Audit(r.Context(), actor, action, "test", out.ID, out)
	writeJSON(w, http.StatusOK, map[string]any{"test": out})
}

func (s *Server) handleAdminDeleteTest(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.store.DeleteTest(r.Context(), id); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "test_delete", "test", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminReviewAssignmentSubmission(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.ReviewAssignmentSubmission(r.Context(), id, actor.ID, req.Status); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "assignment_submission_review", "assignment_submission", id, req)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminPlaceholder(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := s.store.PlaceholderList(r.Context(), name)
		if mapRepoError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func (s *Server) handleAdminAdjustCoins(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	var req struct {
		UserID string `json:"user_id"`
		Amount int    `json:"amount"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil || !repository.IsUUID(req.UserID) || req.Amount == 0 {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Reason == "" {
		req.Reason = "manual_adjust"
	}
	if err := s.store.ManualAdjustCoins(r.Context(), req.UserID, req.Amount, req.Reason, actor.ID); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "coin_adjust", "user", req.UserID, req)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := s.store.ListChannels(r.Context(), "", true)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": channels})
}

func (s *Server) handleAdminPostChannel(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	var channel repository.Channel
	if err := decodeJSON(r, &channel); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if raw := r.PathValue("id"); raw != "" {
		if !repository.IsUUID(raw) {
			writeError(w, http.StatusBadRequest, "bad id")
			return
		}
		channel.ID = raw
	}
	if strings.TrimSpace(channel.ManualInviteLink) != "" && !isTelegramLink(channel.ManualInviteLink) {
		writeError(w, http.StatusBadRequest, "invalid telegram link")
		return
	}
	if r.Method == http.MethodPost && !channel.IsActive {
		channel.IsActive = true
	}
	out, err := s.store.UpsertChannel(r.Context(), channel)
	if mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "channel_upsert", "channel", out.ID, out)
	writeJSON(w, http.StatusOK, map[string]any{"channel": out})
}

func (s *Server) handleAdminDeleteChannel(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.store.DeleteChannel(r.Context(), id); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "channel_delete", "channel", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminIssueInvite(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	channelID, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad channel")
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := decodeJSON(r, &req); err != nil || !repository.IsUUID(req.UserID) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	channel, err := s.store.GetChannel(r.Context(), channelID)
	if mapRepoError(w, err) {
		return
	}
	link := channel.ManualInviteLink
	expiresAt := time.Now().Add(24 * time.Hour)
	if channel.InviteLinkType == "bot" && s.bot != nil {
		if generated, err := s.bot.CreateInviteLink(r.Context(), channel.TelegramChatID, "admin-issued", expiresAt); err == nil {
			link = generated
		}
	}
	if link == "" {
		writeError(w, http.StatusConflict, "invite link is not configured")
		return
	}
	exp := expiresAt.Format(time.RFC3339)
	_ = s.store.RecordInviteLink(r.Context(), req.UserID, channel.ID, link, &exp)
	_ = s.store.Audit(r.Context(), actor, "channel_invite_issue", "channel", channelID, req)
	writeJSON(w, http.StatusOK, map[string]any{"invite_link": link, "expires_at": exp})
}

func (s *Server) handleAdminStreams(w http.ResponseWriter, r *http.Request) {
	streams, err := s.store.ListStreams(r.Context(), "", true)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"streams": streams})
}

func (s *Server) handleAdminPostStream(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	var stream repository.LiveStream
	if err := decodeJSON(r, &stream); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if raw := r.PathValue("id"); raw != "" {
		if !repository.IsUUID(raw) {
			writeError(w, http.StatusBadRequest, "bad id")
			return
		}
		stream.ID = raw
	}
	if stream.StartsAt.IsZero() {
		stream.StartsAt = time.Now().Add(7 * 24 * time.Hour)
	}
	out, err := s.store.UpsertStream(r.Context(), stream)
	if mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "stream_upsert", "stream", out.ID, out)
	writeJSON(w, http.StatusOK, map[string]any{"stream": out})
}

func (s *Server) handleAdminDeleteStream(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad stream")
		return
	}
	if err := s.store.DeleteStream(r.Context(), id); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "stream_delete", "stream", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminBroadcast(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	var req struct {
		Title  string `json:"title"`
		Body   string `json:"body"`
		Target string `json:"target"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Body == "" {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Target == "" {
		req.Target = "all"
	}
	id, err := s.store.Broadcast(r.Context(), actor, req.Title, req.Body, req.Target)
	if mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "broadcast_create", "broadcast", id, req)
	writeJSON(w, http.StatusOK, map[string]any{"broadcast_id": id})
}

func (s *Server) handleAdminBroadcasts(w http.ResponseWriter, r *http.Request) {
	broadcasts, err := s.store.ListBroadcasts(r.Context(), 100)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"broadcasts": broadcasts})
}

func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.Settings(r.Context())
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

func (s *Server) handleAdminPatchSettings(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	var req map[string]string
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if link, ok := req["channel_link"]; ok && !isTelegramLink(link) {
		writeError(w, http.StatusBadRequest, "invalid telegram link")
		return
	}
	if err := s.store.PatchSettings(r.Context(), req); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "settings_patch", "settings", "app", req)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListAudit(r.Context(), 100, 0)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": items})
}
