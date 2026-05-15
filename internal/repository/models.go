package repository

import "time"

const (
	PaymentProviderKaspiQR  = "kaspi_qr"
	PaymentProviderKaspiPay = "kaspi_pay"
	PaymentProviderHalyk    = "halyk"
	PaymentProviderBankCard = "bank_card"

	PaymentStatusPending         = "pending"
	PaymentStatusUploadedReceipt = "uploaded_receipt"
	PaymentStatusApproved        = "approved"
	PaymentStatusRejected        = "rejected"
	PaymentStatusExpired         = "expired"
	PaymentStatusCancelled       = "cancelled"

	SubscriptionStatusActive    = "active"
	SubscriptionStatusExpired   = "expired"
	SubscriptionStatusCancelled = "cancelled"
	SubscriptionStatusPaused    = "paused"

	RoleSuperAdmin     = "super_admin"
	RoleContentManager = "content_manager"
	RoleSupport        = "support"
	RoleAnalyst        = "analyst"

	ReceiptStatusUploaded       = "uploaded"
	ReceiptStatusParseFailed    = "parse_failed"
	ReceiptStatusParsePartial   = "parse_partial"
	ReceiptStatusValidCandidate = "valid_candidate"
	ReceiptStatusSuspicious     = "suspicious"
	ReceiptStatusDuplicate      = "duplicate"
	ReceiptStatusRejected       = "rejected"
	ReceiptStatusApproved       = "approved"
)

type TelegramUserInput struct {
	TelegramID int64
	Username   string
	FirstName  string
	LastName   string
	PhotoURL   string
	Language   string
	StartParam string
}

