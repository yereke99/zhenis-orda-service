package repository_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"zhenis-orda-service/internal/repository"
)

const testRecipientBIN = "830520499025"

func receiptOpts() repository.ReceiptValidationOptions {
	return repository.ReceiptValidationOptions{
		ExpectedRecipientBIN: testRecipientBIN,
		AllowedRecipientBINs: []string{testRecipientBIN},
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

func setBasicTariffPrice(t *testing.T, ctx context.Context, store *repository.Store, price int) {
	t.Helper()
	if _, err := store.DB().ExecContext(ctx, `UPDATE tariffs SET price_kzt = ? WHERE code = 'BASIC'`, price); err != nil {
		t.Fatal(err)
	}
}

func TestReceiptPDFValidationAutoApprovesPayment(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 7101, "")
	payment := createPayment(t, ctx, store, user.ID, "BASIC")
	path, size := writeReceiptPDF(t, validReceiptText("TX-VALID-1", "9 900,00", testRecipientBIN)+" QR: https://kaspi.kz/qr/receipt/TX-VALID-1")

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
	if !receipt.QRFound || receipt.DuplicateOfReceiptID != nil || hasValidationError(receipt, "duplicate_identity_found") {
		t.Fatalf("valid original receipt was treated as duplicate: %#v", receipt)
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

func TestKaspiFiscalReceiptNumberValidationAutoApprovesBasic(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 7106, "")
	payment := createPayment(t, ctx, store, user.ID, "BASIC")
	createdAt := time.Date(2026, 5, 22, 18, 50, 0, 0, time.UTC)
	if _, err := store.DB().ExecContext(ctx, `UPDATE payments SET created_at = ? WHERE id = ?`, createdAt, payment.ID); err != nil {
		t.Fatal(err)
	}
	text := "Kaspi fiscal receipt Өнім Қаржылық еркіндік № чека: QR15632984811 " +
		"Дата: 22.05.2026 23:53 ИИН/БИН продавца 830520499025 " +
		"Способ оплаты Kaspi Gold Сумма 9 900 ₸ QR: https://kaspi.kz/qr/receipt/QR15632984811"
	path, size := writeReceiptPDF(t, text)
	opts := receiptOpts()
	opts.AmountToleranceKZT = 500

	updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user.ID, payment.ID, path, "kaspi.pdf", "application/pdf", size, opts)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != repository.PaymentStatusApproved {
		t.Fatalf("expected approved payment, got %s with receipt %#v", updated.Status, receipt)
	}
	if receipt.ParsedCheckID != "QR15632984811" || receipt.ReceiptTransactionKey != "QR15632984811" {
		t.Fatalf("receipt number parsed incorrectly: check=%q key=%q", receipt.ParsedCheckID, receipt.ReceiptTransactionKey)
	}
	if receipt.ParsedAmountKZT == nil || *receipt.ParsedAmountKZT != 9900 || receipt.ParsedCurrency != "KZT" || receipt.ParsedRecipientBIN != testRecipientBIN {
		t.Fatalf("unexpected parsed receipt values: %#v", receipt)
	}
	if receipt.DuplicateOfReceiptID != nil || hasValidationError(receipt, "duplicate_identity_found") {
		t.Fatalf("valid fiscal receipt was marked duplicate: %#v", receipt)
	}
}

func TestReceiptValidationAcceptsSelectedTariffPrices(t *testing.T) {
	cases := []struct {
		name   string
		tariff string
		amount string
		want   int
	}{
		{name: "basic", tariff: "BASIC", amount: "9 900", want: 9900},
		{name: "standard", tariff: "STANDARD", amount: "24 900", want: 24900},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx := newTestStore(t)
			user := registerUser(t, ctx, store, int64(7102+i), "")
			payment := createPayment(t, ctx, store, user.ID, tc.tariff)
			if payment.AmountKZT != tc.want {
				t.Fatalf("payment amount = %d, want %d", payment.AmountKZT, tc.want)
			}
			text := "Kaspi OFD чек Номер чека QR15632984" + strconv.Itoa(i+20) + " transaction TX-" + tc.tariff + " ИИН/БИН продавца " + testRecipientBIN +
				" Сумма " + tc.amount + " ₸ QR: https://kaspi.kz/qr/receipt/TX-" + tc.tariff
			path, size := writeReceiptPDF(t, text)

			updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user.ID, payment.ID, path, "kaspi.pdf", "application/pdf", size, receiptOpts())
			if err != nil {
				t.Fatal(err)
			}
			if updated.Status != repository.PaymentStatusApproved {
				t.Fatalf("payment status = %s, receipt=%#v", updated.Status, receipt)
			}
			if receipt.ExpectedAmountKZT == nil || *receipt.ExpectedAmountKZT != tc.want {
				t.Fatalf("expected amount stored = %#v, want %d", receipt.ExpectedAmountKZT, tc.want)
			}
			if receipt.ParsedAmountKZT == nil || *receipt.ParsedAmountKZT != tc.want {
				t.Fatalf("parsed amount = %#v, want %d", receipt.ParsedAmountKZT, tc.want)
			}
			if receipt.DuplicateOfReceiptID != nil || hasValidationError(receipt, "duplicate_identity_found") {
				t.Fatalf("selected tariff receipt was marked duplicate: %#v", receipt)
			}
		})
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

