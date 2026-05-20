package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ledongthuc/pdf"
)

const receiptDateTolerance = 15 * time.Minute

type ReceiptValidationOptions struct {
	ExpectedRecipientBIN string
	AmountToleranceKZT   int
	SubscriptionDays     int
}

type ReceiptParseResult struct {
	FileHash              string
	RawText               string
	RawTextHash           string
	QRPayload             string
	QRPayloadHash         string
	Provider              string
	ExpectedAmountKZT     int
	ParsedAmountKZT       *int
	AmountDifferenceKZT   *int
	ParsedCurrency        string
	ParsedTransactionID   string
	ReceiptTransactionKey string
	ParsedCheckID         string
	ParsedReferenceID     string
	ParsedPaymentDate     *time.Time
	ParsedRecipient       string
	ParsedRecipientBIN    string
	ExpectedRecipientBIN  string
	ParsedPayerMasked     string
	ValidationStatus      string
	ValidationErrors      []string
}

func ParseReceipt(filePath string, expectedAmount int, paymentCreatedAt time.Time, expectedProvider string) (ReceiptParseResult, error) {
	return parseReceiptPDFWithProvider(filePath, expectedAmount, paymentCreatedAt, "", 0, "", expectedProvider)
}

func ParseReceiptPDF(filePath string, expectedAmount int, paymentCreatedAt time.Time, paymentID string, toleranceKZT int, expectedRecipientBIN string) (ReceiptParseResult, error) {
	return parseReceiptPDFWithProvider(filePath, expectedAmount, paymentCreatedAt, paymentID, toleranceKZT, expectedRecipientBIN, "")
}

func parseReceiptPDFWithProvider(filePath string, expectedAmount int, paymentCreatedAt time.Time, paymentID string, toleranceKZT int, expectedRecipientBIN, expectedProvider string) (ReceiptParseResult, error) {
	result := ReceiptParseResult{
		Provider:             "unknown",
		ExpectedAmountKZT:    expectedAmount,
		ExpectedRecipientBIN: normalizeDigits(expectedRecipientBIN),
		ValidationStatus:     ReceiptStatusUploaded,
		ValidationErrors:     []string{},
	}
	if toleranceKZT < 0 {
		toleranceKZT = 0
	}
	fileHash, err := fileSHA256(filePath)
	if err != nil {
		result.ValidationStatus = ReceiptStatusParseFailed
		result.ValidationErrors = append(result.ValidationErrors, "file_read_failed")
		return result, err
	}
	result.FileHash = fileHash

	rawText, _ := ExtractPDFText(filePath)
	rawText = normalizeReceiptText(rawText)
	result.RawText = rawText
	if rawText == "" {
		result.ValidationStatus = ReceiptStatusParsePartial
		result.ValidationErrors = append(result.ValidationErrors, "pdf_text_unreadable")
		return result, nil
	}

	rawHash := sha256.Sum256([]byte(strings.ToLower(rawText)))
	result.RawTextHash = hex.EncodeToString(rawHash[:])
	result.Provider = inferReceiptProvider(rawText, expectedProvider)
	result.ParsedAmountKZT = extractAmountKZT(rawText, expectedAmount)
	result.ParsedCurrency = extractCurrency(rawText)
	result.ParsedTransactionID = extractIdentityValue(rawText, []string{
		"transaction id", "transaction", "txn", "транзакция", "транзакции",
		"операция", "операции", "номер операции", "id операции",
	})
	result.ParsedCheckID = extractIdentityValue(rawText, []string{
		"check id", "receipt id", "receipt", "чек", "номер чека", "квитанция", "номер квитанции",
	})
	result.ParsedReferenceID = extractIdentityValue(rawText, []string{
		"reference id", "reference", "референс", "rrn", "referense",
	})
	result.ParsedPaymentDate = extractReceiptDate(rawText)
	result.ParsedPayerMasked = maskSensitiveID(extractLabelValue(rawText, []string{"плательщик", "төлеуші", "payer", "sender"}))
	result.ParsedRecipient = safeShortText(extractLabelValue(rawText, []string{"получатель", "recipient", "алушы", "кому", "merchant", "продавец"}), 120)
	result.ParsedRecipientBIN = extractRecipientBIN(rawText, result.ExpectedRecipientBIN)
	result.QRPayload = extractQRPayload(rawText)
	if result.QRPayload != "" {
		qrHash := sha256.Sum256([]byte(strings.ToLower(result.QRPayload)))
		result.QRPayloadHash = hex.EncodeToString(qrHash[:])
	}
	result = BuildReceiptIdentity(result)

	if result.ParsedAmountKZT == nil {
		result.ValidationErrors = append(result.ValidationErrors, "amount_not_found")
	} else {
		diff := *result.ParsedAmountKZT - expectedAmount
		result.AmountDifferenceKZT = &diff
		if expectedAmount > 0 && absInt(diff) > toleranceKZT {
			result.ValidationErrors = append(result.ValidationErrors, "amount_mismatch")
		}
	}
	if result.ExpectedRecipientBIN == "" {
		result.ValidationErrors = append(result.ValidationErrors, "recipient_bin_not_configured")
	} else if result.ParsedRecipientBIN == "" {
		result.ValidationErrors = append(result.ValidationErrors, "recipient_bin_missing")
	} else if result.ParsedRecipientBIN != result.ExpectedRecipientBIN {
		result.ValidationErrors = append(result.ValidationErrors, "recipient_bin_mismatch")
	}
	if !hasReceiptProviderMarker(rawText) {
		result.ValidationErrors = append(result.ValidationErrors, "provider_marker_missing")
	}
	if result.ParsedPaymentDate != nil && receiptDateClearlyBefore(*result.ParsedPaymentDate, paymentCreatedAt) {
		result.ValidationErrors = append(result.ValidationErrors, "payment_date_too_early")
	}
	if result.ReceiptTransactionKey == "" && result.ParsedCheckID == "" && result.QRPayloadHash == "" {
		result.ValidationErrors = append(result.ValidationErrors, "strong_identity_not_found")
	}

	result.ValidationStatus = receiptValidationStatus(result.ValidationErrors)
	_ = paymentID
	return result, nil
}

