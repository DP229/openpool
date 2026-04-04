package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrPathTraversal    = errors.New("path traversal attempt detected")
	ErrInvalidExtension = errors.New("invalid file extension")
	ErrFileNotFound     = errors.New("file not found")
	ErrPathNotAbsolute  = errors.New("path is not absolute")
	ErrInvalidInput     = errors.New("invalid input detected")
)

type Sanitizer struct {
	allowedExtensions map[string]bool
	maxFileSize       int64
}

func NewSanitizer() *Sanitizer {
	return &Sanitizer{
		allowedExtensions: map[string]bool{
			".wasm": true,
			".py":   true,
			".json": true,
		},
		maxFileSize: 100 * 1024 * 1024, // 100MB
	}
}

func (s *Sanitizer) ValidateWASMPath(baseDir, userPath string) (string, error) {
	if userPath == "" {
		return "", ErrFileNotFound
	}

	cleanPath := filepath.Clean(userPath)

	if strings.Contains(cleanPath, "..") {
		return "", ErrPathTraversal
	}

	if strings.HasPrefix(cleanPath, "/") || strings.Contains(cleanPath, ":") {
		return "", ErrPathTraversal
	}

	absPath, err := filepath.Abs(filepath.Join(baseDir, cleanPath))
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(absPath, baseDir) {
		return "", ErrPathTraversal
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	if !s.allowedExtensions[ext] {
		return "", ErrInvalidExtension
	}

	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		return "", ErrFileNotFound
	}
	if err != nil {
		return "", err
	}

	if !info.Mode().IsRegular() {
		return "", ErrInvalidInput
	}

	if info.Size() > s.maxFileSize {
		return "", errors.New("file exceeds maximum size")
	}

	return absPath, nil
}

func (s *Sanitizer) SanitizeString(input string, maxLength int) string {
	if len(input) > maxLength {
		input = input[:maxLength]
	}

	input = strings.ReplaceAll(input, "\x00", "")

	dangerous := []string{"<script>", "</script>", "javascript:", "onerror=", "onload="}
	result := input
	for _, d := range dangerous {
		result = strings.ReplaceAll(result, d, "")
	}

	return result
}

func (s *Sanitizer) ValidateJSON(input []byte) error {
	if len(input) == 0 {
		return ErrInvalidInput
	}

	if len(input) > 10*1024*1024 { // 10MB
		return errors.New("JSON input too large")
	}

	return nil
}

func ValidateAPIKey(key string) error {
	if len(key) < 10 || len(key) > 128 {
		return errors.New("invalid API key length")
	}

	safe := true
	for _, c := range key {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.') {
			safe = false
			break
		}
	}

	if !safe {
		return errors.New("API key contains invalid characters")
	}

	return nil
}

func ValidateTaskID(id string) error {
	if len(id) == 0 || len(id) > 256 {
		return errors.New("invalid task ID length")
	}

	return nil
}

func ValidateCredits(credits int) error {
	if credits < 0 {
		return errors.New("credits cannot be negative")
	}

	if credits > 10000 {
		return errors.New("credits exceed maximum allowed")
	}

	return nil
}