func TestReceiptAmountToleranceAllowsSmallDifference(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 7110, "")
	payment := createPayment(t, ctx, store, user.ID, "BASIC")
	path, size := writeReceiptPDF(t, validReceiptText("TX-TOLERANCE-1", "9 500", testRecipientBIN))
	opts := receiptOpts()
	opts.AmountToleranceKZT = 500

	updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user.ID, payment.ID, path, "kaspi.pdf", "application/pdf", size, opts)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != repository.PaymentStatusApproved {
		t.Fatalf("payment status = %s, receipt errors=%v", updated.Status, receipt.ValidationErrors)
	}
	if receipt.AmountDifferenceKZT == nil || *receipt.AmountDifferenceKZT != -400 {
		t.Fatalf("amount diff = %#v", receipt.AmountDifferenceKZT)
	}
}

func TestReceiptBasicRejectsLowAmount(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 7112, "")
	payment := createPayment(t, ctx, store, user.ID, "BASIC")
	path, size := writeReceiptPDF(t, "Kaspi OFD чек Номер чека QR15632984900 ИИН/БИН продавца "+testRecipientBIN+" Сумма 100 ₸")
	opts := receiptOpts()
	opts.AmountToleranceKZT = 500

	updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user.ID, payment.ID, path, "kaspi.pdf", "application/pdf", size, opts)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != repository.PaymentStatusRejected {
		t.Fatalf("payment status = %s, receipt=%#v", updated.Status, receipt)
	}
	if receipt.ValidationStatus != repository.ReceiptStatusRejected || !hasValidationError(receipt, "amount_mismatch") {
		t.Fatalf("expected amount mismatch rejection, status=%s errors=%v", receipt.ValidationStatus, receipt.ValidationErrors)
	}
}

