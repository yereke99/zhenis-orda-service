package repository

import (
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
)

type ReceiptParseResult struct {
	FileHash            string
	RawTextHash         string
	QRPayloadHash       string
	Provider            string
	ParsedAmountKZT     *int
	ParsedCurrency      string
	ParsedTransactionID string
	ParsedCheckID       string
	ParsedReferenceID   string
	ParsedPaymentDate   *time.Time
	ParsedRecipient     string
	ParsedPayerMasked   string
	ValidationStatus    string
	ValidationErrors    []string
}

func ParseReceipt(filePath string, expectedAmount int, paymentCreatedAt time.Time, expectedProvider string) (ReceiptParseResult, error) {
	result := ReceiptParseResult{
		Provider:         "unknown",
		ValidationStatus: ReceiptStatusUploaded,
		ValidationErrors: []string{},
	}
	fileHash, err := fileSHA256(filePath)
	if err != nil {
		result.ValidationStatus = ReceiptStatusParseFailed
		result.ValidationErrors = append(result.ValidationErrors, "file_read_failed")
		return result, err
	}
	result.FileHash = fileHash

	rawText := ""
	if strings.HasSuffix(strings.ToLower(filePath), ".pdf") {
		rawText, _ = ExtractPDFText(filePath)
	} else {
		rawText, _ = ExtractImageText(filePath)
	}
	rawText = normalizeReceiptText(rawText)
	if rawText == "" {
		result.ValidationStatus = ReceiptStatusParsePartial
		result.ValidationErrors = append(result.ValidationErrors, "ocr_required_or_text_unreadable", "qr_not_found")
		return result, nil
	}

	rawHash := sha256.Sum256([]byte(strings.ToLower(rawText)))
	result.RawTextHash = hex.EncodeToString(rawHash[:])
	result.Provider = inferReceiptProvider(rawText, expectedProvider)
	result.ParsedAmountKZT = extractAmountKZT(rawText)
	result.ParsedCurrency = extractCurrency(rawText)
	result.ParsedTransactionID = extractLabelValue(rawText, []string{"transaction", "транзакция", "транзакции", "операция", "операции"})
	result.ParsedCheckID = extractLabelValue(rawText, []string{"чек", "receipt", "check", "квитанция"})
	result.ParsedReferenceID = extractLabelValue(rawText, []string{"reference", "референс", "rrn", "reference id"})
	result.ParsedPaymentDate = extractReceiptDate(rawText)
	result.ParsedPayerMasked = maskSensitiveID(extractLabelValue(rawText, []string{"плательщик", "төлеуші", "payer"}))
	result.ParsedRecipient = safeShortText(extractLabelValue(rawText, []string{"получатель", "recipient", "алушы", "кому"}), 120)

	if result.ParsedAmountKZT == nil {
		result.ValidationErrors = append(result.ValidationErrors, "amount_not_found")
	} else if expectedAmount > 0 && *result.ParsedAmountKZT != expectedAmount {
		result.ValidationErrors = append(result.ValidationErrors, "amount_mismatch")
	}
	if result.ParsedPaymentDate != nil && result.ParsedPaymentDate.Before(paymentCreatedAt.Add(-24*time.Hour)) {
		result.ValidationErrors = append(result.ValidationErrors, "payment_date_too_early")
	}
	if strings.HasPrefix(expectedProvider, "kaspi") && result.Provider == "unknown" {
		result.ValidationErrors = append(result.ValidationErrors, "kaspi_marker_not_found")
	}
	if result.ParsedTransactionID == "" && result.ParsedCheckID == "" && result.QRPayloadHash == "" {
		result.ValidationErrors = append(result.ValidationErrors, "strong_identity_not_found")
	}
	if result.QRPayloadHash == "" {
		result.ValidationErrors = append(result.ValidationErrors, "qr_not_found")
	}

	result.ValidationStatus = ReceiptStatusValidCandidate
	for _, code := range result.ValidationErrors {
		if code == "amount_mismatch" || code == "payment_date_too_early" || code == "kaspi_marker_not_found" {
			result.ValidationStatus = ReceiptStatusSuspicious
			return result, nil
		}
	}
	if len(result.ValidationErrors) > 0 {
		result.ValidationStatus = ReceiptStatusParsePartial
	}
	return result, nil
}

