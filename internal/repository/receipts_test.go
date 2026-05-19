package repository_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zhenis-orda-service/internal/repository"
)

const testRecipientBIN = "830520499025"

func receiptOpts() repository.ReceiptValidationOptions {
	return repository.ReceiptValidationOptions{
		ExpectedRecipientBIN: testRecipientBIN,
		AmountToleranceKZT:   0,
		SubscriptionDays:     30,
	}
}

func writeReceiptPDF(t *testing.T, text string) (string, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "receipt.pdf")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, int64(len(text))
}

func createPayment(t *testing.T, ctx context.Context, store *repository.Store, userID, tariff string) repository.Payment {
	t.Helper()
	payment, err := store.CreatePayment(ctx, userID, tariff, repository.PaymentProviderKaspiQR, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return payment
}

func hasValidationError(receipt repository.Receipt, code string) bool {
	for _, value := range receipt.ValidationErrors {
		if value == code {
			return true
		}
	}
	return false
}

func validReceiptText(tx, amount, bin string) string {
	return "Kaspi чек transaction " + tx + " Получатель БИН " + bin + " Сумма " + amount + " ₸"
}

func TestReceiptPDFValidationAutoApprovesPayment(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 7101, "")
	payment := createPayment(t, ctx, store, user.ID, "BASIC")
	path, size := writeReceiptPDF(t, validReceiptText("TX-VALID-1", "9 900,00", testRecipientBIN))

	updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user.ID, payment.ID, path, "kaspi.pdf", "application/pdf", size, receiptOpts())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != repository.PaymentStatusApproved {
		t.Fatalf("payment status = %s, receipt = %#v", updated.Status, receipt)
	}
	if receipt.ValidationStatus != repository.ReceiptStatusApproved {
		t.Fatalf("receipt validation status = %s", receipt.ValidationStatus)
	}
	if receipt.ParsedAmountKZT == nil || *receipt.ParsedAmountKZT != 9900 {
		t.Fatalf("parsed amount = %#v", receipt.ParsedAmountKZT)
	}
	if receipt.ExpectedAmountKZT == nil || *receipt.ExpectedAmountKZT != 9900 {
		t.Fatalf("expected amount stored = %#v", receipt.ExpectedAmountKZT)
	}
	if receipt.AmountDifferenceKZT == nil || *receipt.AmountDifferenceKZT != 0 {
		t.Fatalf("amount diff = %#v", receipt.AmountDifferenceKZT)
	}
	if receipt.ParsedRecipientBIN != testRecipientBIN || receipt.ExpectedRecipientBIN != testRecipientBIN {
		t.Fatalf("recipient BIN parsed=%q expected=%q", receipt.ParsedRecipientBIN, receipt.ExpectedRecipientBIN)
	}
	if receipt.ReceiptTransactionKey == "" {
		t.Fatal("expected normalized transaction key")
	}
	sub, err := store.GetActiveSubscription(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sub == nil || sub.Status != repository.SubscriptionStatusActive {
		t.Fatalf("expected active subscription, got %#v", sub)
	}
	updatedUser, err := store.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedUser.CurrentLevel < 1 {
		t.Fatalf("current level = %d", updatedUser.CurrentLevel)
	}
}

func TestReceiptAmountFormatsParseAsKZT(t *testing.T) {
	for _, amount := range []string{"9900", "9900.00", "9900,00", "9 900", "9 900,00", "9 900.00", "9\u00a0900,00"} {
		t.Run(amount, func(t *testing.T) {
			path, _ := writeReceiptPDF(t, validReceiptText("TX-"+strings.ReplaceAll(amount, " ", ""), amount, testRecipientBIN))
			parsed, err := repository.ParseReceiptPDF(path, 9900, time.Now(), "payment-1", 0, testRecipientBIN)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.ParsedAmountKZT == nil || *parsed.ParsedAmountKZT != 9900 {
				t.Fatalf("amount %q parsed as %#v; errors=%v", amount, parsed.ParsedAmountKZT, parsed.ValidationErrors)
			}
			if parsed.ValidationStatus != repository.ReceiptStatusValidCandidate {
				t.Fatalf("amount %q status=%s errors=%v", amount, parsed.ValidationStatus, parsed.ValidationErrors)
			}
		})
	}
}