func TestReceiptStrictSelectedTariffAmountAndMerchant(t *testing.T) {
	cases := []struct {
		name       string
		amount     string
		bin        string
		text       string
		wantStatus string
		wantError  string
	}{
		{
			name:       "100 kzt with correct merchant rejects",
			amount:     "100",
			bin:        testRecipientBIN,
			wantStatus: repository.PaymentStatusRejected,
			wantError:  "amount_mismatch",
		},
		{
			name:       "exact amount valid",
			amount:     "9 500",
			bin:        testRecipientBIN,
			wantStatus: repository.PaymentStatusApproved,
		},
		{
			name:       "lower boundary valid",
			amount:     "9 000",
			bin:        testRecipientBIN,
			wantStatus: repository.PaymentStatusApproved,
		},
		{
			name:       "below lower boundary rejects",
			amount:     "8 999",
			bin:        testRecipientBIN,
			wantStatus: repository.PaymentStatusRejected,
			wantError:  "amount_mismatch",
		},
		{
			name:       "wrong merchant rejects",
			amount:     "9 500",
			bin:        "111111111111",
			wantStatus: repository.PaymentStatusRejected,
			wantError:  "recipient_bin_mismatch",
		},
		{
			name:       "missing merchant rejects",
			text:       "Kaspi OFD чек transaction TX-STRICT-MISSING-BIN Сумма 9 500 ₸",
			wantStatus: repository.PaymentStatusRejected,
			wantError:  "recipient_bin_missing",
		},
		{
			name:       "wrong currency rejects",
			text:       "Kaspi OFD чек transaction TX-STRICT-USD ИИН/БИН продавца " + testRecipientBIN + " Сумма 9 500 USD",
			wantStatus: repository.PaymentStatusRejected,
			wantError:  "currency_mismatch",
		},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx := newTestStore(t)
			setBasicTariffPrice(t, ctx, store, 9500)
			user := registerUser(t, ctx, store, int64(7120+i), "")
			payment := createPayment(t, ctx, store, user.ID, "BASIC")
			if payment.AmountKZT != 9500 {
				t.Fatalf("payment amount = %d", payment.AmountKZT)
			}
			text := tc.text
			if text == "" {
				text = "Kaspi OFD чек Номер чека QR15632985" + strconv.Itoa(i+10) + " transaction TX-STRICT-" + strconv.Itoa(i) + " ИИН/БИН продавца " + tc.bin + " Сумма " + tc.amount + " ₸"
			}
			path, size := writeReceiptPDF(t, text)
			opts := receiptOpts()
			opts.AmountToleranceKZT = 500

			updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user.ID, payment.ID, path, "kaspi.pdf", "application/pdf", size, opts)
			if err != nil {
				t.Fatal(err)
			}
			if updated.Status != tc.wantStatus {
				t.Fatalf("payment status = %s, receipt=%#v", updated.Status, receipt)
			}
			if tc.wantError != "" && !hasValidationError(receipt, tc.wantError) {
				t.Fatalf("expected error %s, got %v", tc.wantError, receipt.ValidationErrors)
			}
			if tc.wantStatus == repository.PaymentStatusRejected {
				sub, err := store.GetActiveSubscription(ctx, user.ID)
				if err != nil {
					t.Fatal(err)
				}
				if sub != nil {
					t.Fatalf("rejected receipt opened subscription: %#v", sub)
				}
				updatedUser, err := store.GetUserByID(ctx, user.ID)
				if err != nil {
					t.Fatal(err)
				}
				if updatedUser.CurrentLevel != 0 {
					t.Fatalf("rejected receipt changed level to %d", updatedUser.CurrentLevel)
				}
			}
		})
	}
}

func TestReceiptAmountOutsideToleranceRejectsPayment(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 7111, "")
	payment := createPayment(t, ctx, store, user.ID, "BASIC")
	path, size := writeReceiptPDF(t, validReceiptText("TX-TOLERANCE-2", "8 900", testRecipientBIN))
	opts := receiptOpts()
	opts.AmountToleranceKZT = 500

	updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user.ID, payment.ID, path, "kaspi.pdf", "application/pdf", size, opts)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != repository.PaymentStatusRejected {
		t.Fatalf("payment status = %s", updated.Status)
	}
	if receipt.ValidationStatus != repository.ReceiptStatusRejected || !hasValidationError(receipt, "amount_mismatch") {
		t.Fatalf("expected rejected amount mismatch, status=%s errors=%v", receipt.ValidationStatus, receipt.ValidationErrors)
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
			if updated.Status != repository.PaymentStatusRejected {
				t.Fatalf("invalid receipt status = %s: %#v", updated.Status, receipt)
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
	if updated.Status != repository.PaymentStatusRejected || receipt.ValidationStatus != repository.ReceiptStatusDuplicate {
		t.Fatalf("same file duplicate status payment=%s receipt=%s errors=%v", updated.Status, receipt.ValidationStatus, receipt.ValidationErrors)
	}

	payment3 := createPayment(t, ctx, store, user2.ID, "BASIC")
	path3, size3 := writeReceiptPDF(t, validReceiptText("TX-DUP-1", "9 900", testRecipientBIN)+" reference DIFFERENT-FILE")
	updated, receipt, err = store.AttachReceiptToPaymentWithValidation(ctx, user2.ID, payment3.ID, path3, "kaspi.pdf", "application/pdf", size3, receiptOpts())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != repository.PaymentStatusRejected || receipt.ValidationStatus != repository.ReceiptStatusDuplicate {
		t.Fatalf("same transaction duplicate status payment=%s receipt=%s errors=%v", updated.Status, receipt.ValidationStatus, receipt.ValidationErrors)
	}
}

func TestReceiptDuplicateUsesFiscalCheckNumber(t *testing.T) {
	store, ctx := newTestStore(t)
	user1 := registerUser(t, ctx, store, 7306, "")
	payment1 := createPayment(t, ctx, store, user1.ID, "BASIC")
	path1, size1 := writeReceiptPDF(t, "Kaspi OFD чек Номер чека QR15632984811 transaction TX-FIRST ИИН/БИН продавца "+testRecipientBIN+" Сумма 9 900 ₸")
	if _, _, err := store.AttachReceiptToPaymentWithValidation(ctx, user1.ID, payment1.ID, path1, "kaspi.pdf", "application/pdf", size1, receiptOpts()); err != nil {
		t.Fatal(err)
	}

	user2 := registerUser(t, ctx, store, 7307, "")
	payment2 := createPayment(t, ctx, store, user2.ID, "BASIC")
	path2, size2 := writeReceiptPDF(t, "Kaspi OFD чек № чека: QR15632984811 transaction TX-SECOND ИИН/БИН продавца "+testRecipientBIN+" Сумма 9 900 ₸")
	updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user2.ID, payment2.ID, path2, "kaspi.pdf", "application/pdf", size2, receiptOpts())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != repository.PaymentStatusRejected || receipt.ValidationStatus != repository.ReceiptStatusDuplicate {
		t.Fatalf("same fiscal check number should reject, payment=%s receipt=%#v", updated.Status, receipt)
	}
	if receipt.ParsedCheckID != "QR15632984811" || receipt.DuplicateOfReceiptID == nil {
		t.Fatalf("duplicate receipt number not stored correctly: %#v", receipt)
	}
}

