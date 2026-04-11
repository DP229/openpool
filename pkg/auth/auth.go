package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	_ "github.com/mattn/go-sqlite3"
)

var (
	ErrInvalidAPIKey       = errors.New("invalid API key")
	ErrExpiredAPIKey       = errors.New("API key has expired")
	ErrInsufficientCredits = errors.New("insufficient credits")
	ErrInvalidScope        = errors.New("invalid scope")
)

type APIKey struct {
	ID            string
	Key           string
	OwnerName     string
	OwnerEmail    string
	Credits       int
	CreatedAt     time.Time
	ExpiresAt     time.Time
	Scopes        []string
	RequestsCount int
	LastUsed      *time.Time
	RateLimit     int
}

type Manager struct {
	db *sql.DB
	mu sync.RWMutex
}

func NewManager(dbPath string) (*Manager, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS api_keys (
			id TEXT PRIMARY KEY,
			key_prefix TEXT NOT NULL,
			key TEXT UNIQUE NOT NULL,
			owner_name TEXT NOT NULL,
			owner_email TEXT NOT NULL,
			credits INTEGER NOT NULL DEFAULT 100,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			scopes TEXT NOT NULL DEFAULT 'submit,query',
			requests_count INTEGER NOT NULL DEFAULT 0,
			last_used INTEGER,
			rate_limit INTEGER NOT NULL DEFAULT 100
		);
		CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(key_prefix);
		CREATE INDEX IF NOT EXISTS idx_api_keys_owner ON api_keys(owner_email);
	`)
	if err != nil {
		db.Close()
		return nil, err
	}

	return &Manager{db: db}, nil
}

func (m *Manager) GenerateAPIKey(ownerName, ownerEmail string, credits int, scopes []string, duration time.Duration) (*APIKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(idBytes)

	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, err
	}
	key := "op_" + hex.EncodeToString(keyBytes)

	// Store bcrypt hash of the key, return plaintext to caller
	hashedKey, err := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	scopesStr := "submit,query"
	if len(scopes) > 0 {
		scopesStr = ""
		for i, s := range scopes {
			if i > 0 {
				scopesStr += ","
			}
			scopesStr += s
		}
	}

	now := time.Now()
	apiKey := &APIKey{
		ID:         id,
		Key:        key,
		OwnerName:  ownerName,
		OwnerEmail: ownerEmail,
		Credits:    credits,
		CreatedAt:  now,
		ExpiresAt:  now.Add(duration),
		Scopes:     scopes,
		RateLimit:  100,
	}

	keyPrefix := key
	if len(keyPrefix) > 8 {
		keyPrefix = keyPrefix[:8]
	}

	_, err = m.db.Exec(`
		INSERT INTO api_keys (id, key_prefix, key, owner_name, owner_email, credits, created_at, expires_at, scopes, rate_limit)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, apiKey.ID, keyPrefix, hashedKey, apiKey.OwnerName, apiKey.OwnerEmail, apiKey.Credits,
		apiKey.CreatedAt.Unix(), apiKey.ExpiresAt.Unix(), scopesStr, apiKey.RateLimit)

	if err != nil {
		return nil, err
	}

	return apiKey, nil
}

func (m *Manager) Validate(key string) (*APIKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keyPrefix := key
	if len(keyPrefix) > 8 {
		keyPrefix = keyPrefix[:8]
	}

	rows, err := m.db.Query(`
		SELECT id, key, owner_name, owner_email, credits, created_at, expires_at, scopes, requests_count, last_used, rate_limit
		FROM api_keys WHERE key_prefix = ?
	`, keyPrefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		apiKey := &APIKey{}
		var hashedKey string
		var scopesStr string
		var lastUsed sql.NullInt64
		var createdAtUnix, expiresAtUnix int64

		err := rows.Scan(
			&apiKey.ID, &hashedKey, &apiKey.OwnerName, &apiKey.OwnerEmail, &apiKey.Credits,
			&createdAtUnix, &expiresAtUnix, &scopesStr, &apiKey.RequestsCount, &lastUsed, &apiKey.RateLimit,
		)
		if err != nil {
			continue
		}

		if err := bcrypt.CompareHashAndPassword([]byte(hashedKey), []byte(key)); err != nil {
			continue
		}

		apiKey.CreatedAt = time.Unix(createdAtUnix, 0)
		apiKey.ExpiresAt = time.Unix(expiresAtUnix, 0)
		apiKey.Key = key

		if time.Now().After(apiKey.ExpiresAt) {
			return nil, ErrExpiredAPIKey
		}

		if scopesStr != "" {
			apiKey.Scopes = []string{}
			for _, s := range []string{"submit", "query", "admin"} {
				if containsScope(scopesStr, s) {
					apiKey.Scopes = append(apiKey.Scopes, s)
				}
			}
		}

		if lastUsed.Valid {
			t := time.Unix(lastUsed.Int64, 0)
			apiKey.LastUsed = &t
		}

		return apiKey, nil
	}

	return nil, ErrInvalidAPIKey
}