type User struct {
	ID              string        `json:"id"`
	TelegramID      int64         `json:"telegram_id"`
	Username        string        `json:"username"`
	FirstName       string        `json:"first_name"`
	LastName        string        `json:"last_name"`
	PhotoURL        string        `json:"photo_url"`
	Language        string        `json:"language"`
	Phone           string        `json:"phone"`
	ReferralCode    string        `json:"referral_code"`
	InvitedByUserID *string       `json:"invited_by_user_id,omitempty"`
	CurrentLevel    int           `json:"current_level"`
	AccessClosed    bool          `json:"access_closed"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	LastSeenAt      time.Time     `json:"last_seen_at"`
	Subscription    *Subscription `json:"subscription,omitempty"`
	CoinBalance     int           `json:"coin_balance,omitempty"`
	ReferralCount   int           `json:"referral_count,omitempty"`
}

type Tariff struct {
	ID                 string   `json:"id"`
	Code               string   `json:"code"`
	Title              string   `json:"title"`
	PriceKZT           int      `json:"price_kzt"`
	ShortDescriptionKK string   `json:"short_description_kk"`
	FullDescriptionKK  string   `json:"full_description_kk"`
	Features           []string `json:"features"`
	FeaturesJSON       string   `json:"-"`
	ImageURL           string   `json:"image_url,omitempty"`
	ImageFilePath      string   `json:"image_file_path,omitempty"`
	ImageSource        string   `json:"image_source"`
	SortOrder          int      `json:"sort_order"`
	IsActive           bool     `json:"is_active"`
}

type Subscription struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id"`
	TariffID    string     `json:"tariff_id"`
	TariffCode  string     `json:"tariff_code"`
	TariffTitle string     `json:"tariff_title"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	CancelledAt *time.Time `json:"cancelled_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type Payment struct {
	ID                string     `json:"id"`
	UserID            string     `json:"user_id"`
	TariffID          string     `json:"tariff_id"`
	TariffCode        string     `json:"tariff_code"`
	SubscriptionID    *string    `json:"subscription_id,omitempty"`
	AmountKZT         int        `json:"amount_kzt"`
	Provider          string     `json:"provider"`
	Status            string     `json:"status"`
	ReceiptFilePath   string     `json:"receipt_file_path"`
	AdminComment      string     `json:"admin_comment"`
	ApprovedByAdminID *int64     `json:"approved_by_admin_id,omitempty"`
	ApprovedAt        *time.Time `json:"approved_at,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	User              *User      `json:"user,omitempty"`
	Receipt           *Receipt   `json:"receipt,omitempty"`
}

type Receipt struct {
	ID                   string     `json:"id"`
	PaymentID            string     `json:"payment_id"`
	UserID               string     `json:"user_id"`
	FilePath             string     `json:"file_path"`
	FileName             string     `json:"file_name"`
	MimeType             string     `json:"mime_type"`
	FileSize             int64      `json:"file_size"`
	Status               string     `json:"status"`
	FileHash             string     `json:"file_hash,omitempty"`
	RawTextHash          string     `json:"raw_text_hash,omitempty"`
	QRPayloadHash        string     `json:"qr_payload_hash,omitempty"`
	Provider             string     `json:"provider,omitempty"`
	ParsedAmountKZT      *int       `json:"parsed_amount_kzt,omitempty"`
	ParsedCurrency       string     `json:"parsed_currency,omitempty"`
	ParsedTransactionID  string     `json:"parsed_transaction_id,omitempty"`
	ParsedCheckID        string     `json:"parsed_check_id,omitempty"`
	ParsedReferenceID    string     `json:"parsed_reference_id,omitempty"`
	ParsedPaymentDate    *time.Time `json:"parsed_payment_date,omitempty"`
	ParsedRecipient      string     `json:"parsed_recipient,omitempty"`
	ParsedPayerMasked    string     `json:"parsed_payer_masked,omitempty"`
	ValidationStatus     string     `json:"validation_status"`
	ValidationErrors     []string   `json:"validation_errors,omitempty"`
	ValidationErrorsJSON string     `json:"-"`
	DuplicateOfReceiptID *string    `json:"duplicate_of_receipt_id,omitempty"`
	QRFound              bool       `json:"qr_found"`
	FileUnique           bool       `json:"file_unique"`
	QRUnique             bool       `json:"qr_unique"`
	CreatedAt            time.Time  `json:"created_at"`
}

type Level struct {
	ID                 string   `json:"id"`
	Number             int      `json:"number"`
	TitleKK            string   `json:"title_kk"`
	TitleRU            string   `json:"title_ru"`
	DescriptionKK      string   `json:"description_kk"`
	DescriptionRU      string   `json:"description_ru"`
	TelegramChatID     string   `json:"telegram_chat_id,omitempty"`
	TelegramConfigured bool     `json:"telegram_configured"`
	SortOrder          int      `json:"sort_order"`
	IsActive           bool     `json:"is_active"`
	Access             bool     `json:"access"`
	Completed          bool     `json:"completed"`
	Progress           Progress `json:"progress"`
	Lessons            []Lesson `json:"lessons,omitempty"`
}

type Lesson struct {
	ID            string     `json:"id"`
	LevelID       string     `json:"level_id"`
	LevelNumber   int        `json:"level_number"`
	TitleKK       string     `json:"title_kk"`
	TitleRU       string     `json:"title_ru"`
	DescriptionKK string     `json:"description_kk"`
	DescriptionRU string     `json:"description_ru"`
	VideoURL      string     `json:"video_url,omitempty"`
	SortOrder     int        `json:"sort_order"`
	IsActive      bool       `json:"is_active"`
	Watched       bool       `json:"watched"`
	WatchedAt     *time.Time `json:"watched_at,omitempty"`
	Access        bool       `json:"access"`
}

type Progress struct {
	LevelNumber     int    `json:"level_number"`
	TotalLessons    int    `json:"total_lessons"`
	WatchedLessons  int    `json:"watched_lessons"`
	TestPassed      bool   `json:"test_passed"`
	AssignmentDone  bool   `json:"assignment_done"`
	Completed       bool   `json:"completed"`
	Percent         int    `json:"percent"`
	NextRequirement string `json:"next_requirement"`
	CanUnlockNext   bool   `json:"can_unlock_next"`
	SubscriptionOK  bool   `json:"subscription_ok"`
}

type Test struct {
	ID            string         `json:"id"`
	LevelID       string         `json:"level_id"`
	LevelNumber   int            `json:"level_number"`
	LessonID      string         `json:"lesson_id,omitempty"`
	LessonTitleKK string         `json:"lesson_title_kk,omitempty"`
	Title         string         `json:"title"`
	PassPercent   int            `json:"pass_percent"`
	IsActive      bool           `json:"is_active"`
	Questions     []TestQuestion `json:"questions,omitempty"`
}

type TestQuestion struct {
	ID             string       `json:"id"`
	TestID         string       `json:"test_id"`
	QuestionTextKK string       `json:"question_text_kk"`
	QuestionTextRU string       `json:"question_text_ru"`
	SortOrder      int          `json:"sort_order"`
	Options        []TestOption `json:"options,omitempty"`
}

type TestOption struct {
	ID           string `json:"id"`
	QuestionID   string `json:"question_id"`
	OptionTextKK string `json:"option_text_kk"`
	OptionTextRU string `json:"option_text_ru"`
	SortOrder    int    `json:"sort_order"`
	IsCorrect    bool   `json:"is_correct,omitempty"`
}

type TestAttempt struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	TestID       string    `json:"test_id"`
	ScorePercent int       `json:"score_percent"`
	CorrectCount int       `json:"correct_count"`
	TotalCount   int       `json:"total_count"`
	Passed       bool      `json:"passed"`
	CreatedAt    time.Time `json:"created_at"`
}

type Assignment struct {
	ID            string `json:"id"`
	LevelID       string `json:"level_id"`
	LevelNumber   int    `json:"level_number"`
	TitleKK       string `json:"title_kk"`
	TitleRU       string `json:"title_ru"`
	DescriptionKK string `json:"description_kk"`
	DescriptionRU string `json:"description_ru"`
	IsActive      bool   `json:"is_active"`
}

type ReferralSummary struct {
	ReferralCode string           `json:"referral_code"`
	ReferralLink string           `json:"referral_link"`
	InvitedCount int              `json:"invited_count"`
	PaidCount    int              `json:"paid_count"`
	Rewards      []ReferralReward `json:"rewards"`
}

type ReferralReward struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	ThresholdCount int       `json:"threshold_count"`
	RewardType     string    `json:"reward_type"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

type Channel struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	TelegramChatID    string `json:"telegram_chat_id"`
	InviteLinkType    string `json:"invite_link_type"`
	ManualInviteLink  string `json:"manual_invite_link,omitempty"`
	TariffRequirement string `json:"tariff_requirement"`
	LevelRequirement  int    `json:"level_requirement"`
	IsActive          bool   `json:"is_active"`
	Access            bool   `json:"access"`
}