func TestReceiptCheckNumberOverridesHashDuplicateSignals(t *testing.T) {
	store, ctx := newTestStore(t)
	user1 := registerUser(t, ctx, store, 7308, "")
	payment1 := createPayment(t, ctx, store, user1.ID, "BASIC")
	path1, size1 := writeReceiptPDF(t, "Kaspi OFD чек Номер чека QR15632984821 ИИН/БИН продавца "+testRecipientBIN+" Сумма 9 900 ₸ QR: https://kaspi.kz/qr/shared")
	if _, _, err := store.AttachReceiptToPaymentWithValidation(ctx, user1.ID, payment1.ID, path1, "kaspi.pdf", "application/pdf", size1, receiptOpts()); err != nil {
		t.Fatal(err)
	}

	user2 := registerUser(t, ctx, store, 7309, "")
	payment2 := createPayment(t, ctx, store, user2.ID, "BASIC")
	path2, size2 := writeReceiptPDF(t, "Kaspi OFD чек Номер чека QR15632984822 ИИН/БИН продавца "+testRecipientBIN+" Сумма 9 900 ₸ QR: https://kaspi.kz/qr/shared")
	updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user2.ID, payment2.ID, path2, "kaspi.pdf", "application/pdf", size2, receiptOpts())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != repository.PaymentStatusApproved {
		t.Fatalf("new fiscal check number should not be rejected by shared QR/hash, payment=%s receipt=%#v", updated.Status, receipt)
	}
	if receipt.ParsedCheckID != "QR15632984822" || receipt.DuplicateOfReceiptID != nil || hasValidationError(receipt, "duplicate_identity_found") {
		t.Fatalf("hash signal overrode receipt number uniqueness: %#v", receipt)
	}
}

