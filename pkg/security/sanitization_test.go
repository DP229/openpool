package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateWASMPath(t *testing.T) {
	sanitizer := NewSanitizer()

	tmpDir, err := os.MkdirTemp("", "openpool-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	validFile := filepath.Join(tmpDir, "test.wasm")
	if err := os.WriteFile(validFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		baseDir  string
		userPath string
		wantErr  error
	}{
		{
			name:     "valid path",
			baseDir:  tmpDir,
			userPath: "test.wasm",
			wantErr:  nil,
		},
		{
			name:     "path traversal with ..",
			baseDir:  tmpDir,
			userPath: "../etc/passwd",
			wantErr:  ErrPathTraversal,
		},
		{
			name:     "absolute path",
			baseDir:  tmpDir,
			userPath: "/etc/passwd",
			wantErr:  ErrPathTraversal,
		},
		{
			name:     "invalid extension",
			baseDir:  tmpDir,
			userPath: "test.txt",
			wantErr:  ErrInvalidExtension,
		},
		{
			name:     "file not found",
			baseDir:  tmpDir,
			userPath: "nonexistent.wasm",
			wantErr:  ErrFileNotFound,
		},
		{
			name:     "empty path",
			baseDir:  tmpDir,
			userPath: "",
			wantErr:  ErrFileNotFound,
		},
		{
			name:     "nested traversal",
			baseDir:  tmpDir,
			userPath: "subdir/../../../etc/passwd",
			wantErr:  ErrPathTraversal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sanitizer.ValidateWASMPath(tt.baseDir, tt.userPath)
			if err != tt.wantErr {
				t.Errorf("ValidateWASMPath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSanitizeString(t *testing.T) {
	sanitizer := NewSanitizer()

	tests := []struct {
		name      string
		input     string
		maxLength int
		want      string
	}{
		{
			name:      "normal string",
			input:     "hello world",
			maxLength: 100,
			want:      "hello world",
		},
		{
			name:      "string with null bytes",
			input:     "hello\x00world",
			maxLength: 100,
			want:      "helloworld",
		},
		{
			name:      "script injection",
			input:     "<script>alert('xss')</script>",
			maxLength: 100,
			want:      "alert('xss')",
		},
		{
			name:      "truncate to max length",
			input:     "hello world",
			maxLength: 5,
			want:      "hello",
		},
		{
			name:      "javascript protocol",
			input:     "javascript:alert(1)",
			maxLength: 100,
			want:      "alert(1)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizer.SanitizeString(tt.input, tt.maxLength)
			if got != tt.want {
				t.Errorf("SanitizeString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateAPIKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"valid key", "op_a1b2c3d4e5f6g7h8i9j0", false},
		{"too short", "short", true},
		{"too long", string(make([]byte, 129)), true},
		{"special chars", "op_a1b2@#$%", true},
		{"empty key", "", true},
		{"with dots", "op_abc123.xyz789", false},
		{"with hyphens", "op_abc123-xyz789", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAPIKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAPIKey() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCredits(t *testing.T) {
	tests := []struct {
		name    string
		credits int
		wantErr bool
	}{
		{"valid credits", 100, false},
		{"zero credits", 0, false},
		{"negative credits", -10, true},
		{"exceeds max", 15000, true},
		{"at max", 10000, false},
		{"just over max", 10001, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCredits(tt.credits)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCredits() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
