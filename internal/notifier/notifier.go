package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"dns-watcher/internal/config"
	"dns-watcher/internal/storage"
)

func FormatChangesText(changes []storage.Change) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[DNS Watcher] %d cambio(s) de DNS detectado(s):\n", len(changes))
	for _, ch := range changes {
		oldStr := "(vacío)"
		if len(ch.OldRecords) > 0 {
			oldStr = strings.Join(ch.OldRecords, ", ")
		}
		newStr := "(vacío)"
		if len(ch.NewRecords) > 0 {
			newStr = strings.Join(ch.NewRecords, ", ")
		}
		fmt.Fprintf(&sb, "- %s [%s]: %s  ==>  %s (%s)\n", ch.Domain, ch.RecordType, oldStr, newStr, ch.DetectedAt)
	}
	return strings.TrimRight(sb.String(), "\n")
}

func NotifyChanges(changes []storage.Change, cfg config.NotifyConfig, appDir string) []string {
	if len(changes) == 0 {
		return nil
	}
	summary := FormatChangesText(changes)
	var errs []string

	if cfg.LogFile {
		if err := appendLog(filepath.Join(appDir, "changes.log"), summary); err != nil {
			errs = append(errs, fmt.Sprintf("log_file: %v", err))
		}
	}

	if cfg.WindowsToast && runtime.GOOS == "windows" {
		if err := sendWindowsToast(fmt.Sprintf("DNS Watcher: %d cambio(s)", len(changes)), toastBody(changes)); err != nil {
			errs = append(errs, fmt.Sprintf("windows_toast: %v", err))
		}
	}

	if cfg.WebhookURL != "" {
		postJSON(cfg.WebhookURL, map[string]any{
			"text":    summary,
			"count":   len(changes),
			"changes": changes,
		}, "webhook", &errs)
	}

	if cfg.DiscordWebhookURL != "" {
		msg := summary
		if len(msg) > 1900 {
			msg = msg[:1900]
		}
		postJSON(cfg.DiscordWebhookURL, map[string]any{"content": msg}, "discord", &errs)
	}

	if cfg.SlackWebhookURL != "" {
		postJSON(cfg.SlackWebhookURL, map[string]any{"text": summary}, "slack", &errs)
	}

	if cfg.TelegramBotToken != "" && cfg.TelegramChatID != "" {
		tgURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.TelegramBotToken)
		msg := summary
		if len(msg) > 4000 {
			msg = msg[:4000]
		}
		postJSON(tgURL, map[string]any{"chat_id": cfg.TelegramChatID, "text": msg}, "telegram", &errs)
	}

	if cfg.SMTPHost != "" && cfg.SMTPTo != "" {
		if err := sendEmail(cfg, summary); err != nil {
			errs = append(errs, fmt.Sprintf("email: %v", err))
		}
	}

	return errs
}

func appendLog(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, text)
	return err
}

func toastBody(changes []storage.Change) string {
	limit := min(3, len(changes))
	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		parts = append(parts, fmt.Sprintf("%s (%s)", changes[i].Domain, changes[i].RecordType))
	}
	if len(changes) > 3 {
		parts = append(parts, fmt.Sprintf("+%d más", len(changes)-3))
	}
	return strings.Join(parts, ", ")
}

func sendWindowsToast(title, body string) error {
	safeTitle := strings.ReplaceAll(title, "'", "''")
	safeBody := strings.ReplaceAll(body, "'", "''")
	ps := fmt.Sprintf(
		"[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null; "+
			"[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null; "+
			"$xml = New-Object Windows.Data.Xml.Dom.XmlDocument; "+
			"$xml.LoadXml('<toast><visual><binding template=\"ToastText02\"><text id=\"1\">%s</text><text id=\"2\">%s</text></binding></visual></toast>'); "+
			"$toast = [Windows.UI.Notifications.ToastNotification]::new($xml); "+
			"[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('DNS Watcher').Show($toast);",
		safeTitle, safeBody,
	)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	return cmd.Run()
}

func postJSON(targetURL string, payload any, label string, errs *[]string) {
	data, err := json.Marshal(payload)
	if err != nil {
		*errs = append(*errs, fmt.Sprintf("%s: %v", label, err))
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(data))
	if err != nil {
		*errs = append(*errs, fmt.Sprintf("%s: %v", label, err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "dns-watcher/0.1")
	resp, err := client.Do(req)
	if err != nil {
		*errs = append(*errs, fmt.Sprintf("%s: %v", label, err))
		return
	}
	resp.Body.Close()
}

func sendEmail(cfg config.NotifyConfig, body string) error {
	from := cfg.SMTPFrom
	if from == "" {
		from = cfg.SMTPUser
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: DNS Watcher: Cambios de DNS detectados\r\n\r\n%s\r\n", from, cfg.SMTPTo, body)
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	var auth smtp.Auth
	if cfg.SMTPUser != "" && cfg.SMTPPassword != "" {
		auth = smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPassword, cfg.SMTPHost)
	}
	return smtp.SendMail(addr, auth, from, []string{cfg.SMTPTo}, []byte(msg))
}