func TestReceiptRejectedIdentityDoesNotPoisonValidUpload(t *testing.T) {
	store, ctx := newTestStore(t)
	user1 := registerUser(t, ctx, store, 7303, "")
	payment1 := createPayment(t, ctx, store, user1.ID, "BASIC")
	text := "Kaspi OFD чек transaction TX-REJECTED-POISON ИИН/БИН продавца " + testRecipientBIN +
		" Сумма 9 900 ₸ QR: https://kaspi.kz/qr/receipt/TX-REJECTED-POISON"
	path, size := writeReceiptPDF(t, text)
	parsed, err := repository.ParseReceiptPDF(path, payment1.AmountKZT, payment1.CreatedAt, payment1.ID, 500, testRecipientBIN)
	if err != nil {
		t.Fatal(err)
	}
	parsed = repository.BuildReceiptIdentity(parsed)
	parsedAmount := any(nil)
	if parsed.ParsedAmountKZT != nil {
		parsedAmount = *parsed.ParsedAmountKZT
	}
	amountDiff := any(nil)
	if parsed.AmountDifferenceKZT != nil {
		amountDiff = *parsed.AmountDifferenceKZT
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO payment_receipts(
			id, payment_id, user_id, file_path, file_name, mime_type, file_size, status,
			file_hash, raw_text_hash, qr_payload_hash, provider, parsed_amount_kzt, expected_amount_kzt,
			amount_difference_kzt, parsed_currency, parsed_transaction_id, receipt_transaction_key,
			parsed_check_id, parsed_reference_id, parsed_recipient_bin, expected_recipient_bin,
			validation_status, validation_errors
		)
		VALUES ('receipt-rejected-poison', ?, ?, ?, 'kaspi.pdf', 'application/pdf', ?, 'uploaded',
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'rejected', '["duplicate_identity_found"]');
	`, payment1.ID, user1.ID, path, size, parsed.FileHash, parsed.RawTextHash, parsed.QRPayloadHash, parsed.Provider,
		parsedAmount, parsed.ExpectedAmountKZT, amountDiff, parsed.ParsedCurrency, parsed.ParsedTransactionID,
		parsed.ReceiptTransactionKey, parsed.ParsedCheckID, parsed.ParsedReferenceID, parsed.ParsedRecipientBIN,
		parsed.ExpectedRecipientBIN); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE payments SET status = 'rejected' WHERE id = ?`, payment1.ID); err != nil {
		t.Fatal(err)
	}

	user2 := registerUser(t, ctx, store, 7304, "")
	payment2 := createPayment(t, ctx, store, user2.ID, "BASIC")
	updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user2.ID, payment2.ID, path, "kaspi.pdf", "application/pdf", size, receiptOpts())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != repository.PaymentStatusApproved {
		t.Fatalf("valid upload should approve despite old rejected receipt, payment=%s receipt=%#v", updated.Status, receipt)
	}
	if receipt.ValidationStatus != repository.ReceiptStatusApproved || receipt.DuplicateOfReceiptID != nil || hasValidationError(receipt, "duplicate_identity_found") {
		t.Fatalf("old rejected receipt poisoned duplicate detection: %#v", receipt)
	}
}

func TestReceiptReuploadSamePendingPaymentDoesNotSelfDuplicate(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 7310, "")
	payment := createPayment(t, ctx, store, user.ID, "BASIC")
	text := "Kaspi OFD чек Номер чека QR15632984831 Дата: 01.01.2020 12:00 ИИН/БИН продавца " + testRecipientBIN + " Сумма 9 900 ₸"
	path1, size1 := writeReceiptPDF(t, text)
	updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user.ID, payment.ID, path1, "kaspi.pdf", "application/pdf", size1, receiptOpts())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != repository.PaymentStatusUploadedReceipt || receipt.ValidationStatus != repository.ReceiptStatusSuspicious || !hasValidationError(receipt, "payment_date_too_early") {
		t.Fatalf("first suspicious upload status payment=%s receipt=%#v", updated.Status, receipt)
	}

	path2, size2 := writeReceiptPDF(t, text)
	updated, receipt, err = store.AttachReceiptToPaymentWithValidation(ctx, user.ID, payment.ID, path2, "kaspi.pdf", "application/pdf", size2, receiptOpts())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != repository.PaymentStatusUploadedReceipt || receipt.ValidationStatus != repository.ReceiptStatusSuspicious {
		t.Fatalf("second suspicious upload status payment=%s receipt=%#v", updated.Status, receipt)
	}
	if receipt.DuplicateOfReceiptID != nil || hasValidationError(receipt, "duplicate_identity_found") {
		t.Fatalf("same pending payment reupload self-matched duplicate: %#v", receipt)
	}
}

