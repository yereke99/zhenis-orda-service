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
)

type TelegramUserInput struct {
	TelegramID int64
	Username   string
	FirstName  string
	LastName   string
	Language   string
	StartParam string
}

type User struct {
	ID              int64         `json:"id"`
	TelegramID      int64         `json:"telegram_id"`
	Username        string        `json:"username"`
	FirstName       string        `json:"first_name"`
	LastName        string        `json:"last_name"`
	Language        string        `json:"language"`
	Phone           string        `json:"phone"`
	ReferralCode    string        `json:"referral_code"`
	InvitedByUserID *int64        `json:"invited_by_user_id,omitempty"`
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
	ID           int64    `json:"id"`
	Code         string   `json:"code"`
	Title        string   `json:"title"`
	PriceKZT     int      `json:"price_kzt"`
	Features     []string `json:"features"`
	FeaturesJSON string   `json:"-"`
	SortOrder    int      `json:"sort_order"`
	IsActive     bool     `json:"is_active"`
}

type Subscription struct {
	ID          int64      `json:"id"`
	UserID      int64      `json:"user_id"`
	TariffID    int64      `json:"tariff_id"`
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
	ID                int64      `json:"id"`
	UserID            int64      `json:"user_id"`
	TariffID          int64      `json:"tariff_id"`
	TariffCode        string     `json:"tariff_code"`
	SubscriptionID    *int64     `json:"subscription_id,omitempty"`
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
}

type Receipt struct {
	ID        int64     `json:"id"`
	PaymentID int64     `json:"payment_id"`
	UserID    int64     `json:"user_id"`
	FilePath  string    `json:"file_path"`
	FileName  string    `json:"file_name"`
	MimeType  string    `json:"mime_type"`
	FileSize  int64     `json:"file_size"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type Level struct {
	ID            int64    `json:"id"`
	Number        int      `json:"number"`
	TitleKK       string   `json:"title_kk"`
	TitleRU       string   `json:"title_ru"`
	DescriptionKK string   `json:"description_kk"`
	DescriptionRU string   `json:"description_ru"`
	SortOrder     int      `json:"sort_order"`
	IsActive      bool     `json:"is_active"`
	Access        bool     `json:"access"`
	Completed     bool     `json:"completed"`
	Progress      Progress `json:"progress"`
	Lessons       []Lesson `json:"lessons,omitempty"`
}

type Lesson struct {
	ID            int64      `json:"id"`
	LevelID       int64      `json:"level_id"`
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
	ID          int64          `json:"id"`
	LevelID     int64          `json:"level_id"`
	LevelNumber int            `json:"level_number"`
	Title       string         `json:"title"`
	PassPercent int            `json:"pass_percent"`
	IsActive    bool           `json:"is_active"`
	Questions   []TestQuestion `json:"questions,omitempty"`
}

type TestQuestion struct {
	ID             int64        `json:"id"`
	TestID         int64        `json:"test_id"`
	QuestionTextKK string       `json:"question_text_kk"`
	QuestionTextRU string       `json:"question_text_ru"`
	SortOrder      int          `json:"sort_order"`
	Options        []TestOption `json:"options,omitempty"`
}

type TestOption struct {
	ID           int64  `json:"id"`
	QuestionID   int64  `json:"question_id"`
	OptionTextKK string `json:"option_text_kk"`
	OptionTextRU string `json:"option_text_ru"`
	SortOrder    int    `json:"sort_order"`
	IsCorrect    bool   `json:"is_correct,omitempty"`
}

type TestAttempt struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	TestID       int64     `json:"test_id"`
	ScorePercent int       `json:"score_percent"`
	CorrectCount int       `json:"correct_count"`
	TotalCount   int       `json:"total_count"`
	Passed       bool      `json:"passed"`
	CreatedAt    time.Time `json:"created_at"`
}

type Assignment struct {
	ID            int64  `json:"id"`
	LevelID       int64  `json:"level_id"`
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
	ID             int64     `json:"id"`
	UserID         int64     `json:"user_id"`
	ThresholdCount int       `json:"threshold_count"`
	RewardType     string    `json:"reward_type"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

type Channel struct {
	ID                int64  `json:"id"`
	Title             string `json:"title"`
	TelegramChatID    string `json:"telegram_chat_id"`
	InviteLinkType    string `json:"invite_link_type"`
	ManualInviteLink  string `json:"manual_invite_link,omitempty"`
	TariffRequirement string `json:"tariff_requirement"`
	LevelRequirement  int    `json:"level_requirement"`
	IsActive          bool   `json:"is_active"`
	Access            bool   `json:"access"`
}

type LiveStream struct {
	ID                int64     `json:"id"`
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
	ReferralsPaid        int `json:"referrals_paid"`
	CoinsIssued          int `json:"coins_issued"`
}

type AdminActor struct {
	ID   int64  `json:"id"`
	Role string `json:"role"`
	Name string `json:"name"`
}
