package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Token                       string
	Port                        string
	Env                         string
	BaseURL                     string
	MiniAppURL                  string
	DBPath                      string
	RedisAddr                   string
	RedisPassword               string
	RedisDB                     int
	AdminIDs                    []int64
	AdminPasswordHash           string
	UploadDir                   string
	BookUploadDir               string
	FreeLessonUploadDir         string
	PaymentDir                  string
	AllowedOrigins              []string
	WhatsAppSalesPhone          string
	KaspiPayURL                 string
	KaspiQRImageURL             string
	HalykPaymentURL             string
	BankCardPaymentURL          string
	PaymentPendingTTL           time.Duration
	PaymentRecipientBIN         string
	PaymentAmountToleranceKZT   int
	SubscriptionDefaultDays     int
	TelegramLogChatID           int64
	TelegramLogThreadID         int
	DisableTelegramBot          bool
	TelegramTestCommandsEnabled bool
	MaxReceiptBytes             int64
	MaxBookImageBytes           int64
	MaxFreeLessonImageBytes     int64
	BrowserSessionTTL           time.Duration
	TelegramInitDataMaxAge      time.Duration
	InactiveReminderCooldown    time.Duration
	SubscriptionReminderHours   int
}

var defaultAdminIDs = []int64{800703982, 513594097}

