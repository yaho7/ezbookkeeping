package emailbill

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/textproto"
	"sort"
	"strconv"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

const defaultIMAPTimeout = 30 * time.Second

// Mailbox retrieves supported messages from an email account.
type Mailbox interface {
	FetchRecent(context.Context) ([]Message, error)
}

// MailboxConfig contains the connection and security settings used by IMAPMailbox.
type MailboxConfig struct {
	Server    string
	Port      uint16
	Username  string
	Password  string
	MaxEmails uint32
	Security  MessageSecurity
}

// IMAPMailbox fetches supported CMB emails over IMAPS without changing message flags.
type IMAPMailbox struct {
	config  MailboxConfig
	parsers []Parser
}

// NewIMAPMailbox creates a read-only IMAP mailbox client.
func NewIMAPMailbox(config MailboxConfig, parsers []Parser) *IMAPMailbox {
	return &IMAPMailbox{config: config, parsers: parsers}
}

// FetchRecent fetches up to MaxEmails recent messages for each supported sender/subject pair.
func (m *IMAPMailbox) FetchRecent(ctx context.Context) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	address := net.JoinHostPort(m.config.Server, strconv.FormatUint(uint64(m.config.Port), 10))
	dialer := &net.Dialer{Timeout: defaultIMAPTimeout}
	imapClient, err := client.DialWithDialerTLS(dialer, address, &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: m.config.Server,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to IMAP server: %w", err)
	}
	defer func() { _ = imapClient.Logout() }()
	imapClient.Timeout = defaultIMAPTimeout

	if err = imapClient.Login(m.config.Username, m.config.Password); err != nil {
		return nil, fmt.Errorf("login to IMAP server: %w", err)
	}
	if _, err = imapClient.Select("INBOX", true); err != nil {
		return nil, fmt.Errorf("select IMAP inbox: %w", err)
	}

	uids, err := m.searchSupportedUIDs(imapClient)
	if err != nil {
		return nil, err
	}
	if len(uids) == 0 {
		return nil, nil
	}

	sequenceSet := new(imap.SeqSet)
	sequenceSet.AddNum(uids...)
	section := &imap.BodySectionName{Peek: true}
	fetched := make(chan *imap.Message)
	fetchErr := make(chan error, 1)
	go func() {
		fetchErr <- imapClient.UidFetch(sequenceSet, []imap.FetchItem{
			imap.FetchUid,
			imap.FetchInternalDate,
			section.FetchItem(),
		}, fetched)
	}()

	messages := make([]Message, 0, len(uids))
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case fetchedMessage, ok := <-fetched:
			if !ok {
				if err = <-fetchErr; err != nil {
					return nil, fmt.Errorf("fetch IMAP messages: %w", err)
				}
				return messages, nil
			}
			if fetchedMessage == nil {
				continue
			}
			body := fetchedMessage.GetBody(section)
			if body == nil {
				continue
			}
			decoded, decodeErr := DecodeMessage(body, m.config.Security, fetchedMessage.InternalDate)
			if decodeErr != nil || !m.supported(decoded) {
				continue
			}
			messages = append(messages, decoded)
		}
	}
}

func (m *IMAPMailbox) searchSupportedUIDs(imapClient *client.Client) ([]uint32, error) {
	unique := make(map[uint32]struct{})
	for _, parser := range m.parsers {
		for _, sender := range parser.AllowedSenders() {
			for _, subject := range parser.SubjectKeywords() {
				criteria := imap.NewSearchCriteria()
				criteria.Header = textproto.MIMEHeader{}
				criteria.Header.Add("From", sender)
				criteria.Header.Add("Subject", subject)
				uids, err := imapClient.UidSearch(criteria)
				if err != nil {
					return nil, fmt.Errorf("search IMAP messages: %w", err)
				}
				for _, uid := range newestUIDs(uids, m.config.MaxEmails) {
					unique[uid] = struct{}{}
				}
			}
		}
	}

	uids := make([]uint32, 0, len(unique))
	for uid := range unique {
		uids = append(uids, uid)
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	return uids, nil
}

func newestUIDs(uids []uint32, limit uint32) []uint32 {
	if limit == 0 || uint32(len(uids)) <= limit {
		return uids
	}
	return uids[len(uids)-int(limit):]
}

func (m *IMAPMailbox) supported(message Message) bool {
	for _, parser := range m.parsers {
		if parser.Matches(message.Sender, message.Subject) {
			return true
		}
	}
	return false
}
