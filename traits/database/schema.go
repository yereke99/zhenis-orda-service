package database

const schemaV1 = `
CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	telegram_id INTEGER NOT NULL UNIQUE,
	username TEXT,
	first_name TEXT,
	last_name TEXT,
	photo_url TEXT,
	language TEXT NOT NULL DEFAULT '',
	phone TEXT,
	referral_code TEXT NOT NULL UNIQUE,
	invited_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
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
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
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
	id TEXT PRIMARY KEY,
	code TEXT NOT NULL UNIQUE,
	title TEXT NOT NULL,
	price_kzt INTEGER NOT NULL,
	short_description_kk TEXT NOT NULL DEFAULT '',
	full_description_kk TEXT NOT NULL DEFAULT '',
	features_json TEXT NOT NULL DEFAULT '[]',
	image_url TEXT,
	image_file_path TEXT,
	image_source TEXT NOT NULL DEFAULT 'none',
	sort_order INTEGER NOT NULL DEFAULT 0,
	is_active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS books (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	description TEXT NOT NULL,
	price_kzt INTEGER NOT NULL,
	image_url TEXT,
	image_file_path TEXT,
	image_source TEXT NOT NULL DEFAULT 'none',
	sort_order INTEGER NOT NULL DEFAULT 0,
	is_active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS free_lessons (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	short_description TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL,
	image_url TEXT,
	image_file_path TEXT,
	image_source TEXT NOT NULL DEFAULT 'none',
	youtube_url TEXT NOT NULL,
	youtube_video_id TEXT NOT NULL,
	youtube_embed_url TEXT NOT NULL,
	sort_order INTEGER NOT NULL DEFAULT 0,
	is_active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS subscriptions (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	tariff_id TEXT NOT NULL REFERENCES tariffs(id),
	status TEXT NOT NULL CHECK(status IN ('active','expired','cancelled','paused')),
	started_at DATETIME NOT NULL,
	expires_at DATETIME NOT NULL,
	cancelled_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS payments (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	tariff_id TEXT NOT NULL REFERENCES tariffs(id),
	subscription_id TEXT REFERENCES subscriptions(id) ON DELETE SET NULL,
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
	id TEXT PRIMARY KEY,
	payment_id TEXT NOT NULL REFERENCES payments(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	file_path TEXT NOT NULL,
	file_name TEXT,
	mime_type TEXT,
	file_size INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'uploaded',
	file_hash TEXT,
	raw_text_hash TEXT,
	qr_payload_hash TEXT,
	provider TEXT NOT NULL DEFAULT 'unknown',
	parsed_amount_kzt INTEGER,
	expected_amount_kzt INTEGER,
	amount_difference_kzt INTEGER,
	parsed_currency TEXT,
	parsed_transaction_id TEXT,
	receipt_transaction_key TEXT,
	parsed_check_id TEXT,
	parsed_reference_id TEXT,
	parsed_payment_date DATETIME,
	parsed_recipient TEXT,
	parsed_recipient_bin TEXT,
	expected_recipient_bin TEXT,
	parsed_payer_masked TEXT,
	validation_status TEXT NOT NULL DEFAULT 'uploaded',
	validation_errors TEXT NOT NULL DEFAULT '[]',
	duplicate_of_receipt_id TEXT REFERENCES payment_receipts(id) ON DELETE SET NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS levels (
	id TEXT PRIMARY KEY,
	number INTEGER NOT NULL UNIQUE,
	title_kk TEXT NOT NULL,
	title_ru TEXT NOT NULL,
	description_kk TEXT,
	description_ru TEXT,
	telegram_chat_id TEXT,
	sort_order INTEGER NOT NULL DEFAULT 0,
	is_active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS lessons (
	id TEXT PRIMARY KEY,
	level_id TEXT NOT NULL REFERENCES levels(id) ON DELETE CASCADE,
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
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	lesson_id TEXT NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
	watched INTEGER NOT NULL DEFAULT 0,
	watched_at DATETIME,
	coin_granted INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY(user_id, lesson_id)
);

CREATE TABLE IF NOT EXISTS tests (
	id TEXT PRIMARY KEY,
	level_id TEXT REFERENCES levels(id) ON DELETE CASCADE,
	lesson_id TEXT REFERENCES lessons(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	pass_percent INTEGER NOT NULL DEFAULT 70,
	is_active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS test_questions (
	id TEXT PRIMARY KEY,
	test_id TEXT NOT NULL REFERENCES tests(id) ON DELETE CASCADE,
	question_text_kk TEXT NOT NULL,
	question_text_ru TEXT NOT NULL,
	sort_order INTEGER NOT NULL DEFAULT 0,
	is_active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(test_id, sort_order)
);

CREATE TABLE IF NOT EXISTS test_options (
	id TEXT PRIMARY KEY,
	question_id TEXT NOT NULL REFERENCES test_questions(id) ON DELETE CASCADE,
	option_text_kk TEXT NOT NULL,
	option_text_ru TEXT NOT NULL,
	is_correct INTEGER NOT NULL DEFAULT 0,
	sort_order INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(question_id, sort_order)
);

CREATE TABLE IF NOT EXISTS test_attempts (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	test_id TEXT NOT NULL REFERENCES tests(id) ON DELETE CASCADE,
	score_percent INTEGER NOT NULL,
	correct_count INTEGER NOT NULL,
	total_count INTEGER NOT NULL,
	passed INTEGER NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS test_answers (
	id TEXT PRIMARY KEY,
	attempt_id TEXT NOT NULL REFERENCES test_attempts(id) ON DELETE CASCADE,
	question_id TEXT NOT NULL REFERENCES test_questions(id) ON DELETE CASCADE,
	selected_option_id TEXT REFERENCES test_options(id) ON DELETE SET NULL,
	is_correct INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS assignments (
	id TEXT PRIMARY KEY,
	level_id TEXT NOT NULL REFERENCES levels(id) ON DELETE CASCADE,
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
	id TEXT PRIMARY KEY,
	assignment_id TEXT NOT NULL REFERENCES assignments(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
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
	id TEXT PRIMARY KEY,
	inviter_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	invited_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	status TEXT NOT NULL DEFAULT 'registered' CHECK(status IN ('registered','paid','rewarded','cancelled')),
	reward_granted INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(invited_user_id)
);

CREATE TABLE IF NOT EXISTS referral_rewards (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	threshold_count INTEGER NOT NULL,
	reward_type TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'granted' CHECK(status IN ('pending','granted','cancelled')),
	source_referral_count INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(user_id, threshold_count)
);

CREATE TABLE IF NOT EXISTS coin_transactions (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	amount INTEGER NOT NULL,
	reason TEXT NOT NULL,
	source_type TEXT NOT NULL,
	source_id TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(user_id, reason, source_type, source_id)
);

CREATE TABLE IF NOT EXISTS channels (
	id TEXT PRIMARY KEY,
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
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	invite_link TEXT NOT NULL,
	expires_at DATETIME,
	status TEXT NOT NULL DEFAULT 'issued' CHECK(status IN ('issued','used','expired','revoked')),
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_level_telegram_invites (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	telegram_user_id INTEGER,
	level_id TEXT NOT NULL REFERENCES levels(id) ON DELETE CASCADE,
	telegram_chat_id TEXT NOT NULL,
	invite_link TEXT NOT NULL,
	invite_link_id TEXT,
	raw_payload TEXT NOT NULL DEFAULT '{}',
	expires_at DATETIME,
	status TEXT NOT NULL DEFAULT 'issued' CHECK(status IN ('issued','used','expired','revoked')),
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS financial_iq_results (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	score INTEGER NOT NULL,
	result_title TEXT NOT NULL,
	result_level TEXT NOT NULL,
	result_text TEXT NOT NULL DEFAULT '',
	answers_json TEXT NOT NULL DEFAULT '{}',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS live_streams (
	id TEXT PRIMARY KEY,
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
	id TEXT PRIMARY KEY,
	stream_id TEXT NOT NULL REFERENCES live_streams(id) ON DELETE CASCADE,
	reminder_key TEXT NOT NULL,
	sent_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(stream_id, reminder_key)
);

CREATE TABLE IF NOT EXISTS live_stream_recordings (
	id TEXT PRIMARY KEY,
	stream_id TEXT NOT NULL REFERENCES live_streams(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	recording_url TEXT NOT NULL,
	tariff_requirement TEXT NOT NULL DEFAULT 'STANDARD',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_stream_attendance (
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	stream_id TEXT NOT NULL REFERENCES live_streams(id) ON DELETE CASCADE,
	attended_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	coin_granted INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY(user_id, stream_id)
);

CREATE TABLE IF NOT EXISTS broadcasts (
	id TEXT PRIMARY KEY,
	admin_id INTEGER,
	title TEXT,
	body TEXT NOT NULL,
	target TEXT NOT NULL DEFAULT 'all',
	status TEXT NOT NULL DEFAULT 'queued',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	sent_at DATETIME
);

CREATE TABLE IF NOT EXISTS broadcast_messages (
	id TEXT PRIMARY KEY,
	broadcast_id TEXT NOT NULL REFERENCES broadcasts(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	telegram_id INTEGER NOT NULL,
	status TEXT NOT NULL DEFAULT 'queued',
	error TEXT,
	sent_at DATETIME
);

CREATE TABLE IF NOT EXISTS support_messages (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	body TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'open',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	closed_at DATETIME
);

CREATE TABLE IF NOT EXISTS admin_actions (
	id TEXT PRIMARY KEY,
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
CREATE INDEX IF NOT EXISTS idx_books_active_sort ON books(is_active, sort_order, created_at);
CREATE INDEX IF NOT EXISTS idx_free_lessons_active_sort ON free_lessons(is_active, sort_order, created_at);
CREATE INDEX IF NOT EXISTS idx_payments_user_status ON payments(user_id, status);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments(status);
CREATE INDEX IF NOT EXISTS idx_payments_expires ON payments(expires_at);
CREATE INDEX IF NOT EXISTS idx_receipts_payment ON payment_receipts(payment_id);
CREATE INDEX IF NOT EXISTS idx_receipts_file_hash ON payment_receipts(file_hash);
CREATE INDEX IF NOT EXISTS idx_receipts_qr_hash ON payment_receipts(qr_payload_hash);
CREATE INDEX IF NOT EXISTS idx_receipts_transaction ON payment_receipts(parsed_transaction_id);
CREATE INDEX IF NOT EXISTS idx_receipts_transaction_key ON payment_receipts(receipt_transaction_key);
CREATE INDEX IF NOT EXISTS idx_receipts_check ON payment_receipts(parsed_check_id);
CREATE INDEX IF NOT EXISTS idx_receipts_validation ON payment_receipts(validation_status);
CREATE INDEX IF NOT EXISTS idx_lessons_level ON lessons(level_id);
CREATE INDEX IF NOT EXISTS idx_lesson_progress_user ON lesson_progress(user_id);
CREATE INDEX IF NOT EXISTS idx_tests_level ON tests(level_id);
CREATE INDEX IF NOT EXISTS idx_tests_lesson ON tests(lesson_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tests_lesson_unique ON tests(lesson_id);
CREATE INDEX IF NOT EXISTS idx_test_attempts_user_test ON test_attempts(user_id, test_id);
CREATE INDEX IF NOT EXISTS idx_assignment_submissions_user ON assignment_submissions(user_id);
CREATE INDEX IF NOT EXISTS idx_referrals_inviter ON referrals(inviter_user_id);
CREATE INDEX IF NOT EXISTS idx_coin_transactions_user ON coin_transactions(user_id);
CREATE INDEX IF NOT EXISTS idx_channels_access ON channels(tariff_requirement, level_requirement, is_active);
CREATE INDEX IF NOT EXISTS idx_channel_invites_user ON channel_invite_links(user_id);
CREATE INDEX IF NOT EXISTS idx_level_invites_user_level ON user_level_telegram_invites(user_id, level_id, status);
CREATE INDEX IF NOT EXISTS idx_level_invites_status_expires ON user_level_telegram_invites(status, expires_at);
CREATE INDEX IF NOT EXISTS idx_financial_iq_user_created ON financial_iq_results(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_streams_start ON live_streams(starts_at, status);
CREATE INDEX IF NOT EXISTS idx_broadcasts_status ON broadcasts(status, created_at);
CREATE INDEX IF NOT EXISTS idx_broadcast_messages_broadcast ON broadcast_messages(broadcast_id, status);
CREATE INDEX IF NOT EXISTS idx_admin_actions_created ON admin_actions(created_at);
`