func receiptValidationStatus(errors []string) string {
	if len(errors) == 0 {
		return ReceiptStatusValidCandidate
	}
	for _, code := range errors {
		switch code {
		case "amount_mismatch", "recipient_bin_mismatch", "payment_date_too_early", "provider_marker_missing", "recipient_bin_not_configured":
			return ReceiptStatusSuspicious
		}
	}
	return ReceiptStatusParsePartial
}

func ExtractPDFText(filePath string) (string, error) {
	f, reader, err := pdf.Open(filePath)
	if err == nil {
		defer f.Close()
		textReader, textErr := reader.GetPlainText()
		if textErr == nil {
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(textReader)
			if strings.TrimSpace(buf.String()) != "" {
				return buf.String(), nil
			}
		}
	}
	rawText, rawErr := extractRawPrintableText(filePath)
	if strings.TrimSpace(rawText) != "" {
		return rawText, nil
	}
	if err != nil {
		return "", err
	}
	return "", rawErr
}

func ExtractImageText(filePath string) (string, error) {
	return "", nil
}

func DecodeQRCode(filePath string) (string, error) {
	return "", nil
}

func BuildReceiptIdentity(result ReceiptParseResult) ReceiptParseResult {
	if result.ReceiptTransactionKey == "" {
		for _, value := range []string{result.ParsedTransactionID, result.ParsedReferenceID, result.ParsedCheckID} {
			if key := normalizeReceiptIdentity(value); key != "" {
				result.ReceiptTransactionKey = key
				break
			}
		}
	}
	return result
}

func (s *Store) AttachReceipt(ctx context.Context, userID string, filePath, fileName, mimeType string, fileSize int64) (Payment, error) {
	payment, _, err := s.attachReceipt(ctx, userID, "", filePath, fileName, mimeType, fileSize, ReceiptValidationOptions{})
	return payment, err
}

func (s *Store) AttachReceiptWithValidation(ctx context.Context, userID string, filePath, fileName, mimeType string, fileSize int64, opts ReceiptValidationOptions) (Payment, Receipt, error) {
	return s.attachReceipt(ctx, userID, "", filePath, fileName, mimeType, fileSize, opts)
}

