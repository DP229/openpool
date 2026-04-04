package auth

import (
	"os"
	"testing"
	"time"
)

func TestManager_GenerateAPIKey(t *testing.T) {
	dbPath := "test_auth.db"
	defer os.Remove(dbPath)

	mgr, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Close()

	key, err := mgr.GenerateAPIKey("Test User", "test@example.com", 100, []string{"submit", "query"}, 365*24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate API key: %v", err)
	}

	if key.ID == "" {
		t.Error("Key ID should not be empty")
	}
	if key.Key == "" {
		t.Error("Key should not be empty")
	}
	if len(key.Key) < 10 {
		t.Error("Key should be at least 10 characters")
	}
	if key.Credits != 100 {
		t.Errorf("Credits should be 100, got %d", key.Credits)
	}
	if len(key.Scopes) != 2 {
		t.Errorf("Should have 2 scopes, got %d", len(key.Scopes))
	}
}

func TestManager_Validate(t *testing.T) {
	dbPath := "test_auth_validate.db"
	defer os.Remove(dbPath)

	mgr, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Close()

	key, _ := mgr.GenerateAPIKey("Test User", "test@example.com", 100, []string{"submit", "query"}, 365*24*time.Hour)

	validated, err := mgr.Validate(key.Key)
	if err != nil {
		t.Fatalf("Failed to validate key: %v", err)
	}

	if validated.ID != key.ID {
		t.Error("Validated key ID should match generated key ID")
	}
	if validated.OwnerEmail != "test@example.com" {
		t.Error("Owner email should match")
	}
}

func TestManager_Validate_InvalidKey(t *testing.T) {
	dbPath := "test_auth_invalid.db"
	defer os.Remove(dbPath)

	mgr, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Close()

	_, err = mgr.Validate("invalid_key")
	if err != ErrInvalidAPIKey {
		t.Errorf("Should return ErrInvalidAPIKey, got %v", err)
	}
}

func TestManager_UseCredits(t *testing.T) {
	dbPath := "test_auth_credits.db"
	defer os.Remove(dbPath)

	mgr, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Close()

	key, err := mgr.GenerateAPIKey("Test User", "test@example.com", 100, []string{"submit"}, 365*24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate API key: %v", err)
	}

	err = mgr.UseCredits(key.Key, 30)
	if err != nil {
		t.Fatalf("Failed to use credits: %v", err)
	}

	validated, err := mgr.Validate(key.Key)
	if err != nil {
		t.Fatalf("Failed to validate key: %v", err)
	}
	if validated.Credits != 70 {
		t.Errorf("Credits should be 70, got %d", validated.Credits)
	}

	err = mgr.UseCredits(key.Key, 100)
	if err != ErrInsufficientCredits {
		t.Errorf("Should return ErrInsufficientCredits, got %v", err)
	}
}

func TestManager_AddCredits(t *testing.T) {
	dbPath := "test_auth_add_credits.db"
	defer os.Remove(dbPath)

	mgr, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Close()

	key, _ := mgr.GenerateAPIKey("Test User", "test@example.com", 100, []string{"submit"}, 365*24*time.Hour)

	err = mgr.AddCredits(key.Key, 50)
	if err != nil {
		t.Fatalf("Failed to add credits: %v", err)
	}

	validated, _ := mgr.Validate(key.Key)
	if validated.Credits != 150 {
		t.Errorf("Credits should be 150, got %d", validated.Credits)
	}
}

func TestManager_RevokeKey(t *testing.T) {
	dbPath := "test_auth_revoke.db"
	defer os.Remove(dbPath)

	mgr, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Close()

	key, err := mgr.GenerateAPIKey("Test User", "test@example.com", 100, []string{"submit"}, 365*24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	err = mgr.RevokeKey(key.ID)
	if err != nil {
		t.Fatalf("Failed to revoke key: %v", err)
	}

	_, err = mgr.Validate(key.Key)
	if err != ErrInvalidAPIKey {
		t.Errorf("Should return ErrInvalidAPIKey, got %v", err)
	}
}

func TestManager_ListKeys(t *testing.T) {
	dbPath := "test_auth_list.db"
	defer os.Remove(dbPath)

	mgr, err := NewManager(dbPath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Close()

	mgr.GenerateAPIKey("User 1", "user1@example.com", 100, []string{"submit"}, 365*24*time.Hour)
	mgr.GenerateAPIKey("User 2", "user2@example.com", 200, []string{"query"}, 365*24*time.Hour)

	keys, err := mgr.ListKeys("")
	if err != nil {
		t.Fatalf("Failed to list keys: %v", err)
	}

	if len(keys) != 2 {
		t.Errorf("Should have 2 keys, got %d", len(keys))
	}

	keys, err = mgr.ListKeys("user1@example.com")
	if err != nil {
		t.Fatalf("Failed to list keys for owner: %v", err)
	}

	if len(keys) != 1 {
		t.Errorf("Should have 1 key for user1@example.com, got %d", len(keys))
	}
}