func TestReceiptDuplicateIgnoresCurrentPaymentReceipt(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 7305, "")
	payment := createPayment(t, ctx, store, user.ID, "BASIC")
	path, size := writeReceiptPDF(t, validReceiptText("TX-SAME-PAYMENT", "9 900", testRecipientBIN))
	parsed, err := repository.ParseReceiptPDF(path, payment.AmountKZT, payment.CreatedAt, payment.ID, 0, testRecipientBIN)
	if err != nil {
		t.Fatal(err)
	}
	parsed = repository.BuildReceiptIdentity(parsed)
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO payment_receipts(
			id, payment_id, user_id, file_path, file_name, mime_type, file_size, status,
			file_hash, raw_text_hash, provider, parsed_amount_kzt, parsed_currency,
			parsed_transaction_id, receipt_transaction_key, parsed_recipient_bin, expected_recipient_bin,
			validation_status, validation_errors
		)
		VALUES ('receipt-same-payment', ?, ?, ?, 'kaspi.pdf', 'application/pdf', ?, 'uploaded',
			?, ?, ?, ?, ?, ?, ?, ?, ?, 'rejected', '["amount_mismatch"]');
	`, payment.ID, user.ID, path, size, parsed.FileHash, parsed.RawTextHash, parsed.Provider, *parsed.ParsedAmountKZT,
		parsed.ParsedCurrency, parsed.ParsedTransactionID, parsed.ReceiptTransactionKey, parsed.ParsedRecipientBIN,
		parsed.ExpectedRecipientBIN); err != nil {
		t.Fatal(err)
	}

	updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user.ID, payment.ID, path, "kaspi.pdf", "application/pdf", size, receiptOpts())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != repository.PaymentStatusApproved {
		t.Fatalf("same payment receipt should not self-match as duplicate, payment=%s receipt=%#v", updated.Status, receipt)
	}
	if receipt.DuplicateOfReceiptID != nil || hasValidationError(receipt, "duplicate_identity_found") {
		t.Fatalf("same payment receipt self-matched duplicate: %#v", receipt)
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
	if updated.Status != repository.PaymentStatusRejected || receipt.ValidationStatus != repository.ReceiptStatusDuplicate {
		t.Fatalf("same QR duplicate status payment=%s receipt=%s errors=%v", updated.Status, receipt.ValidationStatus, receipt.ValidationErrors)
	}
}

func TestReceiptPremiumCourseUsesCoursePrice(t *testing.T) {
	store, ctx := newTestStore(t)
	course, err := store.GetPremiumCourseBySlug(ctx, "altyn-formula")
	if err != nil {
		t.Fatal(err)
	}
	if course.PriceKZT != 250000 {
		t.Fatalf("course price = %d", course.PriceKZT)
	}

	user1 := registerUser(t, ctx, store, 7601, "")
	payment1, err := store.CreatePremiumCoursePayment(ctx, user1.ID, course.ID, repository.PaymentProviderKaspiQR, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	path1, size1 := writeReceiptPDF(t, "Kaspi OFD чек Номер чека QR15632985000 ИИН/БИН продавца "+testRecipientBIN+" Сумма 250 000 ₸")
	updated, receipt, err := store.AttachReceiptToPaymentWithValidation(ctx, user1.ID, payment1.ID, path1, "kaspi.pdf", "application/pdf", size1, receiptOpts())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != repository.PaymentStatusApproved {
		t.Fatalf("premium payment should approve, status=%s receipt=%#v", updated.Status, receipt)
	}
	access, err := store.HasPremiumCourseAccess(ctx, user1.ID, course.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !access {
		t.Fatal("premium course access was not granted")
	}

	user2 := registerUser(t, ctx, store, 7602, "")
	payment2, err := store.CreatePremiumCoursePayment(ctx, user2.ID, course.ID, repository.PaymentProviderKaspiQR, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	path2, size2 := writeReceiptPDF(t, "Kaspi OFD чек Номер чека QR15632985001 ИИН/БИН продавца "+testRecipientBIN+" Сумма 9 900 ₸")
	updated, receipt, err = store.AttachReceiptToPaymentWithValidation(ctx, user2.ID, payment2.ID, path2, "kaspi.pdf", "application/pdf", size2, receiptOpts())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != repository.PaymentStatusRejected || !hasValidationError(receipt, "amount_mismatch") {
		t.Fatalf("premium payment accepted subscription amount, payment=%s receipt=%#v", updated.Status, receipt)
	}
	access, err = store.HasPremiumCourseAccess(ctx, user2.ID, course.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if access {
		t.Fatal("wrong premium receipt granted access")
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
	if updated.Status != repository.PaymentStatusRejected || !hasValidationError(receipt, "amount_mismatch") {
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