func (s *Store) AttachReceiptToPayment(ctx context.Context, userID, paymentID, filePath, fileName, mimeType string, fileSize int64) (Payment, Receipt, error) {
	return s.attachReceipt(ctx, userID, paymentID, filePath, fileName, mimeType, fileSize, ReceiptValidationOptions{})
}

func (s *Store) AttachReceiptToPaymentWithValidation(ctx context.Context, userID, paymentID, filePath, fileName, mimeType string, fileSize int64, opts ReceiptValidationOptions) (Payment, Receipt, error) {
	return s.attachReceipt(ctx, userID, paymentID, filePath, fileName, mimeType, fileSize, opts)
}

func (s *Store) attachReceipt(ctx context.Context, userID, paymentID, filePath, fileName, mimeType string, fileSize int64, opts ReceiptValidationOptions) (Payment, Receipt, error) {
	opts.ExpectedRecipientBIN = normalizeDigits(opts.ExpectedRecipientBIN)
	var payment Payment
	var receipt Receipt
	var finalErr error
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		query := paymentSelectSQL + `
			WHERE p.user_id = ? AND p.status IN ('pending','uploaded_receipt') AND (p.expires_at IS NULL OR p.expires_at > CURRENT_TIMESTAMP)`
		args := []any{userID}
		if paymentID != "" {
			query += ` AND p.id = ?`
			args = append(args, paymentID)
		} else {
			ambiguous, err := hasAmbiguousPendingPaymentTypes(ctx, tx, userID)
			if err != nil {
				return err
			}
			if ambiguous {
				return ErrAmbiguousPayment
			}
		}
		query += ` ORDER BY p.created_at DESC LIMIT 1;`
		found, err := scanPaymentRow(tx.QueryRowContext(ctx, query, args...))
		if err != nil {
			payment, receipt, finalErr = s.receiptNoActivePaymentError(ctx, tx, userID, paymentID, filePath)
			return finalErr
		}

		parsed, parseErr := parseReceiptPDFWithProvider(filePath, found.AmountKZT, found.CreatedAt, found.ID, opts.AmountToleranceKZT, opts.ExpectedRecipientBIN, found.Provider)
		if parseErr != nil && parsed.FileHash == "" {
			return parseErr
		}
		parsed = BuildReceiptIdentity(parsed)
		duplicateID, duplicateStrong, err := s.CheckReceiptDuplicate(ctx, tx, parsed, found.ID)
		if err != nil {
			return err
		}
		if duplicateID != nil {
			parsed.ValidationErrors = append(parsed.ValidationErrors, "duplicate_identity_found")
			if duplicateStrong {
				parsed.ValidationStatus = ReceiptStatusDuplicate
			} else if parsed.ValidationStatus == ReceiptStatusValidCandidate {
				parsed.ValidationStatus = ReceiptStatusSuspicious
			}
		}
		if parsed.ValidationStatus == "" {
			parsed.ValidationStatus = ReceiptStatusUploaded
		}
		errorsJSON, _ := json.Marshal(parsed.ValidationErrors)
		receiptID := newID()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO payment_receipts(
				id, payment_id, user_id, file_path, file_name, mime_type, file_size, status,
				file_hash, raw_text_hash, qr_payload_hash, provider, parsed_amount_kzt, expected_amount_kzt,
				amount_difference_kzt, parsed_currency, parsed_transaction_id, receipt_transaction_key,
				parsed_check_id, parsed_reference_id, parsed_payment_date, parsed_recipient, parsed_recipient_bin,
				expected_recipient_bin, parsed_payer_masked, validation_status, validation_errors, duplicate_of_receipt_id
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'uploaded', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
		`, receiptID, found.ID, userID, filePath, fileName, mimeType, fileSize,
			nullableString(parsed.FileHash), nullableString(parsed.RawTextHash), nullableString(parsed.QRPayloadHash), parsed.Provider,
			nullableInt(parsed.ParsedAmountKZT), nullablePositiveInt(parsed.ExpectedAmountKZT), nullableInt(parsed.AmountDifferenceKZT),
			nullableString(parsed.ParsedCurrency), nullableString(parsed.ParsedTransactionID), nullableString(parsed.ReceiptTransactionKey),
			nullableString(parsed.ParsedCheckID), nullableString(parsed.ParsedReferenceID), nullableTime(parsed.ParsedPaymentDate),
			nullableString(parsed.ParsedRecipient), nullableString(parsed.ParsedRecipientBIN), nullableString(parsed.ExpectedRecipientBIN),
			nullableString(parsed.ParsedPayerMasked), parsed.ValidationStatus, string(errorsJSON), nullableStringPtrValue(duplicateID)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE payments
			SET status = 'uploaded_receipt', receipt_file_path = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?;
		`, filePath, found.ID); err != nil {
			return err
		}

		if parsed.ValidationStatus == ReceiptStatusValidCandidate {
			approved, err := s.approvePaymentTx(ctx, tx, found.ID, AdminActor{ID: 0, Role: "system"}, opts.SubscriptionDays, "", false)
			if err != nil {
				return err
			}
			auditJSON, _ := json.Marshal(map[string]any{
				"receipt_id":              receiptID,
				"receipt_transaction_key": parsed.ReceiptTransactionKey,
				"file_hash":               parsed.FileHash,
			})
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO admin_actions(id, admin_id, role, action, entity_type, entity_id, metadata_json)
				VALUES (?, NULL, 'system', 'payment_auto_approve', 'payment', ?, ?);
			`, newID(), found.ID, string(auditJSON)); err != nil {
				return err
			}
			payment = approved
		} else {
			updated, err := scanPaymentRow(tx.QueryRowContext(ctx, paymentSelectSQL+` WHERE p.id = ?`, found.ID))
			if err != nil {
				return err
			}
			payment = updated
		}
		receipt, err = s.GetReceiptTx(ctx, tx, receiptID)
		return err
	})
	if finalErr != nil {
		return payment, receipt, finalErr
	}
	return payment, receipt, err
}

func hasAmbiguousPendingPaymentTypes(ctx context.Context, q queryer, userID string) (bool, error) {
	var count int
	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT payment_type)
		FROM payments
		WHERE user_id = ? AND status IN ('pending','uploaded_receipt')
			AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP);
	`, userID).Scan(&count); err != nil {
		return false, err
	}
	return count > 1, nil
}

