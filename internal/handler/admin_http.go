package handler

import (
	"net/http"
	"strconv"
	"time"

	"zhenis-orda-service/internal/i18n"
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
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Closed bool `json:"closed"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.SetUserAccessClosed(r.Context(), id, req.Closed); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "user_access", "user", strconv.FormatInt(id, 10), req)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	_ = s.store.Audit(r.Context(), actor, "user_bonus", "user", strconv.FormatInt(id, 10), req)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminPayments(w http.ResponseWriter, r *http.Request) {
	payments, err := s.store.ListPayments(r.Context(), r.URL.Query().Get("status"), 100, 0)
	if mapRepoError(w, err) {
		return
	}
	for i := range payments {
		payments[i].ReceiptFilePath = safePublicUploadPath(s.cfg.UploadDir, payments[i].ReceiptFilePath)
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
	payment.ReceiptFilePath = safePublicUploadPath(s.cfg.UploadDir, payment.ReceiptFilePath)
	writeJSON(w, http.StatusOK, map[string]any{"payment": payment})
}

func (s *Server) handleAdminApprovePayment(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Days int `json:"days"`
	}
	_ = decodeJSON(r, &req)
	if req.Days == 0 {
		req.Days = s.cfg.SubscriptionDefaultDays
	}
	payment, err := s.store.ApprovePayment(r.Context(), id, actor.ID, req.Days)
	if mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "payment_approve", "payment", strconv.FormatInt(id, 10), req)
	if s.bot != nil {
		if user, err := s.store.GetUserByID(r.Context(), payment.UserID); err == nil {
			_ = s.bot.SendMessage(r.Context(), user.TelegramID, i18n.T(user.Language, "payment_approved"))
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
	_ = s.store.Audit(r.Context(), actor, "payment_reject", "payment", strconv.FormatInt(id, 10), req)
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
	_ = s.store.Audit(r.Context(), actor, "subscription_update", "subscription", strconv.FormatInt(id, 10), req)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminLevels(w http.ResponseWriter, r *http.Request) {
	levels, err := s.store.ListLevels(r.Context(), 0)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"levels": levels})
}

func (s *Server) handleAdminPostLevel(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	var level repository.Level
	if err := decodeJSON(r, &level); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if raw := r.PathValue("id"); raw != "" {
		level.ID, _ = strconv.ParseInt(raw, 10, 64)
	}
	if !level.IsActive {
		level.IsActive = true
	}
	out, err := s.store.UpsertLevel(r.Context(), level)
	if mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "level_upsert", "level", strconv.FormatInt(out.ID, 10), out)
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
	_ = s.store.Audit(r.Context(), actor, "level_delete", "level", strconv.FormatInt(id, 10), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminLessons(w http.ResponseWriter, r *http.Request) {
	lessons, err := s.store.ListLessons(r.Context(), 0, 0)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lessons": lessons})
}

func (s *Server) handleAdminPostLesson(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	var lesson repository.Lesson
	if err := decodeJSON(r, &lesson); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if raw := r.PathValue("id"); raw != "" {
		lesson.ID, _ = strconv.ParseInt(raw, 10, 64)
	}
	if !lesson.IsActive {
		lesson.IsActive = true
	}
	out, err := s.store.UpsertLesson(r.Context(), lesson)
	if mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "lesson_upsert", "lesson", strconv.FormatInt(out.ID, 10), out)
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
	_ = s.store.Audit(r.Context(), actor, "lesson_delete", "lesson", strconv.FormatInt(id, 10), nil)
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
		test.ID, _ = strconv.ParseInt(raw, 10, 64)
	}
	if !test.IsActive {
		test.IsActive = true
	}
	out, err := s.store.UpsertTest(r.Context(), test)
	if mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "test_upsert", "test", strconv.FormatInt(out.ID, 10), out)
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
	_ = s.store.Audit(r.Context(), actor, "test_delete", "test", strconv.FormatInt(id, 10), nil)
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
	_ = s.store.Audit(r.Context(), actor, "assignment_submission_review", "assignment_submission", strconv.FormatInt(id, 10), req)
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
		UserID int64  `json:"user_id"`
		Amount int    `json:"amount"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil || req.UserID == 0 || req.Amount == 0 {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Reason == "" {
		req.Reason = "manual_adjust"
	}
	if err := s.store.ManualAdjustCoins(r.Context(), req.UserID, req.Amount, req.Reason, actor.ID); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "coin_adjust", "user", strconv.FormatInt(req.UserID, 10), req)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := s.store.ListChannels(r.Context(), 0, true)
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
		channel.ID, _ = strconv.ParseInt(raw, 10, 64)
	}
	if !channel.IsActive {
		channel.IsActive = true
	}
	out, err := s.store.UpsertChannel(r.Context(), channel)
	if mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "channel_upsert", "channel", strconv.FormatInt(out.ID, 10), out)
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
	_ = s.store.Audit(r.Context(), actor, "channel_delete", "channel", strconv.FormatInt(id, 10), nil)
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
		UserID int64 `json:"user_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.UserID == 0 {
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
	_ = s.store.Audit(r.Context(), actor, "channel_invite_issue", "channel", strconv.FormatInt(channelID, 10), req)
	writeJSON(w, http.StatusOK, map[string]any{"invite_link": link, "expires_at": exp})
}

func (s *Server) handleAdminStreams(w http.ResponseWriter, r *http.Request) {
	streams, err := s.store.ListStreams(r.Context(), 0, true)
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
		stream.ID, _ = strconv.ParseInt(raw, 10, 64)
	}
	if stream.StartsAt.IsZero() {
		stream.StartsAt = time.Now().Add(7 * 24 * time.Hour)
	}
	out, err := s.store.UpsertStream(r.Context(), stream)
	if mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "stream_upsert", "stream", strconv.FormatInt(out.ID, 10), out)
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
	_ = s.store.Audit(r.Context(), actor, "stream_delete", "stream", strconv.FormatInt(id, 10), nil)
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
	_ = s.store.Audit(r.Context(), actor, "broadcast_create", "broadcast", strconv.FormatInt(id, 10), req)
	writeJSON(w, http.StatusOK, map[string]any{"broadcast_id": id})
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
