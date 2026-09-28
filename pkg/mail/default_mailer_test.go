package mail

import (
	"github.com/mayswind/ezbookkeeping/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomail "gopkg.in/mail.v2"
	"testing"
)

func TestSecureMailerRequiresTLSOnSubmissionPorts(t *testing.T) {
	for _, host := range []string{"smtp.example.com:465", "smtp.example.com:587"} {
		m, err := NewSecureMailer(&settings.SMTPConfig{SMTPHost: host, SMTPSkipTLSVerify: true})
		require.NoError(t, err)
		assert.False(t, m.dialer.TLSConfig.InsecureSkipVerify)
		assert.Equal(t, gomail.MandatoryStartTLS, m.dialer.StartTLSPolicy)
		assert.Equal(t, host == "smtp.example.com:465", m.dialer.SSL)
	}
}
