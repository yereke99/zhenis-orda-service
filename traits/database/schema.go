package database

const schemaV1 = `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	telegram_id INTEGER NOT NULL UNIQUE,
	username TEXT,
	first_name TEXT,
	last_name TEXT,
	language TEXT NOT NULL DEFAULT '',
	phone TEXT,
	referral_code TEXT NOT NULL UNIQUE,
	invited_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	current_level INTEGER NOT NULL DEFAULT 0,
	access_closed INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS admins (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	telegram_id INTEGER NOT NULL UNIQUE,
	role TEXT NOT NULL DEFAULT 'super_admin',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS staff_users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	login TEXT NOT NULL UNIQUE,
	password_hash TEXT,
	role TEXT NOT NULL CHECK(role IN ('super_admin','content_manager','support','analyst')),
	is_active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS diagnostics (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name TEXT,
	city TEXT,
	age INTEGER,
	income TEXT,
	has_debt TEXT,
	has_business TEXT,
	main_problem TEXT,
	growth_area TEXT,
	answers_json TEXT NOT NULL DEFAULT '{}',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tariffs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	code TEXT NOT NULL UNIQUE,
	title TEXT NOT NULL,
	price_kzt INTEGER NOT NULL,
	features_json TEXT NOT NULL DEFAULT '[]',
	sort_order INTEGER NOT NULL DEFAULT 0,
	is_active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS subscriptions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	tariff_id INTEGER NOT NULL REFERENCES tariffs(id),
	status TEXT NOT NULL CHECK(status IN ('active','expired','cancelled','paused')),
	started_at DATETIME NOT NULL,
	expires_at DATETIME NOT NULL,
	cancelled_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS payments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	tariff_id INTEGER NOT NULL REFERENCES tariffs(id),
	subscription_id INTEGER REFERENCES subscriptions(id) ON DELETE SET NULL,
	amount_kzt INTEGER NOT NULL,
	provider TEXT NOT NULL CHECK(provider IN ('kaspi_qr','kaspi_pay','halyk','bank_card')),
	status TEXT NOT NULL CHECK(status IN ('pending','uploaded_receipt','approved','rejected','expired','cancelled')),
	receipt_file_path TEXT,
	admin_comment TEXT,
	approved_by_admin_id INTEGER,
	approved_at DATETIME,
	expires_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS payment_receipts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	payment_id INTEGER NOT NULL REFERENCES payments(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	file_path TEXT NOT NULL,
	file_name TEXT,
	mime_type TEXT,
	file_size INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'uploaded',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS levels (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	number INTEGER NOT NULL UNIQUE,
	title_kk TEXT NOT NULL,
	title_ru TEXT NOT NULL,
	description_kk TEXT,
	description_ru TEXT,
	sort_order INTEGER NOT NULL DEFAULT 0,
	is_active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS lessons (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	level_id INTEGER NOT NULL REFERENCES levels(id) ON DELETE CASCADE,
	title_kk TEXT NOT NULL,
	title_ru TEXT NOT NULL,
	description_kk TEXT,
	description_ru TEXT,
	video_url TEXT,
	sort_order INTEGER NOT NULL DEFAULT 0,
	is_active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(level_id, sort_order)
);

CREATE TABLE IF NOT EXISTS lesson_progress (
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	lesson_id INTEGER NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
	watched INTEGER NOT NULL DEFAULT 0,
	watched_at DATETIME,
	coin_granted INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY(user_id, lesson_id)
);

CREATE TABLE IF NOT EXISTS tests (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	level_id INTEGER NOT NULL REFERENCES levels(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	pass_percent INTEGER NOT NULL DEFAULT 70,
	is_active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(level_id)
);

CREATE TABLE IF NOT EXISTS test_questions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	test_id INTEGER NOT NULL REFERENCES tests(id) ON DELETE CASCADE,
	question_text_kk TEXT NOT NULL,
	question_text_ru TEXT NOT NULL,
	sort_order INTEGER NOT NULL DEFAULT 0,
	is_active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(test_id, sort_order)
);

CREATE TABLE IF NOT EXISTS test_options (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	question_id INTEGER NOT NULL REFERENCES test_questions(id) ON DELETE CASCADE,
	option_text_kk TEXT NOT NULL,
	option_text_ru TEXT NOT NULL,
	is_correct INTEGER NOT NULL DEFAULT 0,
	sort_order INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(question_id, sort_order)
);

CREATE TABLE IF NOT EXISTS test_attempts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	test_id INTEGER NOT NULL REFERENCES tests(id) ON DELETE CASCADE,
	score_percent INTEGER NOT NULL,
	correct_count INTEGER NOT NULL,
	total_count INTEGER NOT NULL,
	passed INTEGER NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS test_answers (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	attempt_id INTEGER NOT NULL REFERENCES test_attempts(id) ON DELETE CASCADE,
	question_id INTEGER NOT NULL REFERENCES test_questions(id) ON DELETE CASCADE,
	selected_option_id INTEGER REFERENCES test_options(id) ON DELETE SET NULL,
	is_correct INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS assignments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	level_id INTEGER NOT NULL REFERENCES levels(id) ON DELETE CASCADE,
	title_kk TEXT NOT NULL,
	title_ru TEXT NOT NULL,
	description_kk TEXT,
	description_ru TEXT,
	is_active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(level_id)
);

CREATE TABLE IF NOT EXISTS assignment_submissions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	assignment_id INTEGER NOT NULL REFERENCES assignments(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	answer_text TEXT,
	file_path TEXT,
	link_url TEXT,
	status TEXT NOT NULL DEFAULT 'submitted' CHECK(status IN ('submitted','reviewed','rejected')),
	reviewed_by_admin_id INTEGER,
	reviewed_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS referrals (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	inviter_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	invited_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	status TEXT NOT NULL DEFAULT 'registered' CHECK(status IN ('registered','paid','rewarded','cancelled')),
	reward_granted INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(invited_user_id)
);

CREATE TABLE IF NOT EXISTS referral_rewards (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	threshold_count INTEGER NOT NULL,
	reward_type TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'granted' CHECK(status IN ('pending','granted','cancelled')),
	source_referral_count INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(user_id, threshold_count)
);

CREATE TABLE IF NOT EXISTS coin_transactions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	amount INTEGER NOT NULL,
	reason TEXT NOT NULL,
	source_type TEXT NOT NULL,
	source_id TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(user_id, reason, source_type, source_id)
);

CREATE TABLE IF NOT EXISTS channels (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL,
	telegram_chat_id TEXT NOT NULL,
	invite_link_type TEXT NOT NULL DEFAULT 'bot' CHECK(invite_link_type IN ('bot','manual')),
	manual_invite_link TEXT,
	tariff_requirement TEXT NOT NULL DEFAULT 'BASIC' CHECK(tariff_requirement IN ('BASIC','STANDARD','VIP')),
	level_requirement INTEGER NOT NULL DEFAULT 1,
	is_active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS channel_invite_links (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	invite_link TEXT NOT NULL,
	expires_at DATETIME,
	status TEXT NOT NULL DEFAULT 'issued' CHECK(status IN ('issued','used','expired','revoked')),
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS live_streams (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL,
	description TEXT,
	starts_at DATETIME NOT NULL,
	stream_url TEXT,
	tariff_requirement TEXT NOT NULL DEFAULT 'STANDARD',
	status TEXT NOT NULL DEFAULT 'scheduled' CHECK(status IN ('scheduled','live','finished','cancelled')),
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS live_stream_reminders (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	stream_id INTEGER NOT NULL REFERENCES live_streams(id) ON DELETE CASCADE,
	reminder_key TEXT NOT NULL,
	sent_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(stream_id, reminder_key)
);

CREATE TABLE IF NOT EXISTS live_stream_recordings (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	stream_id INTEGER NOT NULL REFERENCES live_streams(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	recording_url TEXT NOT NULL,
	tariff_requirement TEXT NOT NULL DEFAULT 'STANDARD',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_stream_attendance (
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	stream_id INTEGER NOT NULL REFERENCES live_streams(id) ON DELETE CASCADE,
	attended_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	coin_granted INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY(user_id, stream_id)
);

CREATE TABLE IF NOT EXISTS broadcasts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	admin_id INTEGER,
	title TEXT,
	body TEXT NOT NULL,
	target TEXT NOT NULL DEFAULT 'all',
	status TEXT NOT NULL DEFAULT 'queued',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	sent_at DATETIME
);

CREATE TABLE IF NOT EXISTS broadcast_messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	broadcast_id INTEGER NOT NULL REFERENCES broadcasts(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	status TEXT NOT NULL DEFAULT 'queued',
	error TEXT,
	sent_at DATETIME
);

CREATE TABLE IF NOT EXISTS support_messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	body TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'open',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	closed_at DATETIME
);

CREATE TABLE IF NOT EXISTS admin_actions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	admin_id INTEGER,
	role TEXT,
	action TEXT NOT NULL,
	entity_type TEXT,
	entity_id TEXT,
	metadata_json TEXT NOT NULL DEFAULT '{}',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS app_settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const indexesV1 = `
