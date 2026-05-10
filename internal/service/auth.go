package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidInitData = errors.New("invalid telegram init data")

type TelegramInitUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	Language  string `json:"language_code"`
	PhotoURL  string `json:"photo_url"`
}

type TelegramInitData struct {
	User       TelegramInitUser
	QueryID    string
	AuthDate   time.Time
	StartParam string
	Raw        string
}

type TelegramInitValidator struct {
	token  string
	maxAge time.Duration
}

func NewTelegramInitValidator(token string, maxAge time.Duration) TelegramInitValidator {
	return TelegramInitValidator{token: token, maxAge: maxAge}
}

func (v TelegramInitValidator) Validate(raw string, now time.Time) (TelegramInitData, error) {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(v.token) == "" {
		return TelegramInitData{}, ErrInvalidInitData
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return TelegramInitData{}, ErrInvalidInitData
	}
	givenHash := values.Get("hash")
	if givenHash == "" {
		return TelegramInitData{}, ErrInvalidInitData
	}
	values.Del("hash")

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	dataCheckString := strings.Join(parts, "\n")

	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretMAC.Write([]byte(v.token))
	secret := secretMAC.Sum(nil)

	checkMAC := hmac.New(sha256.New, secret)
	_, _ = checkMAC.Write([]byte(dataCheckString))
	expected := hex.EncodeToString(checkMAC.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(givenHash)) {
		return TelegramInitData{}, ErrInvalidInitData
	}

	authUnix, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil || authUnix <= 0 {
		return TelegramInitData{}, ErrInvalidInitData
	}
	authDate := time.Unix(authUnix, 0)
	if v.maxAge > 0 && now.Sub(authDate) > v.maxAge {
		return TelegramInitData{}, ErrInvalidInitData
	}

	var user TelegramInitUser
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil || user.ID == 0 {
		return TelegramInitData{}, ErrInvalidInitData
	}
	return TelegramInitData{
		User:       user,
		QueryID:    values.Get("query_id"),
		AuthDate:   authDate,
		StartParam: values.Get("start_param"),
		Raw:        raw,
	}, nil
}