func (s *Store) receiptNoActivePaymentError(ctx context.Context, tx *sql.Tx, userID, paymentID, filePath string) (Payment, Receipt, error) {
	fileHash, _ := fileSHA256(filePath)
	if fileHash != "" {
		receipt, err := scanReceiptRow(tx.QueryRowContext(ctx, receiptSelectSQL+`
			WHERE r.file_hash = ? AND r.validation_status = 'approved'
			ORDER BY r.created_at ASC
			LIMIT 1;
		`, fileHash))
		if err == nil {
			payment, payErr := scanPaymentRow(tx.QueryRowContext(ctx, paymentSelectSQL+` WHERE p.id = ?`, receipt.PaymentID))
			if payErr != nil {
				return Payment{}, receipt, payErr
			}
			if receipt.UserID == userID {
				return payment, receipt, ErrReceiptAlreadyApproved
			}
			return payment, receipt, ErrReceiptDuplicate
		}
		if err != ErrNotFound {
			return Payment{}, Receipt{}, err
		}
	}

	if paymentID != "" {
		payment, err := scanPaymentRow(tx.QueryRowContext(ctx, paymentSelectSQL+` WHERE p.id = ? AND p.user_id = ?`, paymentID, userID))
		if err != nil {
			return Payment{}, Receipt{}, err
		}
		return payment, Receipt{}, inactivePaymentError(payment)
	}

	payment, err := scanPaymentRow(tx.QueryRowContext(ctx, paymentSelectSQL+`
		WHERE p.user_id = ? AND p.status IN ('pending','uploaded_receipt','expired','cancelled','approved')
		ORDER BY p.created_at DESC
		LIMIT 1;
	`, userID))
	if err == nil {
		if (payment.Status == PaymentStatusPending || payment.Status == PaymentStatusUploadedReceipt) && payment.ExpiresAt != nil && !payment.ExpiresAt.After(nowUTC()) {
			return payment, Receipt{}, ErrPaymentExpired
		}
		if payment.Status == PaymentStatusExpired {
			return payment, Receipt{}, ErrPaymentExpired
		}
		if payment.Status == PaymentStatusCancelled {
			return payment, Receipt{}, ErrPaymentCancelled
		}
	}
	return Payment{}, Receipt{}, ErrNotFound
}

