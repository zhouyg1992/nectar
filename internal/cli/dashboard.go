package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/config"
	"github.com/zhouyg1992/nectar/internal/dashboard"
)

func runDashboard(args []string) error {
	fs := flag.NewFlagSet("dashboard", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config.toml")
	jsonOut := fs.Bool("json", false, "print login URL as JSON")
	noBrowser := fs.Bool("no-browser", false, "print the login URL without opening a browser")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if !cfg.DashboardEnabled() {
		return fmt.Errorf("dashboard is disabled; add [dashboard] enabled = true to %s and restart nectar", config.DefaultPath())
	}
	token, err := dashboard.EnsureToken(cfg.DashboardTokenPath())
	if err != nil {
		return err
	}
	loginURL := dashboard.LoginURL(cfg.Dashboard.Listen, token)
	if *jsonOut {
		data, err := json.Marshal(map[string]string{
			"url":        loginURL,
			"listen":     cfg.Dashboard.Listen,
			"token_file": cfg.DashboardTokenPath(),
		})
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	fmt.Println(loginURL)
	fmt.Printf("\nListen: %s\n", cfg.Dashboard.Listen)
	fmt.Printf("Token:  %s\n", cfg.DashboardTokenPath())
	if !dashboardRunning(cfg.Dashboard.Listen) {
		fmt.Println("\nDashboard is not reachable yet. Start Nectar first: nectar service start")
	}
	if !*noBrowser {
		if err := openDashboardBrowser(loginURL); err != nil {
			fmt.Printf("\nCould not open browser automatically: %v\n", err)
		}
	}
	return nil
}

var openDashboardBrowser = openBrowser

func openBrowser(rawURL string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", rawURL)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		if isWSL() {
			command = exec.Command("cmd.exe", "/c", "start", "", rawURL)
		} else {
			command = exec.Command("xdg-open", rawURL)
		}
	}
	if err := command.Start(); err != nil {
		return err
	}
	return nil
}

func isWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "microsoft")
}

func dashboardRunning(listen string) bool {
	host, port, err := splitListen(listen)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://" + host + ":" + port + "/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func splitListen(listen string) (string, string, error) {
	listen = strings.TrimSpace(listen)
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", "", err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return host, port, nil
}
