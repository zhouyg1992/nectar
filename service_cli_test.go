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
		{
			name:    "dingtalk configured",
			content: "[bot]\nplatform = \"dingtalk\"\n\n[dingtalk]\nclient_id = \"dingrealid\"\nclient_secret = \"real-secret-value\"\n",
		},
		{
			name:    "dingtalk auto detected",
			content: "[dingtalk]\nclient_id = \"dingrealid\"\nclient_secret = \"real-secret-value\"\n",
		},
		{
			name:    "dingtalk placeholders",
			content: "[bot]\nplatform = \"dingtalk\"\n\n[dingtalk]\nclient_id = \"dingxxxxxxxxxxxxxxxx\"\nclient_secret = \"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\"\n",
			wantErr: true,
		},
		{
			name:    "both platforms require selection",
			content: "[feishu]\napp_id = \"cli_realid\"\napp_secret = \"real-secret-value\"\n\n[dingtalk]\nclient_id = \"dingrealid\"\nclient_secret = \"real-secret-value\"\n",
			wantErr: true,
		},
		{
			name:    "telegram configured",
			content: "[bot]\nplatform = \"telegram\"\n\n[telegram]\nbot_token = \"test-token\"\n",
		},
		{
			name:    "telegram auto detected",
			content: "[telegram]\nbot_token = \"test-token\"\n",
		},
		{
			name:    "telegram placeholder",
			content: "[bot]\nplatform = \"telegram\"\n\n[telegram]\nbot_token = \"123456:xxxxxxxxxxxxxxxx\"\n",
			wantErr: true,
		},
		{
			name:    "telegram and feishu require selection",
			content: "[feishu]\napp_id = \"cli_realid\"\napp_secret = \"real-secret-value\"\n\n[telegram]\nbot_token = \"test-token\"\n",
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
