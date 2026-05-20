package handler

import (
	"bytes"
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
	_ = s.store.Audit(r.Context(), actor, "user_access", "user", id, req)
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
