package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrCacheMiss = errors.New("cache miss")

type KV interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	Del(ctx context.Context, key string) error
}

type RedisKV struct {
	client *redis.Client
}

func NewRedisKV(client *redis.Client) RedisKV {
	return RedisKV{client: client}
}

func (r RedisKV) Get(ctx context.Context, key string) (string, error) {
	value, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", ErrCacheMiss
	}
	return value, err
}

func (r RedisKV) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r RedisKV) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	return r.client.SetNX(ctx, key, value, ttl).Result()
}

func (r RedisKV) Del(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

type MemoryKV struct {
	mu    sync.Mutex
	items map[string]memoryItem
}

type memoryItem struct {
	value     string
	expiresAt time.Time
}

func NewMemoryKV() *MemoryKV {
	return &MemoryKV{items: map[string]memoryItem{}}
}

func (m *MemoryKV) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[key]
	if !ok {
		return "", ErrCacheMiss
	}
	if !item.expiresAt.IsZero() && time.Now().After(item.expiresAt) {
		delete(m.items, key)
		return "", ErrCacheMiss
	}
	return item.value, nil
}

func (m *MemoryKV) Set(_ context.Context, key, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := memoryItem{value: value}
	if ttl > 0 {
		item.expiresAt = time.Now().Add(ttl)
	}
	m.items[key] = item
	return nil
}

func (m *MemoryKV) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if item, ok := m.items[key]; ok {
		if item.expiresAt.IsZero() || time.Now().Before(item.expiresAt) {
			return false, nil
		}
	}
	item := memoryItem{value: value}
	if ttl > 0 {
		item.expiresAt = time.Now().Add(ttl)
	}
	m.items[key] = item
	return true, nil
}

func (m *MemoryKV) Del(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, key)
	return nil
}

type BrowserSession struct {
	AdminID int64  `json:"admin_id"`
	Role    string `json:"role"`
	Name    string `json:"name"`
}

type SessionManager struct {
	kv  KV
	ttl time.Duration
}

func NewSessionManager(kv KV, ttl time.Duration) SessionManager {
	return SessionManager{kv: kv, ttl: ttl}
}

func (s SessionManager) Create(ctx context.Context, session BrowserSession) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(session)
	if err := s.kv.Set(ctx, "browser_session:"+token, string(raw), s.ttl); err != nil {
		return "", err
	}
	return token, nil
}

func (s SessionManager) Get(ctx context.Context, token string) (BrowserSession, error) {
	raw, err := s.kv.Get(ctx, "browser_session:"+token)
	if err != nil {
		return BrowserSession{}, err
	}
	var session BrowserSession
	if err := json.Unmarshal([]byte(raw), &session); err != nil {
		return BrowserSession{}, err
	}
	return session, nil
}

func (s SessionManager) Delete(ctx context.Context, token string) error {
	return s.kv.Del(ctx, "browser_session:"+token)
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