func Load() (Config, error) {
	uploadDir := getEnv("UPLOAD_DIR", "uploads")
	recipientBIN := strings.TrimSpace(os.Getenv("PAYMENT_RECIPIENT_BIN"))
	if recipientBIN == "" {
		recipientBIN = getEnv("PAYMENT_COMPANY_BIN", "830520499025")
	}
	cfg := Config{
		Token:                     "8146044709:AAGljvxX5uoj1TkYcAA05XKkhgmOffHadtY",
		Port:                      getEnv("PORT", "8088"),
		Env:                       getEnv("ENV", "development"),
		BaseURL:                   strings.TrimRight(getEnv("BASE_URL", "https://zhenis-orda.kz"), "/"),
		MiniAppURL:                strings.TrimRight(getEnv("MINI_APP_URL", "https://zhenis-orda.kz"), "/"),
		DBPath:                    getEnv("DB_PATH", "data/zhenis_orda.sqlite"),
		RedisAddr:                 getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:             "YOUR_PASSWORD_HERE_1999",
		AdminPasswordHash:         strings.TrimSpace(os.Getenv("ADMIN_PASSWORD_HASH")),
		AdminIDs:                  defaultAdminIDs,
		UploadDir:                 uploadDir,
		BookUploadDir:             getEnv("BOOK_UPLOAD_DIR", filepath.Join(uploadDir, "books")),
		FreeLessonUploadDir:       getEnv("FREE_LESSON_UPLOAD_DIR", filepath.Join(uploadDir, "free-lessons")),
		PaymentDir:                getEnv("PAYMENT_DIR", "payment"),
		AllowedOrigins:            splitCSV(getEnv("ALLOWED_ORIGINS", "https://zhenis-orda.kz")),
		WhatsAppSalesPhone:        digitsOnly(os.Getenv("WHATSAPP_SALES_PHONE")),
		KaspiPayURL:               "https://pay.kaspi.kz/pay/vdx8u2ff",
		KaspiQRImageURL:           os.Getenv("KASPI_QR_IMAGE_URL"),
		HalykPaymentURL:           os.Getenv("HALYK_PAYMENT_URL"),
		BankCardPaymentURL:        os.Getenv("BANK_CARD_PAYMENT_URL"),
		PaymentRecipientBIN:       digitsOnly(recipientBIN),
		PaymentAmountToleranceKZT: getEnvInt("PAYMENT_AMOUNT_TOLERANCE_KZT", 500),
		SubscriptionDefaultDays:   getEnvInt("SUBSCRIPTION_DEFAULT_DAYS", 30),
		DisableTelegramBot:        getEnvBool("DISABLE_TELEGRAM_BOT", false),
		MaxReceiptBytes:           int64(getEnvInt("MAX_RECEIPT_BYTES", 10*1024*1024)),
		MaxBookImageBytes:         int64(getEnvInt("MAX_BOOK_IMAGE_BYTES", 5*1024*1024)),
		MaxFreeLessonImageBytes:   int64(getEnvInt("MAX_FREE_LESSON_IMAGE_BYTES", 5*1024*1024)),
		BrowserSessionTTL:         time.Duration(getEnvInt("BROWSER_SESSION_TTL_HOURS", 24)) * time.Hour,
		TelegramInitDataMaxAge:    time.Duration(getEnvInt("TELEGRAM_INIT_DATA_MAX_AGE_HOURS", 24)) * time.Hour,
		InactiveReminderCooldown:  time.Duration(getEnvInt("INACTIVE_REMINDER_COOLDOWN_HOURS", 72)) * time.Hour,
		SubscriptionReminderHours: getEnvInt("SUBSCRIPTION_REMINDER_HOURS", 72),
	}

	cfg.RedisDB = getEnvInt("REDIS_DB", 0)
	cfg.TelegramTestCommandsEnabled = getEnvBool("ENABLE_TELEGRAM_TEST_COMMANDS", cfg.Env != "production")
	if rawAdminIDs := strings.TrimSpace(os.Getenv("ADMIN_IDS")); rawAdminIDs != "" {
		cfg.AdminIDs = parseInt64List(rawAdminIDs)
	}
	cfg.PaymentPendingTTL = time.Duration(getEnvInt("PAYMENT_PENDING_TTL_MINUTES", 60)) * time.Minute
	cfg.TelegramLogChatID = getEnvInt64("TELEGRAM_LOG_CHAT_ID", 0)
	cfg.TelegramLogThreadID = getEnvInt("TELEGRAM_LOG_THREAD_ID", 0)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var problems []string
	if c.Port == "" {
		problems = append(problems, "PORT is required")
	}
	if c.Env == "" {
		problems = append(problems, "ENV is required")
	}
	if c.BaseURL == "" {
		problems = append(problems, "BASE_URL is required")
	} else if _, err := url.ParseRequestURI(c.BaseURL); err != nil {
		problems = append(problems, "BASE_URL must be a valid URL")
	}
	if c.MiniAppURL == "" {
		problems = append(problems, "MINI_APP_URL is required")
	} else if _, err := url.ParseRequestURI(c.MiniAppURL); err != nil {
		problems = append(problems, "MINI_APP_URL must be a valid URL")
	}
	if c.DBPath == "" {
		problems = append(problems, "DB_PATH is required")
	}
	if c.PaymentDir == "" {
		problems = append(problems, "PAYMENT_DIR is required")
	}
	if c.BookUploadDir == "" {
		problems = append(problems, "BOOK_UPLOAD_DIR is required")
	}
	if c.FreeLessonUploadDir == "" {
		problems = append(problems, "FREE_LESSON_UPLOAD_DIR is required")
	}
	if c.SubscriptionDefaultDays <= 0 {
		problems = append(problems, "SUBSCRIPTION_DEFAULT_DAYS must be positive")
	}
	if c.PaymentPendingTTL <= 0 {
		problems = append(problems, "PAYMENT_PENDING_TTL_MINUTES must be positive")
	}
	if c.PaymentRecipientBIN == "" {
		problems = append(problems, "PAYMENT_RECIPIENT_BIN is required")
	}
	if c.PaymentAmountToleranceKZT < 0 {
		problems = append(problems, "PAYMENT_AMOUNT_TOLERANCE_KZT must be zero or positive")
	}
	if c.MaxReceiptBytes <= 0 {
		problems = append(problems, "MAX_RECEIPT_BYTES must be positive")
	}
	if c.MaxBookImageBytes <= 0 {
		problems = append(problems, "MAX_BOOK_IMAGE_BYTES must be positive")
	}
	if c.MaxFreeLessonImageBytes <= 0 {
		problems = append(problems, "MAX_FREE_LESSON_IMAGE_BYTES must be positive")
	}
	if c.Env == "production" {
		if c.Token == "" {
			problems = append(problems, "TOKEN is required in production")
		}
		if c.AdminPasswordHash == "" {
			problems = append(problems, "ADMIN_PASSWORD_HASH is required in production")
		}
		if len(c.AdminIDs) == 0 {
			problems = append(problems, "ADMIN_IDS is required in production")
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (c Config) IsProduction() bool {
	return c.Env == "production"
}

func (c Config) Addr() string {
	return ":" + strings.TrimPrefix(c.Port, ":")
}

func getEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getEnvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func getEnvInt64(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

func getEnvBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func parseInt64List(raw string) []int64 {
	values := splitCSV(raw)
	result := make([]int64, 0, len(values))
	for _, value := range values {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			result = append(result, parsed)
		}
	}
	return result
}

func digitsOnly(raw string) string {
	var builder strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func (c Config) PaymentURL(provider string) string {
	switch provider {
	case "kaspi_qr":
		return c.KaspiQRImageURL
	case "kaspi_pay":
		return c.KaspiPayURL
	case "halyk":
		return c.HalykPaymentURL
	case "bank_card":
		return c.BankCardPaymentURL
	default:
		return ""
	}
}

func (c Config) String() string {
	return fmt.Sprintf("env=%s port=%s base_url=%s mini_app_url=%s db=%s redis=%s bot_disabled=%t", c.Env, c.Port, c.BaseURL, c.MiniAppURL, c.DBPath, c.RedisAddr, c.DisableTelegramBot)
}
