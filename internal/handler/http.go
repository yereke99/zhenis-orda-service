package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"zhenis-orda-service/config"
	"zhenis-orda-service/internal/i18n"
	"zhenis-orda-service/internal/repository"
	"zhenis-orda-service/internal/service"

	"go.uber.org/zap"
)

type InviteIssuer interface {
	CreateInviteLink(ctx context.Context, chatID, name string, expiresAt time.Time) (string, error)
	SendMessage(ctx context.Context, chatID int64, text string) error
}

type ReceiptAdminNotifier interface {
	NotifyReceiptAdmins(ctx context.Context, user repository.User, payment repository.Payment, receipt repository.Receipt)
}

type Server struct {
	cfg       config.Config
	store     *repository.Store
	kv        KV
	sessions  SessionManager
	validator service.TelegramInitValidator
	logger    *zap.Logger
	bot       InviteIssuer
}

type ctxKey string

const (
	ctxUserKey    ctxKey = "user"
	ctxAdminKey   ctxKey = "admin"
	sessionCookie        = "zo_admin_session"
)

func NewServer(cfg config.Config, store *repository.Store, kv KV, logger *zap.Logger) *Server {
	return &Server{
		cfg:       cfg,
		store:     store,
		kv:        kv,
		sessions:  NewSessionManager(kv, cfg.BrowserSessionTTL),
		validator: service.NewTelegramInitValidator(cfg.Token, cfg.TelegramInitDataMaxAge),
		logger:    logger,
	}
}

