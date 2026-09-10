package emailbill

import (
	"strings"
	"testing"
	"time"
)

func TestQQFoldedAuthenticationProperties(t *testing.T) {
	security := MessageSecurity{RequireAuthenticationResults: true, TrustedAuthservDomains: []string{"qq.com"}}
	for _, result := range []string{
		"mx.qq.com; dkim=pass(signature was verified) header.d=message.c\r\n\t mbchina.com",
		"mx.qq.com; spf=pass(120.234.86.5) smtp.mailfrom=<ccsvc\r\n\t @message.cmbchina.com>",
		"mx.qq.com; dkim=pass header.d=message.c\r\n\t mbchina.com header.i=@message.cmbchina.com",
	} {
		raw := "From: ccsvc@message.cmbchina.com\r\nSubject: bill\r\nAuthentication-Results: " + result + "\r\n\r\n"
		message, err := DecodeMessageHeadersWithLimit(strings.NewReader(raw), security, time.Now(), DefaultMaxMessageBytes)
		if err != nil || !message.Authenticated {
			t.Fatalf("folded authentication rejected: %v", err)
		}
	}
}

func TestFoldedAuthenticationStillRejectsUntrustedResults(t *testing.T) {
	security := MessageSecurity{RequireAuthenticationResults: true, TrustedAuthservDomains: []string{"qq.com"}}
	for _, result := range []string{
		"mx.qq.com.evil.example; dkim=pass header.d=message.c mbchina.com",
		"mx.qq.com; dkim=pass header.d=message.cmbchina.com .evil.example",
		"mx.qq.com; dkim=fail header.d=message.c mbchina.com",
		"mx.qq.com; dkim=passfail header.d=message.cmbchina.com",
		"mx.qq.com; spf=fail reason=dkim=pass header.d=message.cmbchina.com",
		"mx.qq.com; dkim=pass header.d=evil.example header.i=@cmbchina.com",
	} {
		if authenticationResultsValid(result, security) {
			t.Fatalf("untrusted result accepted: %s", result)
		}
	}
}