func TestReceiptValidationBlocksWrongAmountAndRecipient(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		wantError string
	}{
		{
			name:      "wrong amount",
			text:      validReceiptText("TX-WRONG-AMOUNT", "900", testRecipientBIN),
			wantError: "amount_mismatch",
		},
		{
			name:      "wrong recipient BIN",
			text:      validReceiptText("TX-WRONG-BIN", "9 900", "111111111111"),
			wantError: "recipient_bin_mismatch",
		},
		{
			name:      "missing recipient BIN",
			text:      "Kaspi чек transaction TX-NO-BIN Сумма 9 900 ₸",
			wantError: "recipient_bin_missing",
		},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx := newTestStore(t)
			user := registerUser(t, ctx, store, int64(7200+i), "")
			payment := createPayment(t, ctx, store, user.ID, "BASIC")
			path, size := writeReceiptPDF(t, tc.text)

			updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user.ID, payment.ID, path, "kaspi.pdf", "application/pdf", size, receiptOpts())
			if err != nil {
				t.Fatal(err)
			}
			if updated.Status == repository.PaymentStatusApproved {
				t.Fatalf("invalid receipt approved: %#v", receipt)
			}
			if !hasValidationError(receipt, tc.wantError) {
				t.Fatalf("expected %s, got %v", tc.wantError, receipt.ValidationErrors)
			}
			sub, err := store.GetActiveSubscription(ctx, user.ID)
			if err != nil {
				t.Fatal(err)
			}
			if sub != nil {
				t.Fatalf("invalid receipt opened subscription: %#v", sub)
			}
		})
	}
}

func TestReceiptDuplicateIdentitiesAreIdempotent(t *testing.T) {
	store, ctx := newTestStore(t)
	user1 := registerUser(t, ctx, store, 7301, "")
	payment1 := createPayment(t, ctx, store, user1.ID, "BASIC")
	path1, size1 := writeReceiptPDF(t, validReceiptText("TX-DUP-1", "9 900", testRecipientBIN))
	if _, _, err := store.AttachReceiptToPaymentWithValidation(ctx, user1.ID, payment1.ID, path1, "kaspi.pdf", "application/pdf", size1, receiptOpts()); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.AttachReceiptWithValidation(ctx, user1.ID, path1, "kaspi.pdf", "application/pdf", size1, receiptOpts()); !errors.Is(err, repository.ErrReceiptAlreadyApproved) {
		t.Fatalf("same approved receipt error = %v", err)
	}

	user2 := registerUser(t, ctx, store, 7302, "")
	payment2 := createPayment(t, ctx, store, user2.ID, "BASIC")
	updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user2.ID, payment2.ID, path1, "kaspi.pdf", "application/pdf", size1, receiptOpts())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status == repository.PaymentStatusApproved || receipt.ValidationStatus != repository.ReceiptStatusDuplicate {
		t.Fatalf("same file duplicate status payment=%s receipt=%s errors=%v", updated.Status, receipt.ValidationStatus, receipt.ValidationErrors)
	}

	payment3 := createPayment(t, ctx, store, user2.ID, "BASIC")
	path3, size3 := writeReceiptPDF(t, validReceiptText("TX-DUP-1", "9 900", testRecipientBIN)+" reference DIFFERENT-FILE")
	updated, receipt, err = store.AttachReceiptToPaymentWithValidation(ctx, user2.ID, payment3.ID, path3, "kaspi.pdf", "application/pdf", size3, receiptOpts())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status == repository.PaymentStatusApproved || receipt.ValidationStatus != repository.ReceiptStatusDuplicate {
		t.Fatalf("same transaction duplicate status payment=%s receipt=%s errors=%v", updated.Status, receipt.ValidationStatus, receipt.ValidationErrors)
	}
}

