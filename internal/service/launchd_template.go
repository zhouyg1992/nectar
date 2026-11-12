package service

import (
	"bytes"
	"fmt"
	"path/filepath"
	"text/template"
)

const launchdLabel = "com.zhouyg1992.nectar"

var launchdTemplate = template.Must(template.New("launchd").Funcs(template.FuncMap{
	"xml": xmlEscape,
}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>{{xml .Label}}</string>
  <key>ProgramArguments</key>
  <array>
    <string>{{xml .BinaryPath}}</string>
    <string>-config</string>
    <string>{{xml .ConfigPath}}</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ThrottleInterval</key>
  <integer>30</integer>
  <key>ProcessType</key>
  <string>Background</string>
  <key>WorkingDirectory</key>
  <string>{{xml .Home}}</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key>
    <string>{{xml .Home}}</string>
    <key>PATH</key>
    <string>{{xml .Path}}</string>
    {{- if .CodexHome}}
    <key>CODEX_HOME</key>
    <string>{{xml .CodexHome}}</string>
    {{- end}}
  </dict>
  <key>StandardOutPath</key>
  <string>{{xml .OutLog}}</string>
  <key>StandardErrorPath</key>
  <string>{{xml .ErrLog}}</string>
</dict>
</plist>
`))

func renderLaunchd(m *Manager) (string, error) {
	outLog, errLog := m.logPaths()
	data := struct {
		Label      string
		BinaryPath string
		ConfigPath string
		Home       string
		Path       string
		CodexHome  string
		OutLog     string
		ErrLog     string
	}{
		Label:      m.Label,
		BinaryPath: m.BinaryPath,
		ConfigPath: m.ConfigPath,
		Home:       m.Home,
		Path:       buildPath(m.BinaryPath, "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"),
		CodexHome:  codexHome(),
		OutLog:     outLog,
		ErrLog:     errLog,
	}
	var buf bytes.Buffer
	if err := launchdTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render launchd plist: %w", err)
	}
	return buf.String(), nil
}

func (m *Manager) logPaths() (string, string) {
	dir := filepath.Join(m.Home, "Library", "Logs")
	return filepath.Join(dir, "nectar.out.log"), filepath.Join(dir, "nectar.err.log")
}