func ExtractPDFText(filePath string) (string, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, r := range string(raw) {
		if r == '\n' || r == '\r' || r == '\t' || (unicode.IsPrint(r) && r < 0xFFFD) {
			out.WriteRune(r)
		}
	}
	return out.String(), nil
}

func ExtractImageText(filePath string) (string, error) {
	// OCR is intentionally optional in this MVP. The original image is stored and
	// the receipt is flagged for manual review when text cannot be extracted.
	return "", nil
}

func DecodeQRCode(filePath string) (string, error) {
	return "", nil
}

func BuildReceiptIdentity(result ReceiptParseResult) ReceiptParseResult {
	return result
}

func (s *Store) AttachReceipt(ctx context.Context, userID string, filePath, fileName, mimeType string, fileSize int64) (Payment, error) {
	payment, _, err := s.attachReceipt(ctx, userID, "", filePath, fileName, mimeType, fileSize)
	return payment, err
}

func (s *Store) AttachReceiptToPayment(ctx context.Context, userID, paymentID, filePath, fileName, mimeType string, fileSize int64) (Payment, Receipt, error) {
	return s.attachReceipt(ctx, userID, paymentID, filePath, fileName, mimeType, fileSize)
}

func (s *Store) attachReceipt(ctx context.Context, userID, paymentID, filePath, fileName, mimeType string, fileSize int64) (Payment, Receipt, error) {
	var payment Payment
	var receipt Receipt
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		query := paymentSelectSQL + `
			WHERE p.user_id = ? AND p.status IN ('pending','uploaded_receipt') AND (p.expires_at IS NULL OR p.expires_at > CURRENT_TIMESTAMP)`
		args := []any{userID}
		if paymentID != "" {
			query += ` AND p.id = ?`
			args = append(args, paymentID)
		}
		query += ` ORDER BY p.created_at DESC LIMIT 1;`
		found, err := scanPaymentRow(tx.QueryRowContext(ctx, query, args...))
		if err != nil {
			return rowErr(err)
		}

		parsed, parseErr := ParseReceipt(filePath, found.AmountKZT, found.CreatedAt, found.Provider)
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
				file_hash, raw_text_hash, qr_payload_hash, provider, parsed_amount_kzt, parsed_currency,
				parsed_transaction_id, parsed_check_id, parsed_reference_id, parsed_payment_date,
				parsed_recipient, parsed_payer_masked, validation_status, validation_errors, duplicate_of_receipt_id
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'uploaded', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
		`, receiptID, found.ID, userID, filePath, fileName, mimeType, fileSize,
			nullableString(parsed.FileHash), nullableString(parsed.RawTextHash), nullableString(parsed.QRPayloadHash), parsed.Provider,
			nullableInt(parsed.ParsedAmountKZT), nullableString(parsed.ParsedCurrency), nullableString(parsed.ParsedTransactionID),
			nullableString(parsed.ParsedCheckID), nullableString(parsed.ParsedReferenceID), nullableTime(parsed.ParsedPaymentDate),
			nullableString(parsed.ParsedRecipient), nullableString(parsed.ParsedPayerMasked), parsed.ValidationStatus, string(errorsJSON),
			nullableStringPtrValue(duplicateID)); err != nil {
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
		receipt, err = s.GetReceiptTx(ctx, tx, receiptID)
		return err
	})
	return payment, receipt, err
}

func (s *Store) CheckReceiptDuplicate(ctx context.Context, q queryer, parsed ReceiptParseResult, paymentID string) (*string, bool, error) {
	identities := []struct {
		column string
		value  string
		strong bool
	}{
		{"file_hash", parsed.FileHash, true},
		{"qr_payload_hash", parsed.QRPayloadHash, true},
		{"raw_text_hash", parsed.RawTextHash, true},
		{"parsed_transaction_id", parsed.ParsedTransactionID, true},
		{"parsed_check_id", parsed.ParsedCheckID, true},
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
			return &id, false, nil
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
		COALESCE(r.provider, 'unknown'), r.parsed_amount_kzt, COALESCE(r.parsed_currency, ''),
		COALESCE(r.parsed_transaction_id, ''), COALESCE(r.parsed_check_id, ''), COALESCE(r.parsed_reference_id, ''),
		r.parsed_payment_date, COALESCE(r.parsed_recipient, ''), COALESCE(r.parsed_payer_masked, ''),
		COALESCE(r.validation_status, 'uploaded'), COALESCE(r.validation_errors, '[]'), r.duplicate_of_receipt_id, r.created_at
	FROM payment_receipts r`

func scanReceiptRow(row interface{ Scan(dest ...any) error }) (Receipt, error) {
	var receipt Receipt
	var amount sql.NullInt64
	var paymentDate sql.NullTime
	var duplicate sql.NullString
	if err := row.Scan(&receipt.ID, &receipt.PaymentID, &receipt.UserID, &receipt.FilePath, &receipt.FileName, &receipt.MimeType, &receipt.FileSize,
		&receipt.Status, &receipt.FileHash, &receipt.RawTextHash, &receipt.QRPayloadHash, &receipt.Provider, &amount, &receipt.ParsedCurrency,
		&receipt.ParsedTransactionID, &receipt.ParsedCheckID, &receipt.ParsedReferenceID, &paymentDate, &receipt.ParsedRecipient,
		&receipt.ParsedPayerMasked, &receipt.ValidationStatus, &receipt.ValidationErrorsJSON, &duplicate, &receipt.CreatedAt); err != nil {
		return Receipt{}, rowErr(err)
	}
	if amount.Valid {
		value := int(amount.Int64)
		receipt.ParsedAmountKZT = &value
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

func normalizeReceiptText(raw string) string {
	raw = strings.ReplaceAll(raw, "\x00", " ")
	return strings.Join(strings.Fields(raw), " ")
}

func inferReceiptProvider(rawText, expected string) string {
	lower := strings.ToLower(rawText)
	switch {
	case strings.Contains(lower, "kaspi qr"):
		return "kaspi_qr"
	case strings.Contains(lower, "kaspi pay"):
		return "kaspi_pay"
	case strings.Contains(lower, "kaspi"):
		return "kaspi"
	case strings.HasPrefix(expected, "kaspi"):
		return "unknown"
	default:
		return "unknown"
	}
}

func extractAmountKZT(raw string) *int {
	re := regexp.MustCompile(`(?i)([0-9][0-9\s.,]{2,})\s*(₸|тг|kzt|тенге)`)
	matches := re.FindAllStringSubmatch(raw, -1)
	best := 0
	for _, match := range matches {
		digits := regexp.MustCompile(`\D`).ReplaceAllString(match[1], "")
		if digits == "" {
			continue
		}
		value, err := strconv.Atoi(digits)
		if err == nil && value > best {
			best = value
		}
	}
	if best == 0 {
		return nil
	}
	return &best
}

func extractCurrency(raw string) string {
	if regexp.MustCompile(`(?i)(₸|тг|kzt|тенге)`).MatchString(raw) {
		return "KZT"
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

func extractReceiptDate(raw string) *time.Time {
	re := regexp.MustCompile(`(\d{2})[./-](\d{2})[./-](\d{4})(?:\s+(\d{2}):(\d{2}))?`)
	match := re.FindStringSubmatch(raw)
	if len(match) == 0 {
		return nil
	}
	layout := "02.01.2006"
	value := match[0]
	if len(match) > 5 && match[4] != "" {
		layout = "02.01.2006 15:04"
	}
	value = strings.ReplaceAll(strings.ReplaceAll(value, "/", "."), "-", ".")
	parsed, err := time.ParseInLocation(layout, value, time.Local)
	if err != nil {
		return nil
	}
	return &parsed
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

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}
