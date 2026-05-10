package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s *Store) CreatePayment(ctx context.Context, userID int64, tariffCode, provider string, ttl time.Duration) (Payment, error) {
	tariff, err := s.GetTariffByCode(ctx, tariffCode)
	if err != nil {
		return Payment{}, err
	}
	if !tariff.IsActive {
		return Payment{}, ErrInvalidState
	}
	provider = normalizeProvider(provider)
	expiresAt := nowUTC().Add(ttl)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO payments(user_id, tariff_id, amount_kzt, provider, status, expires_at)
		VALUES (?, ?, ?, ?, 'pending', ?);
	`, userID, tariff.ID, tariff.PriceKZT, provider, expiresAt)
	if err != nil {
		return Payment{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Payment{}, err
	}
	return s.GetPayment(ctx, id)
}

func (s *Store) LatestPendingPayment(ctx context.Context, userID int64) (Payment, error) {
	payment, err := scanPaymentRow(s.db.QueryRowContext(ctx, paymentSelectSQL+`
		WHERE p.user_id = ? AND p.status IN ('pending','uploaded_receipt') AND (p.expires_at IS NULL OR p.expires_at > CURRENT_TIMESTAMP)
		ORDER BY p.created_at DESC
		LIMIT 1;
	`, userID))
	return payment, rowErr(err)
}

func (s *Store) GetPayment(ctx context.Context, paymentID int64) (Payment, error) {
	payment, err := scanPaymentRow(s.db.QueryRowContext(ctx, paymentSelectSQL+` WHERE p.id = ?`, paymentID))
	return payment, rowErr(err)
}

func (s *Store) AttachReceipt(ctx context.Context, userID int64, filePath, fileName, mimeType string, fileSize int64) (Payment, error) {
	var payment Payment
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		found, err := scanPaymentRow(tx.QueryRowContext(ctx, paymentSelectSQL+`
			WHERE p.user_id = ? AND p.status = 'pending' AND (p.expires_at IS NULL OR p.expires_at > CURRENT_TIMESTAMP)
			ORDER BY p.created_at DESC
			LIMIT 1;
		`, userID))
		if err != nil {
			return rowErr(err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO payment_receipts(payment_id, user_id, file_path, file_name, mime_type, file_size)
			VALUES (?, ?, ?, ?, ?, ?);
		`, found.ID, userID, filePath, fileName, mimeType, fileSize); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE payments
			SET status = 'uploaded_receipt', receipt_file_path = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?;
		`, filePath, found.ID); err != nil {
			return err
		}
		updated, err := scanPaymentRow(tx.QueryRowContext(ctx, paymentSelectSQL+` WHERE p.id = ?`, found.ID))
		if err != nil {
			return err
		}
		payment = updated
		return nil
	})
	return payment, err
}

func (s *Store) ApprovePayment(ctx context.Context, paymentID int64, adminID int64, subscriptionDays int) (Payment, error) {
	if subscriptionDays <= 0 {
		subscriptionDays = 30
	}
	var payment Payment
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		found, err := scanPaymentRow(tx.QueryRowContext(ctx, paymentSelectSQL+` WHERE p.id = ?`, paymentID))
		if err != nil {
			return err
		}
		if found.Status == PaymentStatusApproved {
			payment = found
			return nil
		}
		if found.Status != PaymentStatusPending && found.Status != PaymentStatusUploadedReceipt {
			return ErrInvalidState
		}
		now := nowUTC()
		startsAt := now
		expiresAt := now.AddDate(0, 0, subscriptionDays)
		var activeID sql.NullInt64
		var activeExpires sql.NullTime
		if err := tx.QueryRowContext(ctx, `
			SELECT id, expires_at
			FROM subscriptions
			WHERE user_id = ? AND status = 'active' AND expires_at > CURRENT_TIMESTAMP
			ORDER BY expires_at DESC
			LIMIT 1;
		`, found.UserID).Scan(&activeID, &activeExpires); err != nil && err != sql.ErrNoRows {
			return err
		}
		var subscriptionID int64
		if activeID.Valid {
			subscriptionID = activeID.Int64
			startsAt = activeExpires.Time
			expiresAt = activeExpires.Time.AddDate(0, 0, subscriptionDays)
			if _, err := tx.ExecContext(ctx, `
				UPDATE subscriptions
				SET tariff_id = ?, expires_at = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?;
			`, found.TariffID, expiresAt, subscriptionID); err != nil {
				return err
			}
		} else {
			res, err := tx.ExecContext(ctx, `
				INSERT INTO subscriptions(user_id, tariff_id, status, started_at, expires_at)
				VALUES (?, ?, 'active', ?, ?);
			`, found.UserID, found.TariffID, startsAt, expiresAt)
			if err != nil {
				return err
			}
			subscriptionID, err = res.LastInsertId()
			if err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE payments
			SET status = 'approved', subscription_id = ?, approved_by_admin_id = ?, approved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?;
		`, subscriptionID, adminID, found.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE users
			SET current_level = CASE WHEN current_level < 1 THEN 1 ELSE current_level END,
				updated_at = CURRENT_TIMESTAMP
			WHERE id = ?;
		`, found.UserID); err != nil {
			return err
		}
		if err := s.applyReferralPaymentRewardsTx(ctx, tx, found.UserID); err != nil {
			return err
		}
		updated, err := scanPaymentRow(tx.QueryRowContext(ctx, paymentSelectSQL+` WHERE p.id = ?`, found.ID))
		if err != nil {
			return err
		}
		payment = updated
		return nil
	})
	return payment, err
}

func (s *Store) RejectPayment(ctx context.Context, paymentID int64, adminID int64, comment string) (Payment, error) {
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
		updated, err := scanPaymentRow(tx.QueryRowContext(ctx, paymentSelectSQL+` WHERE p.id = ?`, paymentID))
		if err != nil {
			return err
		}
		payment = updated
		return nil
	})
	return payment, err
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

func (s *Store) applyReferralPaymentRewardsTx(ctx context.Context, tx *sql.Tx, invitedUserID int64) error {
	var referralID, inviterID int64
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

func (s *Store) extendSubscriptionTx(ctx context.Context, tx *sql.Tx, userID int64, days int, fallbackTariffCode string) error {
	var subID sql.NullInt64
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
		_, err := tx.ExecContext(ctx, `UPDATE subscriptions SET expires_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, expires.Time.AddDate(0, 0, days), subID.Int64)
		return err
	}
	var tariffID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM tariffs WHERE code = ?`, fallbackTariffCode).Scan(&tariffID); err != nil {
		return err
	}
	now := nowUTC()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO subscriptions(user_id, tariff_id, status, started_at, expires_at)
		VALUES (?, ?, 'active', ?, ?);
	`, userID, tariffID, now, now.AddDate(0, 0, days))
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
	var subscriptionID, approvedBy sql.NullInt64
	var approvedAt, expiresAt sql.NullTime
	if err := row.Scan(&payment.ID, &payment.UserID, &payment.TariffID, &payment.TariffCode, &subscriptionID, &payment.AmountKZT, &payment.Provider, &payment.Status, &payment.ReceiptFilePath, &payment.AdminComment, &approvedBy, &approvedAt, &expiresAt, &payment.CreatedAt, &payment.UpdatedAt); err != nil {
		return Payment{}, rowErr(err)
	}
	payment.SubscriptionID = scanInt64(subscriptionID)
	payment.ApprovedByAdminID = scanInt64(approvedBy)
	payment.ApprovedAt = scanTime(approvedAt)
	payment.ExpiresAt = scanTime(expiresAt)
	return payment, nil
}

func (s *Store) ManualAddSubscriptionDays(ctx context.Context, userID int64, days int, tariffCode string) error {
	if days <= 0 {
		return ErrInvalidState
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		return s.extendSubscriptionTx(ctx, tx, userID, days, tariffCode)
	})
}

func (s *Store) ManualAdjustCoins(ctx context.Context, userID int64, amount int, reason string, adminID int64) error {
	_, err := s.AddCoins(ctx, userID, amount, reason, "admin", fmt.Sprintf("%d:%d", adminID, time.Now().UnixNano()))
	return err
}
