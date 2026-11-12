package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateServiceConfig(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name:    "configured",
			content: "[feishu]\napp_id = \"cli_aab6629286bb5ce7\"\napp_secret = \"real-secret-value\"\n",
		},
		{
			name:    "short placeholders",
			content: "[feishu]\napp_id = \"cli_xxx\"\napp_secret = \"xxx\"\n",
			wantErr: true,
		},
		{
			name:    "example placeholders",
			content: "[feishu]\napp_id = \"cli_xxxxxxxxxxxxxxxx\"\napp_secret = \"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\"\n",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".toml")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			err := validateServiceConfig(path)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}