func inactivePaymentError(payment Payment) error {
	if payment.Status == PaymentStatusApproved {
		return ErrReceiptAlreadyApproved
	}
	if payment.Status == PaymentStatusExpired || ((payment.Status == PaymentStatusPending || payment.Status == PaymentStatusUploadedReceipt) && payment.ExpiresAt != nil && !payment.ExpiresAt.After(nowUTC())) {
		return ErrPaymentExpired
	}
	if payment.Status == PaymentStatusCancelled {
		return ErrPaymentCancelled
	}
	return ErrInvalidState
}

func (s *Store) CheckReceiptDuplicate(ctx context.Context, q queryer, parsed ReceiptParseResult, paymentID string) (*string, bool, error) {
	identities := []struct {
		column string
		value  string
		strong bool
	}{
		{"file_hash", parsed.FileHash, true},
		{"receipt_transaction_key", parsed.ReceiptTransactionKey, true},
		{"parsed_transaction_id", parsed.ParsedTransactionID, true},
		{"parsed_check_id", parsed.ParsedCheckID, true},
		{"qr_payload_hash", parsed.QRPayloadHash, true},
		{"raw_text_hash", parsed.RawTextHash, true},
	}
	for _, identity := range identities {
		if strings.TrimSpace(identity.value) == "" {
			continue
		}
		var id string
		err := q.QueryRowContext(ctx, `
			SELECT id
			FROM payment_receipts
			WHERE validation_status = 'approved' AND payment_id <> ? AND `+identity.column+` = ?
			ORDER BY created_at ASC
			LIMIT 1;
		`, paymentID, identity.value).Scan(&id)
		if err == nil {
			return &id, identity.strong, nil
		}
		if err != sql.ErrNoRows {
			return nil, false, err
		}
	}
	for _, identity := range identities {
		if strings.TrimSpace(identity.value) == "" {
			continue
		}
		var id string
		err := q.QueryRowContext(ctx, `
			SELECT id
			FROM payment_receipts
			WHERE payment_id <> ? AND `+identity.column+` = ?
			ORDER BY created_at ASC
			LIMIT 1;
		`, paymentID, identity.value).Scan(&id)
		if err == nil {
			return &id, identity.strong, nil
		}
		if err != sql.ErrNoRows {
			return nil, false, err
		}
	}
	return nil, false, nil
}

func (s *Store) GetReceipt(ctx context.Context, receiptID string) (Receipt, error) {
	receipt, err := s.GetReceiptTx(ctx, s.db, receiptID)
	return receipt, rowErr(err)
}

func (s *Store) GetReceiptTx(ctx context.Context, q queryer, receiptID string) (Receipt, error) {
	return scanReceiptRow(q.QueryRowContext(ctx, receiptSelectSQL+` WHERE r.id = ?`, receiptID))
}

func (s *Store) LatestReceiptForPayment(ctx context.Context, paymentID string) (*Receipt, error) {
	receipt, err := scanReceiptRow(s.db.QueryRowContext(ctx, receiptSelectSQL+`
		WHERE r.payment_id = ?
		ORDER BY r.created_at DESC
		LIMIT 1;
	`, paymentID))
	if err == ErrNotFound {
		return nil, nil
	}
	return &receipt, err
}

