package handler

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"zhenis-orda-service/internal/repository"
)

var youtubeVideoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func (s *Server) handleFreeLessons(w http.ResponseWriter, r *http.Request) {
	lessons, err := s.store.ListFreeLessons(r.Context(), true)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"free_lessons": lessons})
}

func (s *Server) handleFreeLesson(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad free lesson")
		return
	}
	lesson, err := s.store.GetFreeLesson(r.Context(), id, true)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"free_lesson": lesson})
}

func (s *Server) handleAdminFreeLessons(w http.ResponseWriter, r *http.Request) {
	lessons, err := s.store.ListFreeLessons(r.Context(), false)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"free_lessons": lessons})
}

func (s *Server) handleAdminFreeLesson(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	lesson, err := s.store.GetFreeLesson(r.Context(), id, false)
	if mapRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"free_lesson": lesson})
}

func (s *Server) handleAdminPostFreeLesson(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	var req struct {
		ID               string `json:"id"`
		Title            string `json:"title"`
		ShortDescription string `json:"short_description"`
		Description      string `json:"description"`
		ImageURL         string `json:"image_url"`
		ImageFilePath    string `json:"image_file_path"`
		ImageSource      string `json:"image_source"`
		YouTubeURL       string `json:"youtube_url"`
		SortOrder        int    `json:"sort_order"`
		IsActive         *bool  `json:"is_active"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if raw := r.PathValue("id"); raw != "" {
		if !repository.IsUUID(raw) {
			writeError(w, http.StatusBadRequest, "bad id")
			return
		}
		req.ID = raw
	}
	req.ImageURL = strings.TrimSpace(req.ImageURL)
	req.ImageFilePath = strings.TrimSpace(req.ImageFilePath)
	if req.ImageURL != "" && !isHTTPURL(req.ImageURL) {
		writeError(w, http.StatusBadRequest, "invalid image url")
		return
	}
	if req.ImageFilePath != "" && !strings.HasPrefix(req.ImageFilePath, "/uploads/free-lessons/") {
		writeError(w, http.StatusBadRequest, "invalid uploaded image path")
		return
	}
	videoID, embedURL, err := parseYouTubeURL(req.YouTubeURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid youtube link")
		return
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	lesson := repository.FreeLesson{
		ID:               req.ID,
		Title:            req.Title,
		ShortDescription: req.ShortDescription,
		Description:      req.Description,
		ImageURL:         req.ImageURL,
		ImageFilePath:    req.ImageFilePath,
		ImageSource:      req.ImageSource,
		YouTubeURL:       strings.TrimSpace(req.YouTubeURL),
		YouTubeVideoID:   videoID,
		YouTubeEmbedURL:  embedURL,
		SortOrder:        req.SortOrder,
		IsActive:         active,
	}
	out, err := s.store.UpsertFreeLesson(r.Context(), lesson)
	if mapRepoError(w, err) {
		return
	}
	action := "free_lesson_create"
	if r.Method == http.MethodPatch {
		action = "free_lesson_update"
	}
	_ = s.store.Audit(r.Context(), actor, action, "free_lesson", out.ID, out)
	writeJSON(w, http.StatusOK, map[string]any{"free_lesson": out})
}

func (s *Server) handleAdminArchiveFreeLesson(w http.ResponseWriter, r *http.Request) {
	actor := adminFromContext(r.Context())
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.store.ArchiveFreeLesson(r.Context(), id); mapRepoError(w, err) {
		return
	}
	_ = s.store.Audit(r.Context(), actor, "free_lesson_archive", "free_lesson", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminFreeLessonImageUpload(w http.ResponseWriter, r *http.Request) {
	maxBytes := s.cfg.MaxFreeLessonImageBytes
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024)
	if err := r.ParseMultipartForm(maxBytes + 1024); err != nil {
		writeError(w, http.StatusBadRequest, "image file is too large or invalid")
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "image file is required")
		return
	}
	defer file.Close()

	fileName := filepath.Base(header.Filename)
	if ext := strings.ToLower(filepath.Ext(fileName)); ext != "" && !allowedBookImageExt(ext) {
		writeError(w, http.StatusBadRequest, "unsupported image file type")
		return
	}
	mimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if mimeType != "" && !allowedFreeLessonImageMIME(mimeType) && !strings.EqualFold(mimeType, "application/octet-stream") {
		writeError(w, http.StatusBadRequest, "unsupported image file type")
		return
	}
	head := make([]byte, 512)
	n, readErr := file.Read(head)
	if readErr != nil && readErr != io.EOF {
		writeError(w, http.StatusBadRequest, "image file is invalid")
		return
	}
	ext, ok := detectedBookImageExt(head[:n])
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported image file type")
		return
	}

	now := time.Now()
	dir := filepath.Join(s.cfg.FreeLessonUploadDir, now.Format("2006"), now.Format("01"))
	if err := ensureUploadDir(dir); err != nil {
		writeError(w, http.StatusInternalServerError, "image directory error")
		return
	}
	path := filepath.Join(dir, uuid.NewString()+ext)
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "image save error")
		return
	}
	limited := io.LimitReader(io.MultiReader(bytes.NewReader(head[:n]), file), maxBytes+1)
	written, copyErr := io.Copy(out, limited)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		writeError(w, http.StatusInternalServerError, "image save error")
		return
	}
	if written > maxBytes {
		_ = os.Remove(path)
		writeError(w, http.StatusBadRequest, "image file is too large")
		return
	}
	publicPath := safePublicFreeLessonUploadPath(s.cfg.FreeLessonUploadDir, path)
	if publicPath == "" {
		_ = os.Remove(path)
		writeError(w, http.StatusInternalServerError, "image path error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"image_file_path": publicPath,
		"image_source":    "uploaded",
	})
}

func allowedFreeLessonImageMIME(value string) bool {
	mimeType := strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	switch mimeType {
	case "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func parseYouTubeURL(value string) (string, string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", "", fmt.Errorf("empty youtube url")
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("bad youtube url")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", "", fmt.Errorf("bad youtube scheme")
	}
	host := strings.ToLower(parsed.Hostname())
	var videoID string
	switch {
	case host == "youtu.be":
		videoID = firstPathSegment(parsed.Path)
	case host == "youtube.com" || strings.HasSuffix(host, ".youtube.com"):
		parts := pathSegments(parsed.Path)
		if len(parts) > 0 {
			switch parts[0] {
			case "watch":
				videoID = parsed.Query().Get("v")
			case "shorts", "embed":
				if len(parts) > 1 {
					videoID = parts[1]
				}
			}
		}
	}
	videoID = strings.TrimSpace(videoID)
	if !youtubeVideoIDPattern.MatchString(videoID) {
		return "", "", fmt.Errorf("bad youtube video id")
	}
	return videoID, "https://www.youtube.com/embed/" + videoID, nil
}

func firstPathSegment(pathValue string) string {
	parts := pathSegments(pathValue)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func pathSegments(pathValue string) []string {
	rawParts := strings.Split(strings.Trim(pathValue, "/"), "/")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		if part == "" {
			continue
		}
		if decoded, err := url.PathUnescape(part); err == nil {
			part = decoded
		}
		parts = append(parts, part)
	}
	return parts
}