func (m *Manager) findKeyID(key string) (string, error) {
	keyPrefix := key
	if len(keyPrefix) > 8 {
		keyPrefix = keyPrefix[:8]
	}

	rows, err := m.db.Query(`SELECT id, key FROM api_keys WHERE key_prefix = ?`, keyPrefix)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	for rows.Next() {
		var id, hashedKey string
		if err := rows.Scan(&id, &hashedKey); err != nil {
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hashedKey), []byte(key)); err != nil {
			continue
		}
		return id, nil
	}

	return "", ErrInvalidAPIKey
}

func (m *Manager) UseCredits(key string, amount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	id, err := m.findKeyID(key)
	if err != nil {
		return err
	}

	var currentCredits int
	err = m.db.QueryRow("SELECT credits FROM api_keys WHERE id = ?", id).Scan(&currentCredits)
	if err != nil {
		return err
	}

	if currentCredits < amount {
		return ErrInsufficientCredits
	}

	_, err = m.db.Exec(`
		UPDATE api_keys 
		SET credits = credits - ?, requests_count = requests_count + 1, last_used = ?
		WHERE id = ?
	`, amount, time.Now().Unix(), id)

	return err
}

func (m *Manager) AddCredits(key string, amount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	id, err := m.findKeyID(key)
	if err != nil {
		return err
	}

	_, err = m.db.Exec("UPDATE api_keys SET credits = credits + ? WHERE id = ?", amount, id)
	return err
}

func (m *Manager) RevokeKey(idOrKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.Exec("DELETE FROM api_keys WHERE id = ?", idOrKey)
	return err
}

func (m *Manager) ListKeys(ownerEmail string) ([]*APIKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	query := "SELECT id, key_prefix, key, owner_name, owner_email, credits, created_at, expires_at, scopes, requests_count, last_used, rate_limit FROM api_keys"
	args := []interface{}{}

	if ownerEmail != "" {
		query += " WHERE owner_email = ?"
		args = append(args, ownerEmail)
	}

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*APIKey
	for rows.Next() {
		var keyPrefix, hashedKey string
		var scopesStr string
		var lastUsed sql.NullInt64
		var createdAtUnix, expiresAtUnix int64
		apiKey := &APIKey{}

		err := rows.Scan(
			&apiKey.ID, &keyPrefix, &hashedKey, &apiKey.OwnerName, &apiKey.OwnerEmail,
			&apiKey.Credits, &createdAtUnix, &expiresAtUnix, &scopesStr,
			&apiKey.RequestsCount, &lastUsed, &apiKey.RateLimit,
		)
		if err != nil {
			return nil, err
		}

		apiKey.Key = keyPrefix + "..."
		apiKey.CreatedAt = time.Unix(createdAtUnix, 0)
		apiKey.ExpiresAt = time.Unix(expiresAtUnix, 0)

		if scopesStr != "" {
			apiKey.Scopes = []string{}
			for _, s := range []string{"submit", "query", "admin"} {
				if containsScope(scopesStr, s) {
					apiKey.Scopes = append(apiKey.Scopes, s)
				}
			}
		}

		if lastUsed.Valid {
			t := time.Unix(lastUsed.Int64, 0)
			apiKey.LastUsed = &t
		}

		keys = append(keys, apiKey)
	}

	return keys, nil
}

func (m *Manager) Close() error {
	return m.db.Close()
}

func containsScope(scopesStr, scope string) bool {
	return len(scopesStr) > 0 &&
		(scopesStr == scope ||
			len(scopesStr) > len(scope) &&
				(scopesStr[:len(scope)+1] == scope+"," ||
					scopesStr[len(scopesStr)-len(scope)-1:] == ","+scope ||
					(len(scopesStr) > len(scope) && subtle.ConstantTimeCompare([]byte(scopesStr), []byte(scope)) == 1)))
}
