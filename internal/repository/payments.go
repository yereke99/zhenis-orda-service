package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (s *Store) CreatePayment(ctx context.Context, userID string, tariffCode, provider string, ttl time.Duration) (Payment, error) {
	tariff, err := s.GetTariffByCode(ctx, tariffCode)
	if err != nil {
		return Payment{}, err
	}
	return s.createPaymentForTariff(ctx, userID, tariff, provider, ttl)
}

func (s *Store) CreatePaymentByTariffID(ctx context.Context, userID string, tariffID, provider string, ttl time.Duration) (Payment, error) {
	tariff, err := s.GetTariffByID(ctx, tariffID)
	if err != nil {
		return Payment{}, err
	}
	return s.createPaymentForTariff(ctx, userID, tariff, provider, ttl)
}

func (s *Store) createPaymentForTariff(ctx context.Context, userID string, tariff Tariff, provider string, ttl time.Duration) (Payment, error) {
	if !tariff.IsActive {
		return Payment{}, ErrInvalidState
	}
	provider = normalizeProvider(provider)
	paymentID := newID()
	expiresAt := nowUTC().Add(ttl)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO payments(id, user_id, tariff_id, amount_kzt, provider, status, expires_at)
		VALUES (?, ?, ?, ?, ?, 'pending', ?);
	`, paymentID, userID, tariff.ID, tariff.PriceKZT, provider, expiresAt)
	if err != nil {
		return Payment{}, err
	}
	return s.GetPayment(ctx, paymentID)
}

func (s *Store) LatestPendingPayment(ctx context.Context, userID string) (Payment, error) {
	payment, err := scanPaymentRow(s.db.QueryRowContext(ctx, paymentSelectSQL+`
		WHERE p.user_id = ? AND p.status IN ('pending','uploaded_receipt') AND (p.expires_at IS NULL OR p.expires_at > CURRENT_TIMESTAMP)
		ORDER BY p.created_at DESC
		LIMIT 1;
	`, userID))
	return payment, rowErr(err)
}

func (s *Store) GetPayment(ctx context.Context, paymentID string) (Payment, error) {
	payment, err := scanPaymentRow(s.db.QueryRowContext(ctx, paymentSelectSQL+` WHERE p.id = ?`, paymentID))
	return payment, rowErr(err)
}

func (s *Store) ApprovePayment(ctx context.Context, paymentID string, adminID int64, subscriptionDays int) (Payment, error) {
	return s.approvePayment(ctx, paymentID, AdminActor{ID: adminID, Role: RoleSuperAdmin}, subscriptionDays, "")
}

func (s *Store) ApprovePaymentReviewed(ctx context.Context, paymentID string, actor AdminActor, subscriptionDays int, overrideComment string) (Payment, error) {
	return s.approvePayment(ctx, paymentID, actor, subscriptionDays, overrideComment)
}

func (s *Store) approvePayment(ctx context.Context, paymentID string, actor AdminActor, subscriptionDays int, overrideComment string) (Payment, error) {
	var payment Payment
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		updated, err := s.approvePaymentTx(ctx, tx, paymentID, actor, subscriptionDays, overrideComment, true)
		if err != nil {
			return err
		}
		payment = updated
		return nil
	})
	return payment, err
}

func (s *Store) approvePaymentTx(ctx context.Context, tx *sql.Tx, paymentID string, actor AdminActor, subscriptionDays int, overrideComment string, enforceReviewRules bool) (Payment, error) {
	if subscriptionDays <= 0 {
		subscriptionDays = 30
	}
	found, err := scanPaymentRow(tx.QueryRowContext(ctx, paymentSelectSQL+` WHERE p.id = ?`, paymentID))
	if err != nil {
		return Payment{}, err
	}
	if found.Status == PaymentStatusApproved {
		return found, nil
	}
	if found.Status != PaymentStatusPending && found.Status != PaymentStatusUploadedReceipt {
		return Payment{}, ErrInvalidState
	}
	if found.ExpiresAt != nil && !found.ExpiresAt.After(nowUTC()) {
		return Payment{}, ErrPaymentExpired
	}
	receipt, err := latestReceiptForPaymentTx(ctx, tx, found.ID)
	if err != nil {
		return Payment{}, err
	}
	if enforceReviewRules && receipt != nil {
		comment := strings.TrimSpace(overrideComment)
		switch receipt.ValidationStatus {
		case ReceiptStatusDuplicate, ReceiptStatusSuspicious, ReceiptStatusRejected:
			if actor.Role != RoleSuperAdmin || comment == "" {
				return Payment{}, ErrInvalidState
			}
		case ReceiptStatusParseFailed, ReceiptStatusParsePartial:
			if comment == "" {
				return Payment{}, ErrInvalidState
			}
		}
	}
	now := nowUTC()
	startsAt := now
	expiresAt := now.AddDate(0, 0, subscriptionDays)
	var activeID sql.NullString
	var activeExpires sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT id, expires_at
		FROM subscriptions
		WHERE user_id = ? AND status = 'active' AND expires_at > CURRENT_TIMESTAMP
		ORDER BY expires_at DESC
		LIMIT 1;
	`, found.UserID).Scan(&activeID, &activeExpires); err != nil && err != sql.ErrNoRows {
		return Payment{}, err
	}
	var subscriptionID string
	if activeID.Valid {
		subscriptionID = activeID.String
		startsAt = activeExpires.Time
		expiresAt = activeExpires.Time.AddDate(0, 0, subscriptionDays)
		if _, err := tx.ExecContext(ctx, `
			UPDATE subscriptions
			SET tariff_id = ?, expires_at = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?;
		`, found.TariffID, expiresAt, subscriptionID); err != nil {
			return Payment{}, err
		}
	} else {
		subscriptionID = newID()
		_, err := tx.ExecContext(ctx, `
			INSERT INTO subscriptions(id, user_id, tariff_id, status, started_at, expires_at)
			VALUES (?, ?, ?, 'active', ?, ?);
		`, subscriptionID, found.UserID, found.TariffID, startsAt, expiresAt)
		if err != nil {
			return Payment{}, err
		}
	}
	approvedBy := any(actor.ID)
	if actor.ID == 0 {
		approvedBy = nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE payments
		SET status = 'approved', subscription_id = ?, approved_by_admin_id = ?, approved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?;
	`, subscriptionID, approvedBy, found.ID); err != nil {
		return Payment{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE users
		SET current_level = CASE WHEN current_level < 1 THEN 1 ELSE current_level END,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?;
	`, found.UserID); err != nil {
		return Payment{}, err
	}
	if err := s.applyReferralPaymentRewardsTx(ctx, tx, found.UserID); err != nil {
		return Payment{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE payment_receipts
		SET validation_status = 'approved'
		WHERE payment_id = ? AND id = (
			SELECT id FROM payment_receipts WHERE payment_id = ? ORDER BY created_at DESC LIMIT 1
		);
	`, found.ID, found.ID); err != nil {
		return Payment{}, err
	}
	return scanPaymentRow(tx.QueryRowContext(ctx, paymentSelectSQL+` WHERE p.id = ?`, found.ID))
}

func (s *Store) RejectPayment(ctx context.Context, paymentID string, adminID int64, comment string) (Payment, error) {
	var payment Payment
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		found, err := scanPaymentRow(tx.QueryRowContext(ctx, paymentSelectSQL+` WHERE p.id = ?`, paymentID))
		if err != nil {
			return err
		}
		if found.Status == PaymentStatusApproved {
			return ErrInvalidState
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE payments
			SET status = 'rejected', admin_comment = ?, approved_by_admin_id = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?;
		`, comment, adminID, paymentID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE payment_receipts
			SET validation_status = 'rejected'
			WHERE payment_id = ? AND id = (
				SELECT id FROM payment_receipts WHERE payment_id = ? ORDER BY created_at DESC LIMIT 1
			);
		`, paymentID, paymentID); err != nil {
			return err
		}
		updated, err := scanPaymentRow(tx.QueryRowContext(ctx, paymentSelectSQL+` WHERE p.id = ?`, paymentID))
		if err != nil {
			return err
		}
		payment = updated
		return nil
	})
	return payment, err
}

func latestReceiptForPaymentTx(ctx context.Context, tx *sql.Tx, paymentID string) (*Receipt, error) {
	receipt, err := scanReceiptRow(tx.QueryRowContext(ctx, receiptSelectSQL+`
		WHERE r.payment_id = ?
		ORDER BY r.created_at DESC
		LIMIT 1;
	`, paymentID))
	if err == ErrNotFound {
		return nil, nil
	}
	return &receipt, err
}

func (s *Store) ExpirePendingPayments(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE payments
		SET status = 'expired', updated_at = CURRENT_TIMESTAMP
		WHERE status = 'pending' AND expires_at IS NOT NULL AND expires_at <= CURRENT_TIMESTAMP;
	`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) ExpireSubscriptions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE subscriptions
		SET status = 'expired', updated_at = CURRENT_TIMESTAMP
		WHERE status = 'active' AND expires_at <= CURRENT_TIMESTAMP;
	`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) applyReferralPaymentRewardsTx(ctx context.Context, tx *sql.Tx, invitedUserID string) error {
	var referralID, inviterID string
	err := tx.QueryRowContext(ctx, `
		SELECT id, inviter_user_id
		FROM referrals
		WHERE invited_user_id = ?;
	`, invitedUserID).Scan(&referralID, &inviterID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE referrals
		SET status = 'paid', reward_granted = 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'registered';
	`, referralID); err != nil {
		return err
	}
	if err := s.AddCoinsTx(ctx, tx, inviterID, 100, "referral_paid", "referral", sourceID(invitedUserID)); err != nil {
		return err
	}
	var paidCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM referrals
		WHERE inviter_user_id = ? AND status IN ('paid','rewarded');
	`, inviterID).Scan(&paidCount); err != nil {
		return err
	}
	thresholds := map[int]string{
		1:  "7_days_free",
		3:  "1_month_free",
		5:  "closed_vip_stream",
		10: "personal_mini_review",
		20: "1_month_vip_tariff_access",
		50: "personal_zoom_with_mentor",
	}
	for threshold, rewardType := range thresholds {
		if paidCount >= threshold {
			if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO referral_rewards(user_id, threshold_count, reward_type, status, source_referral_count)
				VALUES (?, ?, ?, 'granted', ?);
			`, inviterID, threshold, rewardType, paidCount); err != nil {
				return err
			}
			if threshold == 1 {
				if err := s.extendSubscriptionTx(ctx, tx, inviterID, 7, "BASIC"); err != nil {
					return err
				}
			}
			if threshold == 3 {
				if err := s.extendSubscriptionTx(ctx, tx, inviterID, 30, "BASIC"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *Store) extendSubscriptionTx(ctx context.Context, tx *sql.Tx, userID string, days int, fallbackTariffCode string) error {
	var subID sql.NullString
	var expires sql.NullTime
	err := tx.QueryRowContext(ctx, `
		SELECT id, expires_at
		FROM subscriptions
		WHERE user_id = ? AND status = 'active' AND expires_at > CURRENT_TIMESTAMP
		ORDER BY expires_at DESC
		LIMIT 1;
	`, userID).Scan(&subID, &expires)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if subID.Valid {
		_, err := tx.ExecContext(ctx, `UPDATE subscriptions SET expires_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, expires.Time.AddDate(0, 0, days), subID.String)
		return err
	}
	var tariffID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM tariffs WHERE code = ?`, fallbackTariffCode).Scan(&tariffID); err != nil {
		return err
	}
	now := nowUTC()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO subscriptions(id, user_id, tariff_id, status, started_at, expires_at)
		VALUES (?, ?, ?, 'active', ?, ?);
	`, newID(), userID, tariffID, now, now.AddDate(0, 0, days))
	return err
}

const paymentSelectSQL = `
	SELECT p.id, p.user_id, p.tariff_id, t.code, p.subscription_id, p.amount_kzt, p.provider, p.status,
		COALESCE(p.receipt_file_path, ''), COALESCE(p.admin_comment, ''), p.approved_by_admin_id,
		p.approved_at, p.expires_at, p.created_at, p.updated_at
	FROM payments p
	JOIN tariffs t ON t.id = p.tariff_id`

func scanPaymentRow(row interface{ Scan(dest ...any) error }) (Payment, error) {
	var payment Payment
	var subscriptionID sql.NullString
	var approvedBy sql.NullInt64
	var approvedAt, expiresAt sql.NullTime
	if err := row.Scan(&payment.ID, &payment.UserID, &payment.TariffID, &payment.TariffCode, &subscriptionID, &payment.AmountKZT, &payment.Provider, &payment.Status, &payment.ReceiptFilePath, &payment.AdminComment, &approvedBy, &approvedAt, &expiresAt, &payment.CreatedAt, &payment.UpdatedAt); err != nil {
		return Payment{}, rowErr(err)
	}
	payment.SubscriptionID = scanStringPtr(subscriptionID)
	payment.ApprovedByAdminID = scanInt64(approvedBy)
	payment.ApprovedAt = scanTime(approvedAt)
	payment.ExpiresAt = scanTime(expiresAt)
	return payment, nil
}

func (s *Store) ManualAddSubscriptionDays(ctx context.Context, userID string, days int, tariffCode string) error {
	if days <= 0 {
		return ErrInvalidState
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		return s.extendSubscriptionTx(ctx, tx, userID, days, tariffCode)
	})
}

func (s *Store) ManualAdjustCoins(ctx context.Context, userID string, amount int, reason string, adminID int64) error {
	_, err := s.AddCoins(ctx, userID, amount, reason, "admin", fmt.Sprintf("%d:%d", adminID, time.Now().UnixNano()))
	return err
}
