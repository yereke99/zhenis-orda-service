package handler

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"hash"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"zhenis-orda-service/internal/repository"
)

const legalDocumentType = "privacy_policy_offer"

var legalURLPattern = regexp.MustCompile(`(?i)\b(?:https?://|www\.|t\.me/|telegram\.me/)\S+`)

type legalDocumentMeta struct {
	DocumentType string
	Version      string
	Hash         string
	Paths        map[string]string
}

type legalDocument struct {
	Language    string
	Title       string
	ContentHTML string
	Meta        legalDocumentMeta
}

func (s *Server) handleLegalAgreementStatus(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	meta, err := loadLegalDocumentMeta()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "legal document unavailable")
		return
	}
	agreement, err := s.store.GetUserLegalAgreement(r.Context(), user.ID, meta.DocumentType, meta.Version)
	if mapRepoError(w, err) {
		return
	}
	resp := map[string]any{
		"accepted":         agreement != nil,
		"document_type":    meta.DocumentType,
		"document_version": meta.Version,
	}
	if agreement != nil {
		resp["accepted_at"] = agreement.AcceptedAt
		resp["document_language"] = agreement.DocumentLanguage
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleLegalDocument(w http.ResponseWriter, r *http.Request) {
	language, ok := normalizeLegalLanguage(r.URL.Query().Get("lang"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid language")
		return
	}
	document, err := loadLegalDocument(language)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "legal document unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"language":         document.Language,
		"document_type":    document.Meta.DocumentType,
		"document_version": document.Meta.Version,
		"title":            document.Title,
		"content_html":     document.ContentHTML,
	})
}

