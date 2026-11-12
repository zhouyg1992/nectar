package service

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

const systemdUnit = "nectar.service"

var systemdTemplate = template.Must(template.New("systemd").Funcs(template.FuncMap{
	"systemd": systemdEscape,
}).Parse(`[Unit]
Description=Nectar Feishu-Codex bridge

[Service]
Type=simple
ExecStart="{{systemd .BinaryPath}}" -config "{{systemd .ConfigPath}}"
WorkingDirectory="{{systemd .Home}}"
Environment="HOME={{systemd .Home}}"
Environment="PATH={{systemd .Path}}"
{{- if .CodexHome}}
Environment="CODEX_HOME={{systemd .CodexHome}}"
{{- end}}
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
`))

func renderSystemd(m *Manager) (string, error) {
	data := struct {
		BinaryPath string
		ConfigPath string
		Home       string
		Path       string
		CodexHome  string
	}{
		BinaryPath: m.BinaryPath,
		ConfigPath: m.ConfigPath,
		Home:       m.Home,
		Path:       buildPath(m.BinaryPath, "/usr/local/bin", "/usr/bin", "/bin"),
		CodexHome:  codexHome(),
	}
	var buf bytes.Buffer
	if err := systemdTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render systemd unit: %w", err)
	}
	return buf.String(), nil
}

func systemdEscape(value string) string {
	return strings.ReplaceAll(value, "%", "%%")
}