const receiptSelectSQL = `
	SELECT r.id, r.payment_id, r.user_id, r.file_path, COALESCE(r.file_name, ''), COALESCE(r.mime_type, ''), r.file_size,
		r.status, COALESCE(r.file_hash, ''), COALESCE(r.raw_text_hash, ''), COALESCE(r.qr_payload_hash, ''),
		COALESCE(r.provider, 'unknown'), r.parsed_amount_kzt, r.expected_amount_kzt, r.amount_difference_kzt,
		COALESCE(r.parsed_currency, ''), COALESCE(r.parsed_transaction_id, ''), COALESCE(r.receipt_transaction_key, ''),
		COALESCE(r.parsed_check_id, ''), COALESCE(r.parsed_reference_id, ''), r.parsed_payment_date,
		COALESCE(r.parsed_recipient, ''), COALESCE(r.parsed_recipient_bin, ''), COALESCE(r.expected_recipient_bin, ''),
		COALESCE(r.parsed_payer_masked, ''), COALESCE(r.validation_status, 'uploaded'), COALESCE(r.validation_errors, '[]'),
		r.duplicate_of_receipt_id, r.created_at
	FROM payment_receipts r`

func scanReceiptRow(row interface{ Scan(dest ...any) error }) (Receipt, error) {
	var receipt Receipt
	var amount, expectedAmount, amountDiff sql.NullInt64
	var paymentDate sql.NullTime
	var duplicate sql.NullString
	if err := row.Scan(&receipt.ID, &receipt.PaymentID, &receipt.UserID, &receipt.FilePath, &receipt.FileName, &receipt.MimeType, &receipt.FileSize,
		&receipt.Status, &receipt.FileHash, &receipt.RawTextHash, &receipt.QRPayloadHash, &receipt.Provider, &amount, &expectedAmount, &amountDiff,
		&receipt.ParsedCurrency, &receipt.ParsedTransactionID, &receipt.ReceiptTransactionKey, &receipt.ParsedCheckID,
		&receipt.ParsedReferenceID, &paymentDate, &receipt.ParsedRecipient, &receipt.ParsedRecipientBIN, &receipt.ExpectedRecipientBIN,
		&receipt.ParsedPayerMasked, &receipt.ValidationStatus, &receipt.ValidationErrorsJSON, &duplicate, &receipt.CreatedAt); err != nil {
		return Receipt{}, rowErr(err)
	}
	if amount.Valid {
		value := int(amount.Int64)
		receipt.ParsedAmountKZT = &value
	}
	if expectedAmount.Valid {
		value := int(expectedAmount.Int64)
		receipt.ExpectedAmountKZT = &value
	}
	if amountDiff.Valid {
		value := int(amountDiff.Int64)
		receipt.AmountDifferenceKZT = &value
	}
	receipt.ParsedPaymentDate = scanTime(paymentDate)
	receipt.DuplicateOfReceiptID = scanStringPtr(duplicate)
	_ = json.Unmarshal([]byte(receipt.ValidationErrorsJSON), &receipt.ValidationErrors)
	receipt.QRFound = receipt.QRPayloadHash != ""
	receipt.FileUnique = receipt.DuplicateOfReceiptID == nil || receipt.FileHash == ""
	receipt.QRUnique = receipt.DuplicateOfReceiptID == nil || receipt.QRPayloadHash == ""
	return receipt, nil
}