CREATE INDEX IF NOT EXISTS idx_users_telegram_id ON users(telegram_id);
CREATE INDEX IF NOT EXISTS idx_users_referral_code ON users(referral_code);
CREATE INDEX IF NOT EXISTS idx_users_invited_by ON users(invited_by_user_id);
CREATE INDEX IF NOT EXISTS idx_users_last_seen ON users(last_seen_at);
CREATE INDEX IF NOT EXISTS idx_subscriptions_user_status ON subscriptions(user_id, status);
CREATE INDEX IF NOT EXISTS idx_subscriptions_status_expires ON subscriptions(status, expires_at);
CREATE INDEX IF NOT EXISTS idx_payments_user_status ON payments(user_id, status);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments(status);
CREATE INDEX IF NOT EXISTS idx_payments_expires ON payments(expires_at);
CREATE INDEX IF NOT EXISTS idx_receipts_payment ON payment_receipts(payment_id);
CREATE INDEX IF NOT EXISTS idx_lessons_level ON lessons(level_id);
CREATE INDEX IF NOT EXISTS idx_lesson_progress_user ON lesson_progress(user_id);
CREATE INDEX IF NOT EXISTS idx_tests_level ON tests(level_id);
CREATE INDEX IF NOT EXISTS idx_test_attempts_user_test ON test_attempts(user_id, test_id);
CREATE INDEX IF NOT EXISTS idx_assignment_submissions_user ON assignment_submissions(user_id);
CREATE INDEX IF NOT EXISTS idx_referrals_inviter ON referrals(inviter_user_id);
CREATE INDEX IF NOT EXISTS idx_coin_transactions_user ON coin_transactions(user_id);
CREATE INDEX IF NOT EXISTS idx_channels_access ON channels(tariff_requirement, level_requirement, is_active);
CREATE INDEX IF NOT EXISTS idx_channel_invites_user ON channel_invite_links(user_id);
CREATE INDEX IF NOT EXISTS idx_streams_start ON live_streams(starts_at, status);
CREATE INDEX IF NOT EXISTS idx_admin_actions_created ON admin_actions(created_at);
`
