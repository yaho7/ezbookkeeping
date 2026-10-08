package mail

import (
	"bytes"
	stdmail "net/mail"
	"testing"

	"github.com/mayswind/ezbookkeeping/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomail "gopkg.in/mail.v2"
)

func TestMailerEncodesSenderDisplayName(t *testing.T) {
	mailer := &DefaultMailer{fromAddress: "alice@example.com", fromName: "账单助手"}
	message := mailer.newMessage(&MailMessage{To: "bob@example.com", Subject: "Run finished", Body: "Done"})
	var encoded bytes.Buffer
	_, err := message.WriteTo(&encoded)
	require.NoError(t, err)
	parsed, err := stdmail.ReadMessage(&encoded)
	require.NoError(t, err)
	sender, err := stdmail.ParseAddress(parsed.Header.Get("From"))
	require.NoError(t, err)
	assert.Equal(t, "账单助手", sender.Name)
	assert.Equal(t, "alice@example.com", sender.Address)

	mailer.fromName = ""
	assert.Equal(t, "alice@example.com", mailer.newMessage(&MailMessage{To: "bob@example.com"}).GetHeader("From")[0])
}

func TestSecureMailerRequiresTLSOnSubmissionPorts(t *testing.T) {
	for _, host := range []string{"smtp.example.com:465", "smtp.example.com:587"} {
		m, err := NewSecureMailer(&settings.SMTPConfig{SMTPHost: host, SMTPSkipTLSVerify: true})
		require.NoError(t, err)
		assert.False(t, m.dialer.TLSConfig.InsecureSkipVerify)
		assert.Equal(t, gomail.MandatoryStartTLS, m.dialer.StartTLSPolicy)
		assert.Equal(t, host == "smtp.example.com:465", m.dialer.SSL)
	}
}
