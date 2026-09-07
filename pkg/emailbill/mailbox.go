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
	Server          string
	Port            uint16
	Username        string
	Password        string
	MaxEmails       uint32
	MaxMessageBytes uint32
	Security        MessageSecurity
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

	messageDates, err := m.fetchAuthenticatedHeaders(ctx, imapClient, uids)
	if err != nil {
		return nil, err
	}
	if len(messageDates) == 0 {
		return nil, nil
	}

	authenticatedUIDs := make([]uint32, 0, len(messageDates))
	for uid := range messageDates {
		authenticatedUIDs = append(authenticatedUIDs, uid)
	}
	sort.Slice(authenticatedUIDs, func(i, j int) bool { return authenticatedUIDs[i] < authenticatedUIDs[j] })
	return m.fetchAuthenticatedBodies(ctx, imapClient, authenticatedUIDs, messageDates)
}

func (m *IMAPMailbox) fetchAuthenticatedHeaders(ctx context.Context, imapClient *client.Client, uids []uint32) (map[uint32]time.Time, error) {
	sequenceSet := sequenceSetForUIDs(uids)
	headerSection := &imap.BodySectionName{
		BodyPartName: imap.BodyPartName{Specifier: imap.HeaderSpecifier},
		Peek:         true,
	}
	fetched := make(chan *imap.Message)
	fetchErr := make(chan error, 1)
	go func() {
		fetchErr <- imapClient.UidFetch(sequenceSet, []imap.FetchItem{
			imap.FetchUid,
			imap.FetchInternalDate,
			imap.FetchRFC822Size,
			headerSection.FetchItem(),
		}, fetched)
	}()

	messageDates := make(map[uint32]time.Time, len(uids))
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case fetchedMessage, ok := <-fetched:
			if !ok {
				if err := <-fetchErr; err != nil {
					return nil, fmt.Errorf("fetch IMAP message headers: %w", err)
				}
				return messageDates, nil
			}
			if fetchedMessage == nil {
				continue
			}
			if fetchedMessage.Size > m.maxMessageBytes() {
				continue
			}
			header := fetchedMessage.GetBody(headerSection)
			if header == nil {
				continue
			}
			decoded, decodeErr := DecodeMessageWithLimit(header, m.config.Security, fetchedMessage.InternalDate, m.maxMessageBytes())
			if decodeErr != nil || !decoded.Authenticated || !m.supported(decoded) {
				continue
			}
			messageDates[fetchedMessage.Uid] = fetchedMessage.InternalDate
		}
	}
}

func (m *IMAPMailbox) fetchAuthenticatedBodies(ctx context.Context, imapClient *client.Client, uids []uint32, messageDates map[uint32]time.Time) ([]Message, error) {
	sequenceSet := sequenceSetForUIDs(uids)
	section := &imap.BodySectionName{
		Peek:    true,
		Partial: []int{0, int(m.maxMessageBytes()) + 1},
	}
	fetched := make(chan *imap.Message)
	fetchErr := make(chan error, 1)
	go func() {
		fetchErr <- imapClient.UidFetch(sequenceSet, []imap.FetchItem{
			imap.FetchUid,
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
				if err := <-fetchErr; err != nil {
					return nil, fmt.Errorf("fetch IMAP message bodies: %w", err)
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
			decoded, decodeErr := DecodeMessageWithLimit(body, m.config.Security, messageDates[fetchedMessage.Uid], m.maxMessageBytes())
			if decodeErr != nil || !decoded.Authenticated || !m.supported(decoded) {
				continue
			}
			messages = append(messages, decoded)
		}
	}
}

func (m *IMAPMailbox) maxMessageBytes() uint32 {
	if m.config.MaxMessageBytes == 0 {
		return DefaultMaxMessageBytes
	}
	return m.config.MaxMessageBytes
}

func sequenceSetForUIDs(uids []uint32) *imap.SeqSet {
	sequenceSet := new(imap.SeqSet)
	sequenceSet.AddNum(uids...)
	return sequenceSet
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