func TestReceiptQRIdentityCanApproveAndRejectDuplicate(t *testing.T) {
	store, ctx := newTestStore(t)
	user1 := registerUser(t, ctx, store, 7401, "")
	payment1 := createPayment(t, ctx, store, user1.ID, "BASIC")
	path1, size1 := writeReceiptPDF(t, "Kaspi чек QR: https://kaspi.kz/qr/receipt/ABC123 Получатель БИН "+testRecipientBIN+" Сумма 9 900 ₸")

	updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user1.ID, payment1.ID, path1, "kaspi.pdf", "application/pdf", size1, receiptOpts())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != repository.PaymentStatusApproved || receipt.QRPayloadHash == "" {
		t.Fatalf("QR receipt should approve with hash, payment=%s receipt=%#v", updated.Status, receipt)
	}

	user2 := registerUser(t, ctx, store, 7402, "")
	payment2 := createPayment(t, ctx, store, user2.ID, "BASIC")
	path2, size2 := writeReceiptPDF(t, "Kaspi чек QR: https://kaspi.kz/qr/receipt/ABC123 Получатель БИН "+testRecipientBIN+" Сумма 9 900 ₸ other file")
	updated, receipt, err = store.AttachReceiptToPaymentWithValidation(ctx, user2.ID, payment2.ID, path2, "kaspi.pdf", "application/pdf", size2, receiptOpts())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status == repository.PaymentStatusApproved || receipt.ValidationStatus != repository.ReceiptStatusDuplicate {
		t.Fatalf("same QR duplicate status payment=%s receipt=%s errors=%v", updated.Status, receipt.ValidationStatus, receipt.ValidationErrors)
	}
}

func TestReceiptDoesNotApproveExpiredCancelledOrWrongPendingPayment(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 7501, "")

	expired := createPayment(t, ctx, store, user.ID, "BASIC")
	if _, err := store.DB().ExecContext(ctx, `UPDATE payments SET expires_at = datetime('now', '-1 minute') WHERE id = ?`, expired.ID); err != nil {
		t.Fatal(err)
	}
	path, size := writeReceiptPDF(t, validReceiptText("TX-EXPIRED", "9 900", testRecipientBIN))
	if _, _, err := store.AttachReceiptToPaymentWithValidation(ctx, user.ID, expired.ID, path, "kaspi.pdf", "application/pdf", size, receiptOpts()); !errors.Is(err, repository.ErrPaymentExpired) {
		t.Fatalf("expired payment error = %v", err)
	}

	cancelled := createPayment(t, ctx, store, user.ID, "BASIC")
	if _, err := store.DB().ExecContext(ctx, `UPDATE payments SET status = 'cancelled' WHERE id = ?`, cancelled.ID); err != nil {
		t.Fatal(err)
	}
	path, size = writeReceiptPDF(t, validReceiptText("TX-CANCELLED", "9 900", testRecipientBIN))
	if _, _, err := store.AttachReceiptToPaymentWithValidation(ctx, user.ID, cancelled.ID, path, "kaspi.pdf", "application/pdf", size, receiptOpts()); !errors.Is(err, repository.ErrPaymentCancelled) {
		t.Fatalf("cancelled payment error = %v", err)
	}

	older := createPayment(t, ctx, store, user.ID, "BASIC")
	newer := createPayment(t, ctx, store, user.ID, "STANDARD")
	if _, err := store.DB().ExecContext(ctx, `UPDATE payments SET created_at = datetime('now', '-2 minutes') WHERE id = ?`, older.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE payments SET created_at = datetime('now', '-1 minutes') WHERE id = ?`, newer.ID); err != nil {
		t.Fatal(err)
	}
	path, size = writeReceiptPDF(t, validReceiptText("TX-MULTIPLE", "9 900", testRecipientBIN))
	updated, receipt, err := store.AttachReceiptWithValidation(ctx, user.ID, path, "kaspi.pdf", "application/pdf", size, receiptOpts())
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != newer.ID {
		t.Fatalf("expected latest payment %s, got %s", newer.ID, updated.ID)
	}
	if updated.Status == repository.PaymentStatusApproved || !hasValidationError(receipt, "amount_mismatch") {
		t.Fatalf("latest mismatching payment should not approve: payment=%s errors=%v", updated.Status, receipt.ValidationErrors)
	}
	olderAfter, err := store.GetPayment(ctx, older.ID)
	if err != nil {
		t.Fatal(err)
	}
	if olderAfter.Status != repository.PaymentStatusPending {
		t.Fatalf("older payment was touched: %s", olderAfter.Status)
	}
}