func (s *Server) handleAcceptLegalAgreement(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	var req struct {
		Language        string `json:"language"`
		DocumentType    string `json:"document_type"`
		DocumentVersion string `json:"document_version"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	language, ok := normalizeLegalLanguage(req.Language)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid language")
		return
	}
	meta, err := loadLegalDocumentMeta()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "legal document unavailable")
		return
	}
	if strings.TrimSpace(req.DocumentType) != "" && strings.TrimSpace(req.DocumentType) != meta.DocumentType {
		writeError(w, http.StatusBadRequest, "invalid document type")
		return
	}
	if strings.TrimSpace(req.DocumentVersion) != meta.Version {
		writeError(w, http.StatusConflict, "document version changed")
		return
	}
	agreement, err := s.store.AcceptUserLegalAgreement(r.Context(), repository.UserLegalAgreement{
		UserID:           user.ID,
		TelegramID:       user.TelegramID,
		DocumentType:     meta.DocumentType,
		DocumentVersion:  meta.Version,
		DocumentLanguage: language,
		DocumentHash:     meta.Hash,
	})
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accepted":          true,
		"document_type":     agreement.DocumentType,
		"document_version":  agreement.DocumentVersion,
		"document_language": agreement.DocumentLanguage,
		"accepted_at":       agreement.AcceptedAt,
	})
}

func (s *Server) hasAcceptedLatestLegalAgreement(r *http.Request, user repository.User) (legalDocumentMeta, bool, error) {
	meta, err := loadLegalDocumentMeta()
	if err != nil {
		return legalDocumentMeta{}, false, err
	}
	agreement, err := s.store.GetUserLegalAgreement(r.Context(), user.ID, meta.DocumentType, meta.Version)
	if err != nil {
		return legalDocumentMeta{}, false, err
	}
	return meta, agreement != nil, nil
}

func writeLegalAgreementRequired(w http.ResponseWriter, meta legalDocumentMeta) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error":            "LEGAL_AGREEMENT_REQUIRED",
		"message":          "Agreement acceptance required",
		"document_type":    meta.DocumentType,
		"document_version": meta.Version,
	})
}

func normalizeLegalLanguage(language string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "", "kk", "kz", "kaz":
		return "kk", true
	case "ru", "rus":
		return "ru", true
	default:
		return "", false
	}
}

func loadLegalDocument(language string) (legalDocument, error) {
	meta, err := loadLegalDocumentMeta()
	if err != nil {
		return legalDocument{}, err
	}
	path := meta.Paths[language]
	if path == "" {
		return legalDocument{}, fmt.Errorf("legal document not found")
	}
	paragraphs, err := extractDocxParagraphs(path)
	if err != nil {
		return legalDocument{}, err
	}
	title := legalDocumentTitle(language)
	return legalDocument{
		Language:    language,
		Title:       title,
		ContentHTML: legalParagraphsHTML(paragraphs),
		Meta:        meta,
	}, nil
}

func loadLegalDocumentMeta() (legalDocumentMeta, error) {
	dir, err := legalDocumentDir()
	if err != nil {
		return legalDocumentMeta{}, err
	}
	paths, err := legalDocumentPaths(dir)
	if err != nil {
		return legalDocumentMeta{}, err
	}
	h := sha256.New()
	for _, language := range []string{"kk", "ru"} {
		if err := addLegalDocumentHash(h, language, paths[language]); err != nil {
			return legalDocumentMeta{}, err
		}
	}
	sum := hex.EncodeToString(h.Sum(nil))
	version := sum
	if len(version) > 16 {
		version = version[:16]
	}
	return legalDocumentMeta{DocumentType: legalDocumentType, Version: version, Hash: sum, Paths: paths}, nil
}

func legalDocumentDir() (string, error) {
	candidates := []string{
		"privacy_policy",
		filepath.Join("..", "privacy_policy"),
		filepath.Join("..", "..", "privacy_policy"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("privacy_policy directory not found")
}

func legalDocumentPaths(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	paths := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if !strings.HasSuffix(name, ".docx") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		switch {
		case isLegalLanguageFile(name, "kk"):
			paths["kk"] = path
		case isLegalLanguageFile(name, "ru"):
			paths["ru"] = path
		}
	}
	if paths["kk"] == "" || paths["ru"] == "" {
		return nil, fmt.Errorf("legal documents are incomplete")
	}
	return paths, nil
}

func isLegalLanguageFile(name, language string) bool {
	switch language {
	case "kk":
		return strings.Contains(name, "_kk") || strings.Contains(name, "-kk") || strings.Contains(name, "kaz")
	case "ru":
		return strings.Contains(name, "_ru") || strings.Contains(name, "-ru") || strings.Contains(name, "rus")
	default:
		return false
	}
}

func addLegalDocumentHash(h hash.Hash, language, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	_, _ = h.Write([]byte(language))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(data)
	_, _ = h.Write([]byte{0})
	return nil
}

func extractDocxParagraphs(path string) ([]string, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return parseDocxDocumentXML(rc)
	}
	return nil, fmt.Errorf("document body not found")
}

func parseDocxDocumentXML(r io.Reader) ([]string, error) {
	decoder := xml.NewDecoder(r)
	var paragraphs []string
	var builder strings.Builder
	inParagraph := false
	inText := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				inParagraph = true
				builder.Reset()
			case "t":
				inText = true
			case "tab":
				if inParagraph {
					builder.WriteByte(' ')
				}
			case "br", "cr":
				if inParagraph {
					builder.WriteByte('\n')
				}
			}
		case xml.CharData:
			if inParagraph && inText {
				builder.Write([]byte(t))
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				if paragraph := cleanLegalParagraph(builder.String()); paragraph != "" {
					paragraphs = append(paragraphs, paragraph)
				}
				inParagraph = false
				inText = false
				builder.Reset()
			}
		}
	}
	if len(paragraphs) == 0 {
		return nil, fmt.Errorf("document is empty")
	}
	return paragraphs, nil
}

func cleanLegalParagraph(value string) string {
	value = strings.ReplaceAll(value, "\u00a0", " ")
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = strings.Join(strings.Fields(legalURLPattern.ReplaceAllString(lines[i], "")), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func legalParagraphsHTML(paragraphs []string) string {
	var b strings.Builder
	for _, paragraph := range paragraphs {
		escaped := html.EscapeString(paragraph)
		escaped = strings.ReplaceAll(escaped, "\n", "<br>")
		tag := "p"
		class := ""
		if isLegalHeading(paragraph) {
			tag = "h3"
		} else if isLegalListItem(paragraph) {
			class = ` class="legal-list-item"`
		}
		b.WriteString("<")
		b.WriteString(tag)
		b.WriteString(class)
		b.WriteString(">")
		b.WriteString(escaped)
		b.WriteString("</")
		b.WriteString(tag)
		b.WriteString(">")
	}
	return b.String()
}

func isLegalHeading(paragraph string) bool {
	runes := []rune(paragraph)
	if len(runes) == 0 || len(runes) > 120 {
		return false
	}
	if strings.HasSuffix(paragraph, ":") {
		return true
	}
	lower := strings.ToLower(paragraph)
	return strings.Contains(lower, "құпиялық") ||
		strings.Contains(lower, "оферта") ||
		strings.Contains(lower, "политика") ||
		strings.Contains(lower, "раздел") ||
		strings.Contains(lower, "тарау")
}

func isLegalListItem(paragraph string) bool {
	trimmed := strings.TrimSpace(paragraph)
	if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "•") {
		return true
	}
	if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' && (trimmed[1] == '.' || trimmed[1] == ')') {
		return true
	}
	return false
}

func legalDocumentTitle(language string) string {
	if language == "ru" {
		return "Политика конфиденциальности и оферта"
	}
	return "Құпиялық саясаты және оферта"
}
