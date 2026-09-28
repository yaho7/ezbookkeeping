package settings

import (
	"fmt"
	"net"
	"net/mail"
	"strings"
)

// EmailBillNotificationConfig is independent of the application's verification mailer.
type EmailBillNotificationConfig struct {
	Enabled               bool
	Mode                  string
	Recipient             string
	SMTPServer            string
	SMTPPort              uint16
	SMTPUser              string
	SMTPPassword          string
	FromAddress           string
	UseMailboxCredentials bool
}

// NormalizeEmailBillNotification infers common providers and validates sending when enabled or tested.
func NormalizeEmailBillNotification(config *EmailBillConfig, requireSending bool) error {
	if config.Notification == nil {
		config.Notification = &EmailBillNotificationConfig{Mode: "always", UseMailboxCredentials: true}
	}
	n := config.Notification
	n.Mode, n.Recipient = strings.TrimSpace(n.Mode), strings.TrimSpace(n.Recipient)
	n.SMTPServer, n.SMTPUser = strings.TrimSpace(n.SMTPServer), strings.TrimSpace(n.SMTPUser)
	n.FromAddress = strings.TrimSpace(n.FromAddress)
	if n.Mode == "" {
		n.Mode = "always"
	}
	if n.Mode != "always" && n.Mode != "changes_only" && n.Mode != "changes_or_errors" && n.Mode != "errors_only" {
		return fmt.Errorf("Choose a notification condition")
	}
	if n.Recipient == "" {
		n.Recipient = config.MailUser
	}
	if n.FromAddress == "" {
		n.FromAddress = config.MailUser
	}
	if n.SMTPUser == "" {
		n.SMTPUser = config.MailUser
	}
	if n.SMTPServer == "" {
		switch emailDomain(n.SMTPUser) {
		case "qq.com", "foxmail.com":
			n.SMTPServer = "smtp.qq.com"
		case "gmail.com", "googlemail.com":
			n.SMTPServer = "smtp.gmail.com"
		case "163.com":
			n.SMTPServer = "smtp.163.com"
		case "126.com":
			n.SMTPServer = "smtp.126.com"
		case "outlook.com", "hotmail.com", "live.com":
			n.SMTPServer = "smtp.office365.com"
			if n.SMTPPort == 0 {
				n.SMTPPort = 587
			}
		}
	}
	if n.SMTPPort == 0 {
		n.SMTPPort = 465
	}
	if !n.Enabled && !requireSending {
		return nil
	}
	for _, address := range []struct{ label, value string }{{"Enter a valid recipient email", n.Recipient}, {"Enter a valid sender email", n.FromAddress}} {
		parsed, err := mail.ParseAddress(address.value)
		if err != nil || parsed.Address != address.value || strings.ContainsAny(address.value, "\x00\r\n") {
			return fmt.Errorf("%s", address.label)
		}
	}
	if n.SMTPServer == "" || strings.ContainsAny(n.SMTPServer, " /\\@\x00\r\n") || (strings.Contains(n.SMTPServer, ":") && net.ParseIP(n.SMTPServer) == nil) {
		return fmt.Errorf("Enter an SMTP server hostname")
	}
	if n.SMTPUser == "" || strings.ContainsAny(n.SMTPUser, "\x00\r\n") {
		return fmt.Errorf("Enter the SMTP username")
	}
	if n.UseMailboxCredentials {
		if n.SMTPUser != config.MailUser {
			return fmt.Errorf("Use the mailbox username when reusing its password")
		}
		if config.MailPassword == "" {
			return fmt.Errorf("Save the mailbox password before enabling notifications")
		}
	} else if n.SMTPPassword == "" {
		return fmt.Errorf("Enter the SMTP password or reuse the mailbox password")
	}
	return nil
}

// EmailBillNotificationSMTP returns the resolved transport without mutating stored configuration.
func EmailBillNotificationSMTP(config *EmailBillConfig) *SMTPConfig {
	n := config.Notification
	password := n.SMTPPassword
	if n.UseMailboxCredentials {
		password = config.MailPassword
	}
	return &SMTPConfig{SMTPHost: net.JoinHostPort(n.SMTPServer, fmt.Sprint(n.SMTPPort)), SMTPUser: n.SMTPUser, SMTPPasswd: password, FromAddress: n.FromAddress}
}