func fileSHA256(filePath string) (string, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func extractRawPrintableText(filePath string) (string, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, r := range string(raw) {
		if r == '\n' || r == '\r' || r == '\t' || (unicode.IsPrint(r) && r < 0xFFFD) {
			out.WriteRune(r)
			continue
		}
		out.WriteByte(' ')
	}
	return out.String(), nil
}

func normalizeReceiptText(raw string) string {
	raw = strings.ReplaceAll(raw, "\x00", " ")
	raw = strings.ReplaceAll(raw, "\u00a0", " ")
	raw = strings.ReplaceAll(raw, "\u202f", " ")
	return strings.Join(strings.Fields(raw), " ")
}

func inferReceiptProvider(rawText, expected string) string {
	lower := strings.ToLower(rawText)
	switch {
	case strings.Contains(lower, "kaspi qr"):
		return "kaspi_qr"
	case strings.Contains(lower, "kaspi pay"):
		return "kaspi_pay"
	case strings.Contains(lower, "kaspi") || strings.Contains(lower, "каспи"):
		return "kaspi"
	case strings.HasPrefix(expected, "kaspi"):
		return "unknown"
	case hasReceiptProviderMarker(rawText):
		return "payment"
	default:
		return "unknown"
	}
}

func hasReceiptProviderMarker(rawText string) bool {
	lower := strings.ToLower(rawText)
	markers := []string{"kaspi", "каспи", "төлем", "оплата", "payment", "чек", "квитанция", "receipt"}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func extractAmountKZT(raw string, expectedAmount int) *int {
	candidates := receiptAmountCandidates(raw)
	if len(candidates) == 0 {
		return nil
	}
	for _, value := range candidates {
		if expectedAmount > 0 && value == expectedAmount {
			v := value
			return &v
		}
	}
	best := 0
	for _, value := range candidates {
		if value > best {
			best = value
		}
	}
	if best == 0 {
		return nil
	}
	return &best
}

func receiptAmountCandidates(raw string) []int {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:сумма|сомасы|amount|итого|total|оплата|төлем|к оплате|барлығы)[^0-9]{0,40}([0-9][0-9\s.,\x{00a0}\x{202f}]*)(?:\s*(?:₸|тг|kzt|тенге))?`),
		regexp.MustCompile(`(?i)([0-9][0-9\s.,\x{00a0}\x{202f}]*)(?:\s*(?:₸|тг|kzt|тенге))`),
	}
	seen := map[int]bool{}
	var values []int
	for _, pattern := range patterns {
		for _, match := range pattern.FindAllStringSubmatch(raw, -1) {
			if len(match) < 2 {
				continue
			}
			value, ok := parseReceiptAmount(match[1])
			if !ok || value <= 0 || value > 100_000_000 || seen[value] {
				continue
			}
			seen[value] = true
			values = append(values, value)
		}
	}
	return values
}

func parseReceiptAmount(raw string) (int, bool) {
	value := strings.TrimSpace(raw)
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.ReplaceAll(value, "\u202f", " ")
	value = strings.Trim(value, " .,")
	if value == "" {
		return 0, false
	}
	lastComma := strings.LastIndex(value, ",")
	lastDot := strings.LastIndex(value, ".")
	decimalPos := -1
	if lastComma > lastDot {
		decimalPos = lastComma
	} else {
		decimalPos = lastDot
	}
	if decimalPos >= 0 {
		fractionDigits := digitsOnlyString(value[decimalPos+1:])
		if len(fractionDigits) == 2 {
			value = value[:decimalPos]
		}
	}
	digits := digitsOnlyString(value)
	if digits == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(digits)
	return parsed, err == nil
}

func extractCurrency(raw string) string {
	if regexp.MustCompile(`(?i)(₸|тг|kzt|тенге)`).MatchString(raw) {
		return "KZT"
	}
	return ""
}

func extractIdentityValue(raw string, labels []string) string {
	for _, label := range labels {
		re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(label) + `(?:\s*(?:id|№|номер|нөмірі))?\s*[:№#-]?\s*([A-Za-zА-Яа-я0-9][A-Za-zА-Яа-я0-9._/\-]{3,80})`)
		if match := re.FindStringSubmatch(raw); len(match) > 1 {
			return safeShortText(strings.TrimSpace(match[1]), 80)
		}
	}
	return ""
}

func extractLabelValue(raw string, labels []string) string {
	for _, label := range labels {
		re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(label) + `\s*[:№#-]?\s*([A-Za-zА-Яа-я0-9._/\- ]{4,80})`)
		if match := re.FindStringSubmatch(raw); len(match) > 1 {
			return safeShortText(strings.TrimSpace(match[1]), 80)
		}
	}
	return ""
}

