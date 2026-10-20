//go:build unit

package handler

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type promotionStoredValue struct {
	value   string
	expires time.Time
}
type promotionMemoryStore struct {
	mu              sync.Mutex
	now             time.Time
	entries         map[string]promotionStoredValue
	fault           string
	failBrowserRead atomic.Bool
}

func newPromotionMemoryStore() *promotionMemoryStore {
	return &promotionMemoryStore{now: time.Now(), entries: make(map[string]promotionStoredValue)}
}
func (m *promotionMemoryStore) unavailable() error {
	if m.fault != "" {
		return errors.New(m.fault)
	}
	return nil
}
func (m *promotionMemoryStore) get(key string) (promotionStoredValue, error) {
	entry, ok := m.entries[key]
	if !ok || !entry.expires.After(m.now) {
		delete(m.entries, key)
		return promotionStoredValue{}, service.ErrAffiliateAttributionMissing
	}
	return entry, nil
}
func (m *promotionMemoryStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.unavailable(); err != nil {
		return err
	}
	m.entries[key] = promotionStoredValue{string(value), m.now.Add(ttl)}
	return nil
}
func (m *promotionMemoryStore) SetIfAbsent(_ context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.unavailable(); err != nil {
		return false, err
	}
	if _, err := m.get(key); err == nil {
		return false, nil
	}
	m.entries[key] = promotionStoredValue{string(value), m.now.Add(ttl)}
	return true, nil
}
func (m *promotionMemoryStore) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.unavailable(); err != nil {
		return "", err
	}
	if strings.HasPrefix(key, affiliateCookiePrefix) && m.failBrowserRead.Swap(false) {
		return "", errors.New("temporary browser attribution read failure")
	}
	value, err := m.get(key)
	return value.value, err
}
func (m *promotionMemoryStore) Take(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.unavailable(); err != nil {
		return "", err
	}
	value, err := m.get(key)
	if err != nil {
		return "", err
	}
	delete(m.entries, key)
	return value.value, nil
}
func (m *promotionMemoryStore) Count(_ context.Context, key string, ttl time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.unavailable(); err != nil {
		return 0, err
	}
	value, err := m.get(key)
	n := int64(0)
	if err == nil {
		n, err = strconv.ParseInt(value.value, 10, 64)
		if err != nil {
			return 0, err
		}
	} else {
		value.expires = m.now.Add(ttl)
	}
	n++
	value.value = strconv.FormatInt(n, 10)
	m.entries[key] = value
	return n, nil
}
func (m *promotionMemoryStore) FastForward(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = m.now.Add(d)
}
func (m *promotionMemoryStore) SetError(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fault = message
}
func (m *promotionMemoryStore) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for key := range m.entries {
		if _, err := m.get(key); err == nil {
			keys = append(keys, key)
		}
	}
	return keys
}