type UserLevelTelegramInvite struct {
	ID             string     `json:"id"`
	UserID         string     `json:"user_id"`
	TelegramUserID *int64     `json:"telegram_user_id,omitempty"`
	LevelID        string     `json:"level_id"`
	TelegramChatID string     `json:"telegram_chat_id"`
	InviteLink     string     `json:"invite_link"`
	InviteLinkID   string     `json:"invite_link_id,omitempty"`
	RawPayload     string     `json:"-"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type FinancialIQResult struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Score       int       `json:"score"`
	ResultTitle string    `json:"result_title"`
	ResultLevel string    `json:"result_level"`
	ResultText  string    `json:"result_text"`
	AnswersJSON string    `json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type LiveStream struct {
	ID                string    `json:"id"`
	Title             string    `json:"title"`
	Description       string    `json:"description"`
	StartsAt          time.Time `json:"starts_at"`
	StreamURL         string    `json:"stream_url"`
	TariffRequirement string    `json:"tariff_requirement"`
	Status            string    `json:"status"`
	RecordingURL      string    `json:"recording_url,omitempty"`
}

type AdminStats struct {
	UsersTotal           int `json:"users_total"`
	ActiveSubscriptions  int `json:"active_subscriptions"`
	ExpiredSubscriptions int `json:"expired_subscriptions"`
	PendingPayments      int `json:"pending_payments"`
	UploadedReceipts     int `json:"uploaded_receipts"`
	ApprovedPayments     int `json:"approved_payments"`
	MonthlyRevenueKZT    int `json:"monthly_revenue_kzt"`
	LessonsCount         int `json:"lessons_count"`
	TestsCount           int `json:"tests_count"`
	ReferralsPaid        int `json:"referrals_paid"`
	CoinsIssued          int `json:"coins_issued"`
}

type AdminActor struct {
	ID   int64  `json:"id"`
	Role string `json:"role"`
	Name string `json:"name"`
}

type Broadcast struct {
	ID          string     `json:"id"`
	AdminID     *int64     `json:"admin_id,omitempty"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Target      string     `json:"target"`
	Status      string     `json:"status"`
	SentCount   int        `json:"sent_count"`
	FailedCount int        `json:"failed_count"`
	CreatedAt   time.Time  `json:"created_at"`
	SentAt      *time.Time `json:"sent_at,omitempty"`
}

type BroadcastRecipient struct {
	UserID     string `json:"user_id"`
	TelegramID int64  `json:"telegram_id"`
	Language   string `json:"language"`
}