func extractRecipientBIN(raw, expected string) string {
	expected = normalizeDigits(expected)
	contextPattern := regexp.MustCompile(`(?i)(?:получатель|recipient|алушы|merchant|продавец|компания|company|поставщик|кому)[^0-9]{0,120}(?:иин\s*/\s*бин|iin\s*/\s*bin|бин|bin|бсн|иин|iin|инн)[^0-9]{0,20}([0-9][0-9\s.\-]{10,20})`)
	if match := contextPattern.FindStringSubmatch(raw); len(match) > 1 {
		if digits := normalizeDigits(match[1]); len(digits) == 12 {
			return digits
		}
	}

	labelPattern := regexp.MustCompile(`(?i)(?:иин\s*/\s*бин|iin\s*/\s*bin|бин|bin|бсн|иин|iin|инн)[^0-9]{0,20}([0-9][0-9\s.\-]{10,20})`)
	matches := labelPattern.FindAllStringSubmatchIndex(raw, -1)
	var first string
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		start := match[0] - 80
		if start < 0 {
			start = 0
		}
		prefix := strings.ToLower(raw[start:match[0]])
		digits := normalizeDigits(raw[match[2]:match[3]])
		if len(digits) != 12 {
			continue
		}
		if strings.Contains(prefix, "плательщик") || strings.Contains(prefix, "төлеуші") || strings.Contains(prefix, "payer") || strings.Contains(prefix, "sender") || strings.Contains(prefix, "отправитель") {
			continue
		}
		if expected != "" && digits == expected {
			return digits
		}
		if first == "" {
			first = digits
		}
	}
	if first != "" {
		return first
	}
	return ""
}

func extractQRPayload(raw string) string {
	for _, label := range []string{"qr payload", "qr-code", "qr code", "qr", "qr код", "код qr"} {
		re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(label) + `\s*[:№#-]?\s*([A-Za-z0-9:/?&=._%+\-]{8,300})`)
		if match := re.FindStringSubmatch(raw); len(match) > 1 {
			return strings.TrimSpace(match[1])
		}
	}
	urlRe := regexp.MustCompile(`(?i)https?://[^\s]+`)
	for _, value := range urlRe.FindAllString(raw, -1) {
		lower := strings.ToLower(value)
		if strings.Contains(lower, "qr") || strings.Contains(lower, "receipt") || strings.Contains(lower, "check") || strings.Contains(lower, "kaspi") {
			return strings.TrimRight(value, ".,;)")
		}
	}
	return ""
}

func extractReceiptDate(raw string) *time.Time {
	patterns := []struct {
		re      *regexp.Regexp
		layouts []string
	}{
		{
			re:      regexp.MustCompile(`\b\d{2}[.]\d{2}[.]\d{4}(?:[ T]\d{2}:\d{2}(?::\d{2})?)?\b`),
			layouts: []string{"02.01.2006 15:04:05", "02.01.2006 15:04", "02.01.2006"},
		},
		{
			re:      regexp.MustCompile(`\b\d{2}/\d{2}/\d{4}(?:[ T]\d{2}:\d{2}(?::\d{2})?)?\b`),
			layouts: []string{"02/01/2006 15:04:05", "02/01/2006 15:04", "02/01/2006"},
		},
		{
			re:      regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}(?:[ T]\d{2}:\d{2}(?::\d{2})?)?\b`),
			layouts: []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"},
		},
	}
	for _, pattern := range patterns {
		value := pattern.re.FindString(raw)
		if value == "" {
			continue
		}
		for _, layout := range pattern.layouts {
			if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
				return &parsed
			}
		}
	}
	return nil
}

func receiptDateClearlyBefore(paidAt, createdAt time.Time) bool {
	paidAt = paidAt.UTC()
	createdAt = createdAt.UTC()
	if paidAt.Hour() == 0 && paidAt.Minute() == 0 && paidAt.Second() == 0 && sameDate(paidAt, createdAt) {
		return false
	}
	return paidAt.Before(createdAt.Add(-receiptDateTolerance))
}

func sameDate(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func normalizeReceiptIdentity(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'А' && r <= 'Я') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func maskSensitiveID(value string) string {
	re := regexp.MustCompile(`\b(\d{6})(\d{6})\b`)
	return re.ReplaceAllString(value, "$1******")
}

func safeShortText(value string, max int) string {
	value = maskSensitiveID(strings.TrimSpace(value))
	if len([]rune(value)) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullablePositiveInt(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func normalizeDigits(value string) string {
	return digitsOnlyString(value)
}

func digitsOnlyString(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