func (s *Server) SetBot(bot InviteIssuer) {
	s.bot = bot
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	staticDir := http.Dir("static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(staticDir)))
	mux.Handle("GET /uploads/books/", http.StripPrefix("/uploads/books/", http.FileServer(http.Dir(s.cfg.BookUploadDir))))
	mux.Handle("GET /uploads/free-lessons/", http.StripPrefix("/uploads/free-lessons/", http.FileServer(http.Dir(s.cfg.FreeLessonUploadDir))))
	mux.Handle("GET /uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(s.cfg.UploadDir))))
	mux.HandleFunc("GET /", s.serveIndex)
	mux.HandleFunc("GET /admin", s.serveIndex)

	mux.HandleFunc("POST /api/browser-auth/login", s.handleBrowserLogin)
	mux.Handle("POST /api/browser-auth/logout", s.withBrowserAuth(s.handleBrowserLogout))
	mux.Handle("GET /api/browser-auth/me", s.withBrowserAuth(s.handleBrowserMe))

	mux.Handle("GET /api/me", s.withMiniAppAuth(http.HandlerFunc(s.handleMe)))
	mux.Handle("GET /api/platform", s.withMiniAppAuth(http.HandlerFunc(s.handlePlatform)))
	mux.Handle("POST /api/diagnostics", s.withMiniAppAuth(http.HandlerFunc(s.handleDiagnostics)))
	mux.Handle("GET /api/tariffs", s.withMiniAppAuth(http.HandlerFunc(s.handleTariffs)))
	mux.Handle("GET /api/legal/agreement-status", s.withMiniAppAuth(http.HandlerFunc(s.handleLegalAgreementStatus)))
	mux.Handle("GET /api/legal/document", s.withMiniAppAuth(http.HandlerFunc(s.handleLegalDocument)))
	mux.Handle("POST /api/legal/accept", s.withMiniAppAuth(http.HandlerFunc(s.handleAcceptLegalAgreement)))
	mux.Handle("POST /api/payments", s.withMiniAppAuth(http.HandlerFunc(s.handleCreatePayment)))
	mux.Handle("GET /api/payments/{id}", s.withMiniAppAuth(http.HandlerFunc(s.handlePayment)))
	mux.Handle("POST /api/payments/{id}/receipt", s.withMiniAppAuth(http.HandlerFunc(s.handlePaymentReceiptUpload)))
	mux.Handle("GET /api/subscription", s.withMiniAppAuth(http.HandlerFunc(s.handleSubscription)))
	mux.Handle("GET /api/levels", s.withMiniAppAuth(http.HandlerFunc(s.handleLevels)))
	mux.Handle("GET /api/levels/{id}", s.withMiniAppAuth(http.HandlerFunc(s.handleLevel)))
	mux.Handle("POST /api/levels/{id}/telegram-invite", s.withMiniAppAuth(http.HandlerFunc(s.handleLevelTelegramInvite)))
	mux.Handle("GET /api/lessons", s.withMiniAppAuth(http.HandlerFunc(s.handleLessons)))
	mux.Handle("GET /api/lessons/{id}", s.withMiniAppAuth(http.HandlerFunc(s.handleLesson)))
	mux.Handle("POST /api/lessons/{id}/watched", s.withMiniAppAuth(http.HandlerFunc(s.handleLessonWatched)))
	mux.Handle("POST /api/financial-iq", s.withMiniAppAuth(http.HandlerFunc(s.handleFinancialIQResult)))
	mux.Handle("GET /api/tests/{level_id}", s.withMiniAppAuth(http.HandlerFunc(s.handleTest)))
	mux.Handle("POST /api/tests/{level_id}/submit", s.withMiniAppAuth(http.HandlerFunc(s.handleSubmitTest)))
	mux.Handle("GET /api/assignments/{level_id}", s.withMiniAppAuth(http.HandlerFunc(s.handleAssignment)))
	mux.Handle("POST /api/assignments/{level_id}/submit", s.withMiniAppAuth(http.HandlerFunc(s.handleSubmitAssignment)))
	mux.Handle("GET /api/referral", s.withMiniAppAuth(http.HandlerFunc(s.handleReferral)))
	mux.Handle("GET /api/bonuses", s.withMiniAppAuth(http.HandlerFunc(s.handleBonuses)))
	mux.Handle("GET /api/coins", s.withMiniAppAuth(http.HandlerFunc(s.handleCoins)))
	mux.Handle("GET /api/streams", s.withMiniAppAuth(http.HandlerFunc(s.handleStreams)))
	mux.Handle("GET /api/channels", s.withMiniAppAuth(http.HandlerFunc(s.handleChannels)))
	mux.Handle("POST /api/channels/{id}/invite", s.withMiniAppAuth(http.HandlerFunc(s.handleIssueInvite)))
	mux.Handle("GET /api/premium-courses", s.withMiniAppAuth(http.HandlerFunc(s.handlePremiumCourses)))
	mux.Handle("GET /api/premium-courses/{id}", s.withMiniAppAuth(http.HandlerFunc(s.handlePremiumCourse)))
	mux.Handle("GET /api/premium-courses/{id}/lessons", s.withMiniAppAuth(http.HandlerFunc(s.handlePremiumCourseLessons)))
	mux.Handle("GET /api/premium-course-lessons/{id}", s.withMiniAppAuth(http.HandlerFunc(s.handlePremiumCourseLesson)))
	mux.Handle("POST /api/premium-courses/{id}/payments", s.withMiniAppAuth(http.HandlerFunc(s.handleCreatePremiumCoursePayment)))
	mux.Handle("POST /api/premium-courses/{id}/telegram-invite", s.withMiniAppAuth(http.HandlerFunc(s.handlePremiumCourseTelegramInvite)))
	mux.Handle("GET /api/books", s.withMiniAppAuth(http.HandlerFunc(s.handleBooks)))
	mux.Handle("GET /api/books/{id}", s.withMiniAppAuth(http.HandlerFunc(s.handleBook)))
	mux.Handle("GET /api/free-lessons", s.withMiniAppAuth(http.HandlerFunc(s.handleFreeLessons)))
	mux.Handle("GET /api/free-lessons/{id}", s.withMiniAppAuth(http.HandlerFunc(s.handleFreeLesson)))
	mux.Handle("POST /api/support", s.withMiniAppAuth(http.HandlerFunc(s.handleSupport)))

	admin := func(h http.HandlerFunc, roles ...string) http.Handler {
		return s.withBrowserAuth(s.withRole(h, roles...))
	}
	mux.Handle("GET /api/admin/stats", admin(s.handleAdminStats, repository.RoleSuperAdmin, repository.RoleAnalyst, repository.RoleSupport, repository.RoleContentManager))
	mux.Handle("GET /api/admin/users", admin(s.handleAdminUsers, repository.RoleSuperAdmin, repository.RoleSupport, repository.RoleAnalyst))
	mux.Handle("GET /api/admin/users/{id}", admin(s.handleAdminUser, repository.RoleSuperAdmin, repository.RoleSupport, repository.RoleAnalyst))
	mux.Handle("PATCH /api/admin/users/{id}/access", admin(s.handleAdminUserAccess, repository.RoleSuperAdmin, repository.RoleSupport))
	mux.Handle("POST /api/admin/users/{id}/bonus", admin(s.handleAdminUserBonus, repository.RoleSuperAdmin, repository.RoleSupport))
	mux.Handle("GET /api/admin/payments", admin(s.handleAdminPayments, repository.RoleSuperAdmin, repository.RoleSupport, repository.RoleAnalyst))
	mux.Handle("GET /api/admin/payments/{id}", admin(s.handleAdminPayment, repository.RoleSuperAdmin, repository.RoleSupport, repository.RoleAnalyst))
	mux.Handle("GET /api/admin/receipts/{id}/file", admin(s.handleAdminReceiptFile, repository.RoleSuperAdmin, repository.RoleSupport, repository.RoleAnalyst))
	mux.Handle("POST /api/admin/payments/{id}/approve", admin(s.handleAdminApprovePayment, repository.RoleSuperAdmin, repository.RoleSupport))
	mux.Handle("POST /api/admin/payments/{id}/reject", admin(s.handleAdminRejectPayment, repository.RoleSuperAdmin, repository.RoleSupport))
	mux.Handle("GET /api/admin/subscriptions", admin(s.handleAdminSubscriptions, repository.RoleSuperAdmin, repository.RoleSupport, repository.RoleAnalyst))
	mux.Handle("PATCH /api/admin/subscriptions/{id}", admin(s.handleAdminPatchSubscription, repository.RoleSuperAdmin, repository.RoleSupport))
	mux.Handle("GET /api/admin/tariffs", admin(s.handleAdminTariffs, repository.RoleSuperAdmin, repository.RoleContentManager, repository.RoleAnalyst))
	mux.Handle("POST /api/admin/tariffs", admin(s.handleAdminPostTariff, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("PATCH /api/admin/tariffs/{id}", admin(s.handleAdminPostTariff, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("DELETE /api/admin/tariffs/{id}", admin(s.handleAdminArchiveTariff, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("POST /api/admin/tariffs/image", admin(s.handleAdminTariffImageUpload, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("GET /api/admin/levels", admin(s.handleAdminLevels, repository.RoleSuperAdmin, repository.RoleContentManager, repository.RoleAnalyst))
	mux.Handle("POST /api/admin/levels", admin(s.handleAdminPostLevel, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("PATCH /api/admin/levels/{id}", admin(s.handleAdminPostLevel, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("DELETE /api/admin/levels/{id}", admin(s.handleAdminDeleteLevel, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("GET /api/admin/lessons", admin(s.handleAdminLessons, repository.RoleSuperAdmin, repository.RoleContentManager, repository.RoleAnalyst))
	mux.Handle("POST /api/admin/lessons", admin(s.handleAdminPostLesson, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("PATCH /api/admin/lessons/{id}", admin(s.handleAdminPostLesson, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("DELETE /api/admin/lessons/{id}", admin(s.handleAdminDeleteLesson, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("GET /api/admin/free-lessons", admin(s.handleAdminFreeLessons, repository.RoleSuperAdmin, repository.RoleContentManager, repository.RoleAnalyst))
	mux.Handle("GET /api/admin/free-lessons/{id}", admin(s.handleAdminFreeLesson, repository.RoleSuperAdmin, repository.RoleContentManager, repository.RoleAnalyst))
	mux.Handle("POST /api/admin/free-lessons", admin(s.handleAdminPostFreeLesson, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("PATCH /api/admin/free-lessons/{id}", admin(s.handleAdminPostFreeLesson, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("DELETE /api/admin/free-lessons/{id}", admin(s.handleAdminArchiveFreeLesson, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("POST /api/admin/free-lessons/upload-image", admin(s.handleAdminFreeLessonImageUpload, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("GET /api/admin/books", admin(s.handleAdminBooks, repository.RoleSuperAdmin, repository.RoleContentManager, repository.RoleAnalyst))
	mux.Handle("POST /api/admin/books", admin(s.handleAdminPostBook, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("PATCH /api/admin/books/{id}", admin(s.handleAdminPostBook, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("DELETE /api/admin/books/{id}", admin(s.handleAdminArchiveBook, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("POST /api/admin/books/upload-image", admin(s.handleAdminBookImageUpload, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("GET /api/admin/premium-courses", admin(s.handleAdminPremiumCourses, repository.RoleSuperAdmin, repository.RoleContentManager, repository.RoleSupport, repository.RoleAnalyst))
	mux.Handle("GET /api/admin/premium-courses/{id}", admin(s.handleAdminPremiumCourse, repository.RoleSuperAdmin, repository.RoleContentManager, repository.RoleSupport, repository.RoleAnalyst))
	mux.Handle("POST /api/admin/premium-courses", admin(s.handleAdminPostPremiumCourse, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("PATCH /api/admin/premium-courses/{id}", admin(s.handleAdminPostPremiumCourse, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("DELETE /api/admin/premium-courses/{id}", admin(s.handleAdminArchivePremiumCourse, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("POST /api/admin/premium-courses/upload-cover", admin(s.handleAdminPremiumCourseCoverUpload, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("GET /api/admin/users/{id}/premium-course-access", admin(s.handleAdminUserPremiumCourseAccess, repository.RoleSuperAdmin, repository.RoleSupport, repository.RoleAnalyst))
	mux.Handle("POST /api/admin/users/{id}/premium-course-access/{course_id}", admin(s.handleAdminGrantPremiumCourseAccess, repository.RoleSuperAdmin, repository.RoleSupport))
	mux.Handle("PATCH /api/admin/users/{id}/premium-course-access/{course_id}", admin(s.handleAdminGrantPremiumCourseAccess, repository.RoleSuperAdmin, repository.RoleSupport))
	mux.Handle("POST /api/admin/users/{id}/premium-course-access/{course_id}/revoke", admin(s.handleAdminRevokePremiumCourseAccess, repository.RoleSuperAdmin, repository.RoleSupport))
	mux.Handle("GET /api/admin/tests", admin(s.handleAdminTests, repository.RoleSuperAdmin, repository.RoleContentManager, repository.RoleAnalyst))
	mux.Handle("POST /api/admin/tests", admin(s.handleAdminPostTest, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("PATCH /api/admin/tests/{id}", admin(s.handleAdminPostTest, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("DELETE /api/admin/tests/{id}", admin(s.handleAdminDeleteTest, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("GET /api/admin/assignments", admin(s.handleAdminPlaceholder("assignments"), repository.RoleSuperAdmin, repository.RoleContentManager, repository.RoleAnalyst))
	mux.Handle("GET /api/admin/assignments/submissions", admin(s.handleAdminPlaceholder("assignment_submissions"), repository.RoleSuperAdmin, repository.RoleContentManager, repository.RoleSupport, repository.RoleAnalyst))
	mux.Handle("PATCH /api/admin/assignments/submissions/{id}", admin(s.handleAdminReviewAssignmentSubmission, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("GET /api/admin/referrals", admin(s.handleAdminPlaceholder("referrals"), repository.RoleSuperAdmin, repository.RoleSupport, repository.RoleAnalyst))
	mux.Handle("GET /api/admin/coins", admin(s.handleAdminPlaceholder("coins"), repository.RoleSuperAdmin, repository.RoleSupport, repository.RoleAnalyst))
	mux.Handle("POST /api/admin/coins/adjust", admin(s.handleAdminAdjustCoins, repository.RoleSuperAdmin, repository.RoleSupport))
	mux.Handle("GET /api/admin/channels", admin(s.handleAdminChannels, repository.RoleSuperAdmin, repository.RoleContentManager, repository.RoleSupport, repository.RoleAnalyst))
	mux.Handle("POST /api/admin/channels", admin(s.handleAdminPostChannel, repository.RoleSuperAdmin, repository.RoleContentManager, repository.RoleSupport))
	mux.Handle("PATCH /api/admin/channels/{id}", admin(s.handleAdminPostChannel, repository.RoleSuperAdmin, repository.RoleContentManager, repository.RoleSupport))
	mux.Handle("DELETE /api/admin/channels/{id}", admin(s.handleAdminDeleteChannel, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("POST /api/admin/channels/{id}/issue-invite", admin(s.handleAdminIssueInvite, repository.RoleSuperAdmin, repository.RoleSupport))
	mux.Handle("GET /api/admin/streams", admin(s.handleAdminStreams, repository.RoleSuperAdmin, repository.RoleContentManager, repository.RoleAnalyst))
	mux.Handle("POST /api/admin/streams", admin(s.handleAdminPostStream, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("PATCH /api/admin/streams/{id}", admin(s.handleAdminPostStream, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("DELETE /api/admin/streams/{id}", admin(s.handleAdminDeleteStream, repository.RoleSuperAdmin, repository.RoleContentManager))
	mux.Handle("POST /api/admin/broadcast", admin(s.handleAdminBroadcast, repository.RoleSuperAdmin, repository.RoleSupport))
	mux.Handle("GET /api/admin/broadcasts", admin(s.handleAdminBroadcasts, repository.RoleSuperAdmin, repository.RoleSupport, repository.RoleAnalyst))
	mux.Handle("GET /api/admin/settings", admin(s.handleAdminSettings, repository.RoleSuperAdmin, repository.RoleAnalyst))
	mux.Handle("PATCH /api/admin/settings", admin(s.handleAdminPatchSettings, repository.RoleSuperAdmin))
	mux.Handle("GET /api/admin/audit", admin(s.handleAdminAudit, repository.RoleSuperAdmin, repository.RoleAnalyst))

	return s.withCORS(s.recoverPanic(mux))
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/admin" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join("static", "index.html"))
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if s.originAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Telegram-Init-Data, X-Miniapp-Dev, X-Dev-Telegram-ID, X-Dev-Username, X-Dev-First-Name, X-Dev-Last-Name, X-Dev-Photo-URL")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	for _, allowed := range s.cfg.AllowedOrigins {
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
	}
	return false
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				s.logger.Error("http panic", zap.Any("panic", value), zap.String("path", r.URL.Path))
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withMiniAppAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.userFromMiniApp(r)
		if err != nil {
			s.logMiniAppAuthFailure(r, err)
			writeError(w, http.StatusUnauthorized, "telegram auth required")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) userFromMiniApp(r *http.Request) (repository.User, error) {
	if s.cfg.Env == "development" && (r.URL.Query().Get("miniapp_dev") == "1" || r.Header.Get("X-Miniapp-Dev") == "1") {
		telegramID := int64(777000)
		if raw := firstNonEmpty(r.URL.Query().Get("telegram_id"), r.Header.Get("X-Dev-Telegram-ID")); raw != "" {
			if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
				telegramID = parsed
			}
		}
		username := firstNonEmpty(r.URL.Query().Get("username"), r.Header.Get("X-Dev-Username"))
		if username == "" {
			username = "dev_preview"
		}
		firstName := firstNonEmpty(r.URL.Query().Get("first_name"), r.Header.Get("X-Dev-First-Name"), "Dev")
		lastName := firstNonEmpty(r.URL.Query().Get("last_name"), r.Header.Get("X-Dev-Last-Name"), "Preview")
		photoURL := firstNonEmpty(r.URL.Query().Get("photo_url"), r.Header.Get("X-Dev-Photo-URL"))
		user, _, err := s.store.RegisterOrUpdateTelegramUser(r.Context(), repository.TelegramUserInput{TelegramID: telegramID, Username: username, FirstName: firstName, LastName: lastName, PhotoURL: photoURL, Language: "kk"})
		return user, err
	}
	rawInitData := telegramInitDataFromRequest(r)
	s.logMiniAppAuthAttempt(r, rawInitData)
	initData, err := s.validator.Validate(rawInitData, time.Now())
	if err != nil {
		return repository.User{}, err
	}
	user, _, err := s.store.RegisterOrUpdateTelegramUser(r.Context(), repository.TelegramUserInput{
		TelegramID: initData.User.ID,
		Username:   initData.User.Username,
		FirstName:  initData.User.FirstName,
		LastName:   initData.User.LastName,
		PhotoURL:   initData.User.PhotoURL,
		Language:   "kk",
		StartParam: initData.StartParam,
	})
	if err == nil {
		s.logMiniAppAuthSuccess(initData.User.ID)
	}
	return user, err
}

func telegramInitDataFromRequest(r *http.Request) string {
	initData := strings.TrimSpace(r.Header.Get("X-Telegram-Init-Data"))
	if initData != "" {
		return initData
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) >= 4 && strings.EqualFold(auth[:4], "tma ") {
		return strings.TrimSpace(auth[4:])
	}
	return ""
}

func (s *Server) logMiniAppAuthAttempt(r *http.Request, initData string) {
	if s.cfg.Env != "development" || s.logger == nil {
		return
	}
	s.logger.Debug("mini app auth attempt",
		zap.String("path", r.URL.Path),
		zap.Bool("has_init_data", initData != ""),
		zap.Int("init_data_length", len(initData)))
}

func (s *Server) logMiniAppAuthFailure(r *http.Request, err error) {
	if s.cfg.Env != "development" || s.logger == nil {
		return
	}
	initData := telegramInitDataFromRequest(r)
	s.logger.Debug("mini app auth failed",
		zap.String("path", r.URL.Path),
		zap.Bool("has_init_data", initData != ""),
		zap.Int("init_data_length", len(initData)),
		zap.Error(err))
}

func (s *Server) logMiniAppAuthSuccess(telegramID int64) {
	if s.cfg.Env != "development" || s.logger == nil {
		return
	}
	s.logger.Debug("mini app auth validated", zap.Int64("telegram_id", telegramID))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *Server) withBrowserAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || cookie.Value == "" {
			writeError(w, http.StatusUnauthorized, "admin auth required")
			return
		}
		session, err := s.sessions.Get(r.Context(), cookie.Value)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "admin auth required")
			return
		}
		ctx := context.WithValue(r.Context(), ctxAdminKey, repository.AdminActor{ID: session.AdminID, Role: session.Role, Name: session.Name})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) withRole(next http.HandlerFunc, roles ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := adminFromContext(r.Context())
		if len(roles) == 0 || actor.Role == repository.RoleSuperAdmin {
			next(w, r)
			return
		}
		for _, role := range roles {
			if actor.Role == role {
				next(w, r)
				return
			}
		}
		writeError(w, http.StatusForbidden, "forbidden")
	}
}

func userFromContext(ctx context.Context) repository.User {
	user, _ := ctx.Value(ctxUserKey).(repository.User)
	return user
}

func adminFromContext(ctx context.Context) repository.AdminActor {
	actor, _ := ctx.Value(ctxAdminKey).(repository.AdminActor)
	return actor
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func decodeJSON(r *http.Request, dest any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(dest)
}

func parsePathID(r *http.Request, key string) (string, error) {
	value := strings.TrimSpace(r.PathValue(key))
	if !repository.IsUUID(value) {
		return "", fmt.Errorf("bad uuid")
	}
	return value, nil
}

func parsePathInt(r *http.Request, key string) (int, error) {
	value, err := strconv.Atoi(r.PathValue(key))
	return value, err
}

func mapRepoError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, repository.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, repository.ErrInvalidState):
		writeError(w, http.StatusConflict, "invalid state")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
	return true
}

func (s *Server) handleBrowserLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		Login    string `json:"login"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ok := false
	if s.cfg.AdminPasswordHash != "" {
		ok = bcrypt.CompareHashAndPassword([]byte(s.cfg.AdminPasswordHash), []byte(req.Password)) == nil
	} else if s.cfg.Env == "development" {
		ok = req.Password == "admin"
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}
	adminID := int64(1)
	if len(s.cfg.AdminIDs) > 0 {
		adminID = s.cfg.AdminIDs[0]
	}
	session := BrowserSession{AdminID: adminID, Role: repository.RoleSuperAdmin, Name: "Super Admin"}
	token, err := s.sessions.Create(r.Context(), session)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session error")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(s.cfg.BrowserSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.IsProduction(),
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"admin": session})
}

func (s *Server) handleBrowserLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		_ = s.sessions.Delete(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.IsProduction(), SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleBrowserMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"admin": adminFromContext(r.Context())})
}

func ensureUploadDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func safePublicUploadPath(uploadDir, filePath string) string {
	if filePath == "" {
		return ""
	}
	rel, err := filepath.Rel(uploadDir, filePath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filePath
	}
	return "/uploads/" + strings.ReplaceAll(rel, string(os.PathSeparator), "/")
}

func safePublicBookUploadPath(bookUploadDir, filePath string) string {
	if filePath == "" {
		return ""
	}
	rel, err := filepath.Rel(bookUploadDir, filePath)
	if err != nil || rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
		return ""
	}
	return "/uploads/books/" + strings.ReplaceAll(rel, string(os.PathSeparator), "/")
}

func safePublicFreeLessonUploadPath(uploadDir, filePath string) string {
	if filePath == "" {
		return ""
	}
	rel, err := filepath.Rel(uploadDir, filePath)
	if err != nil || rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
		return ""
	}
	return "/uploads/free-lessons/" + strings.ReplaceAll(rel, string(os.PathSeparator), "/")
}

func formatRejectMessage(language, comment string) string {
	if strings.TrimSpace(comment) == "" {
		comment = "чек анық емес"
	}
	return fmt.Sprintf(i18n.T(language, "payment_rejected"), comment)
}

func formatPaymentApprovedMessage(language string, payment repository.Payment) string {
	if payment.PaymentType == repository.PaymentTypePremiumCourse {
		return fmt.Sprintf("Төлеміңіз сәтті тексерілді ✅\n\n«%s» курсы ашылды.\nЕнді Mini App ішінен сабақтарды бастай аласыз.", paymentDisplayTitle(payment))
	}
	return fmt.Sprintf("Төлеміңіз сәтті тексерілді ✅\n\n«%s» тарифі ашылды.\nЕнді Mini App ішінен сабақтарды бастай аласыз.", paymentDisplayTitle(payment))
}

func paymentDisplayTitle(payment repository.Payment) string {
	if payment.PaymentType == repository.PaymentTypePremiumCourse {
		if title := strings.TrimSpace(payment.PremiumCourseTitle); title != "" {
			return title
		}
		if slug := strings.TrimSpace(payment.PremiumCourseSlug); slug != "" {
			return slug
		}
		return "Premium курс"
	}
	if title := strings.TrimSpace(payment.TariffTitle); title != "" {
		return title
	}
	if code := strings.TrimSpace(payment.TariffCode); code != "" {
		return code
	}
	return "Тариф"
}

func formatReceiptAdminMessage(user repository.User, payment repository.Payment, receipt repository.Receipt) string {
	fullName := strings.TrimSpace(user.FirstName + " " + user.LastName)
	fullName = cleanCaptionValue(fullName, "Көрсетілмеген")
	username := strings.TrimSpace(user.Username)
	if username == "" {
		username = "Жоқ"
	} else if !strings.HasPrefix(username, "@") {
		username = "@" + username
	}
	contactPhone := strings.TrimSpace(payment.ContactPhone)
	if contactPhone == "" {
		contactPhone = strings.TrimSpace(user.Phone)
	}
	contactPhone = cleanCaptionValue(contactPhone, "Көрсетілмеген")
	telegramID := "Жоқ"
	if user.TelegramID != 0 {
		telegramID = strconv.FormatInt(user.TelegramID, 10)
	}
	title := "🧾 Жаңа төлем чегі келді"
	if paymentAutoRejectedReceipt(payment) {
		title = "🧾 Төлем чегі автоматты түрде қабылданбады"
	}
	return fmt.Sprintf("%s\n\n👤 Қолданушы: %s\n🔗 Telegram: %s\n🆔 Telegram ID: %s\n📞 Байланыс нөмірі: %s\n\n🎓 Тариф/курс: %s\n💰 Күтілген сома: %s\n📏 Рұқсат етілген ауытқу: %s\n📌 Статус: %s\n🧾 Төлем ID: %s\n\n📄 Чек ақпараты:\n• Чектегі сома: %s\n• Валюта: %s\n• Сатушы БИН/ИИН: %s\n• Күтілетін БИН/ИИН: %s\n• Чек нөмірі: %s\n• Бірегейлік: %s\n• Тексеру нәтижесі: %s\n• Себеп: %s\n\n🕒 Төлем құрылған уақыт: %s\n🕒 Чек жіберілген уақыт: %s\n\n%s",
		title,
		fullName,
		username,
		telegramID,
		contactPhone,
		cleanCaptionValue(paymentDisplayTitle(payment), "Көрсетілмеген"),
		optionalKZTAmount(payment.AmountKZT),
		receiptToleranceText(receipt),
		adminPaymentStatusText(payment, receipt),
		cleanCaptionValue(payment.ID, "Жоқ"),
		receiptAmountText(receipt),
		receiptCurrencyText(receipt),
		cleanCaptionValue(receipt.ParsedRecipientBIN, "Анықталмады"),
		cleanCaptionValue(receipt.ExpectedRecipientBIN, "Анықталмады"),
		cleanCaptionValue(firstNonEmpty(receipt.ParsedCheckID, receipt.ReceiptTransactionKey, receipt.ParsedTransactionID), "Анықталмады"),
		receiptUniquenessText(receipt),
		receiptValidationResultText(receipt),
		receiptValidationReasonText(receipt),
		captionTime(payment.CreatedAt),
		captionTime(receipt.CreatedAt),
		receiptAdminInstruction(payment, receipt),
	)
}

func receiptAmountText(receipt repository.Receipt) string {
	if receipt.ParsedAmountKZT == nil {
		return "Анықталмады"
	}
	return formatKZTAmount(*receipt.ParsedAmountKZT) + " ₸"
}

func receiptToleranceText(receipt repository.Receipt) string {
	if receipt.AmountToleranceKZT < 0 {
		return "Анықталмады"
	}
	return formatKZTAmount(receipt.AmountToleranceKZT) + " ₸"
}

func receiptCurrencyText(receipt repository.Receipt) string {
	return cleanCaptionValue(receipt.ParsedCurrency, "Анықталмады")
}

func optionalKZTAmount(amount int) string {
	if amount <= 0 {
		return "Анықталмады"
	}
	return formatKZTAmount(amount) + " ₸"
}

func formatKZTAmount(amount int) string {
	sign := ""
	if amount < 0 {
		sign = "-"
		amount = -amount
	}
	value := strconv.Itoa(amount)
	var parts []string
	for len(value) > 3 {
		parts = append([]string{value[len(value)-3:]}, parts...)
		value = value[:len(value)-3]
	}
	parts = append([]string{value}, parts...)
	return sign + strings.Join(parts, " ")
}

func cleanCaptionValue(value, fallback string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "null", "undefined", "<nil>":
		return fallback
	default:
		return value
	}
}

func captionTime(value time.Time) string {
	if value.IsZero() {
		return "Анықталмады"
	}
	return value.Local().Format("2006-01-02 15:04")
}

func adminPaymentStatusText(payment repository.Payment, receipt repository.Receipt) string {
	if paymentAutoRejectedReceipt(payment) {
		return "автоматты түрде қабылданбады"
	}
	if receiptNeedsManualReview(payment, receipt) {
		return "қолмен тексеру қажет"
	}
	if payment.Status == repository.PaymentStatusApproved {
		if payment.ApprovedByAdminID == nil {
			return "автоматты түрде расталды"
		}
		return "расталды"
	}
	switch payment.Status {
	case repository.PaymentStatusPending:
		return "күтілуде"
	case repository.PaymentStatusRejected:
		return "қабылданбады"
	case repository.PaymentStatusExpired:
		return "уақыты аяқталды"
	case repository.PaymentStatusCancelled:
		return "тоқтатылды"
	default:
		return cleanCaptionValue(payment.Status, "Анықталмады")
	}
}

func receiptNeedsManualReview(payment repository.Payment, receipt repository.Receipt) bool {
	if payment.Status == repository.PaymentStatusRejected {
		return false
	}
	if payment.Status == repository.PaymentStatusUploadedReceipt {
		return true
	}
	switch receipt.ValidationStatus {
	case repository.ReceiptStatusDuplicate, repository.ReceiptStatusSuspicious, repository.ReceiptStatusParseFailed, repository.ReceiptStatusParsePartial, repository.ReceiptStatusRejected:
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

func paymentAutoRejectedReceipt(payment repository.Payment) bool {
	return payment.Status == repository.PaymentStatusRejected && strings.HasPrefix(strings.TrimSpace(payment.AdminComment), "auto_rejected:")
}

func receiptUniquenessText(receipt repository.Receipt) string {
	if receipt.DuplicateOfReceiptID != nil || receipt.ValidationStatus == repository.ReceiptStatusDuplicate || receiptHasCode(receipt.ValidationErrors, "duplicate_identity_found") {
		return "қайталанған чек"
	}
	if receipt.FileHash == "" && receipt.RawTextHash == "" && receipt.QRPayloadHash == "" && receipt.ReceiptTransactionKey == "" {
		return "Анықталмады"
	}
	return "бірегей"
}

func receiptValidationResultText(receipt repository.Receipt) string {
	switch receipt.ValidationStatus {
	case repository.ReceiptStatusApproved:
		return "расталды"
	case repository.ReceiptStatusValidCandidate:
		return "тексеруден өтті"
	case repository.ReceiptStatusDuplicate:
		return "қайталанған чек"
	case repository.ReceiptStatusSuspicious:
		return "қолмен тексеру қажет"
	case repository.ReceiptStatusParseFailed:
		return "оқу мүмкін болмады"
	case repository.ReceiptStatusParsePartial:
		return "толық анықталмады"
	case repository.ReceiptStatusRejected:
		return "қабылданбады"
	case repository.ReceiptStatusUploaded:
		return "жүктелді"
	default:
		return cleanCaptionValue(receipt.ValidationStatus, "Анықталмады")
	}
}

func receiptValidationReasonText(receipt repository.Receipt) string {
	if len(receipt.ValidationErrors) == 0 {
		switch receipt.ValidationStatus {
		case repository.ReceiptStatusApproved, repository.ReceiptStatusValidCandidate:
			return "Қате табылмады"
		default:
			return "Көрсетілмеген"
		}
	}
	reasons := make([]string, 0, len(receipt.ValidationErrors))
	for _, code := range receipt.ValidationErrors {
		reasons = append(reasons, receiptValidationReason(code))
	}
	return strings.Join(reasons, "; ")
}

func receiptValidationReason(code string) string {
	switch code {
	case "amount_not_found":
		return "сома анықталмады"
	case "amount_mismatch":
		return "сома сәйкес емес"
	case "currency_missing":
		return "валюта анықталмады"
	case "currency_mismatch":
		return "валюта KZT емес"
	case "recipient_bin_not_configured":
		return "күтілетін БИН/ИИН бапталмаған"
	case "recipient_bin_missing":
		return "БИН/ИИН анықталмады"
	case "recipient_bin_mismatch":
		return "БИН/ИИН сәйкес емес"
	case "provider_marker_missing":
		return "төлем провайдері анықталмады"
	case "payment_date_too_early":
		return "чек уақыты төлемнен бұрын"
	case "strong_identity_not_found":
		return "чектің бірегей нөмірі анықталмады"
	case "duplicate_identity_found":
		return "чек бұрын қолданылған"
	case "pdf_text_unreadable":
		return "PDF мәтіні оқылмады"
	case "file_read_failed":
		return "файл оқылмады"
	default:
		return cleanCaptionValue(code, "Көрсетілмеген")
	}
}

func receiptHasCode(values []string, code string) bool {
	for _, value := range values {
		if value == code {
			return true
		}
	}
	return false
}

func duplicateReceiptAttempt(receipt repository.Receipt) repository.Receipt {
	receipt.ValidationStatus = repository.ReceiptStatusDuplicate
	if !receiptHasCode(receipt.ValidationErrors, "duplicate_identity_found") {
		receipt.ValidationErrors = append(receipt.ValidationErrors, "duplicate_identity_found")
	}
	receipt.CreatedAt = time.Now()
	return receipt
}

func receiptAdminInstruction(payment repository.Payment, receipt repository.Receipt) string {
	if paymentAutoRejectedReceipt(payment) {
		return "Қолжетімділік берілмеді. Әкімші панелінен себебін тексеруге болады."
	}
	if payment.Status == repository.PaymentStatusRejected {
		return "Төлем қабылданбады. Қолжетімділік берілмеді."
	}
	if receiptNeedsManualReview(payment, receipt) {
		return "Әкімші панелінен тексеріп, төлемді растаңыз немесе қабылдамаңыз."
	}
	if payment.Status == repository.PaymentStatusApproved && payment.ApprovedByAdminID == nil {
		return "Төлем автоматты түрде расталды. Қажет болса әкімші панелінен қайта тексеріңіз."
	}
	return "Әкімші панелінен төлем мәліметтерін тексеріңіз."
}
