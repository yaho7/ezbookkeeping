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
const imapFetchBatchSize = 50

// Mailbox retrieves supported messages from an email account.
type Mailbox interface {
	FetchRecent(context.Context) ([]Message, error)
}

// MessageFilter decides whether a matching message should have its body downloaded.
type MessageFilter func(context.Context, Message) (bool, error)

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
	config        MailboxConfig
	parsers       []Parser
	messageFilter MessageFilter
}

// NewIMAPMailbox creates a read-only IMAP mailbox client.
func NewIMAPMailbox(config MailboxConfig, parsers []Parser) *IMAPMailbox {
	return &IMAPMailbox{config: config, parsers: parsers}
}

// SetMessageFilter installs a metadata-only filter that runs before body download.
func (m *IMAPMailbox) SetMessageFilter(filter MessageFilter) {
	m.messageFilter = filter
}

// TestConnection verifies TLS, credentials, and read-only INBOX access.
func (m *IMAPMailbox) TestConnection(ctx context.Context) error {
	imapClient, err := m.openInbox(ctx)
	if err != nil {
		return err
	}
	_ = imapClient.Logout()
	return nil
}

// FetchRecent fetches up to MaxEmails recent messages for each supported sender/subject pair.
func (m *IMAPMailbox) FetchRecent(ctx context.Context) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	imapClient, err := m.openInbox(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = imapClient.Logout() }()

	uids, err := m.searchSupportedUIDs(imapClient)
	if err != nil {
		return nil, err
	}
	if len(uids) == 0 {
		return nil, nil
	}

	return fetchRecentMessageBatches(ctx, uids, m.config.MaxEmails, func(batch []uint32, remaining uint32) ([]Message, error) {
		messageDates, err := m.fetchMessageDatesWithinLimit(ctx, imapClient, batch)
		if err != nil || len(messageDates) == 0 {
			return nil, err
		}
		messageDates, err = m.fetchAuthenticatedHeaders(ctx, imapClient, sortedUIDs(messageDates), messageDates)
		if err != nil || len(messageDates) == 0 {
			return nil, err
		}
		messageDates = newestMessageDates(messageDates, remaining)
		return m.fetchAuthenticatedBodies(ctx, imapClient, sortedUIDs(messageDates), messageDates)
	})
}

// fetchRecentMessageBatches bounds each IMAP command and stops once enough
// unprocessed messages have been found, even when the inbox contains years of mail.
func fetchRecentMessageBatches(ctx context.Context, uids []uint32, limit uint32, fetch func([]uint32, uint32) ([]Message, error)) ([]Message, error) {
	uids = append([]uint32(nil), uids...)
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	var messages []Message
	for end := len(uids); end > 0; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		start := max(0, end-imapFetchBatchSize)
		remaining := uint32(0)
		if limit > 0 {
			remaining = limit - uint32(len(messages))
		}
		batch, err := fetch(uids[start:end], remaining)
		if err != nil {
			return nil, err
		}
		messages = append(messages, batch...)
		if limit > 0 && uint32(len(messages)) >= limit {
			break
		}
		end = start
	}
	return messages, nil
}

func (m *IMAPMailbox) openInbox(ctx context.Context) (*client.Client, error) {
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
	imapClient.Timeout = defaultIMAPTimeout

	if err = imapClient.Login(m.config.Username, m.config.Password); err != nil {
		_ = imapClient.Logout()
		return nil, fmt.Errorf("login to IMAP server: %w", err)
	}
	if _, err = imapClient.Select("INBOX", true); err != nil {
		_ = imapClient.Logout()
		return nil, fmt.Errorf("select IMAP inbox: %w", err)
	}
	return imapClient, nil
}

func (m *IMAPMailbox) fetchMessageDatesWithinLimit(ctx context.Context, imapClient *client.Client, uids []uint32) (map[uint32]time.Time, error) {
	sequenceSet := sequenceSetForUIDs(uids)
	fetched := make(chan *imap.Message)
	fetchErr := make(chan error, 1)
	go func() {
		fetchErr <- imapClient.UidFetch(sequenceSet, []imap.FetchItem{
			imap.FetchUid,
			imap.FetchInternalDate,
			imap.FetchRFC822Size,
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
					return nil, fmt.Errorf("fetch IMAP message sizes: %w", err)
				}
				return messageDates, nil
			}
			if fetchedMessage == nil {
				continue
			}
			if fetchedMessage.Size > m.maxMessageBytes() {
				continue
			}
			messageDates[fetchedMessage.Uid] = fetchedMessage.InternalDate
		}
	}
}

func (m *IMAPMailbox) fetchAuthenticatedHeaders(ctx context.Context, imapClient *client.Client, uids []uint32, messageDates map[uint32]time.Time) (map[uint32]time.Time, error) {
	sequenceSet := sequenceSetForUIDs(uids)
	headerSection := &imap.BodySectionName{
		BodyPartName: imap.BodyPartName{
			Specifier: imap.HeaderSpecifier,
			Fields:    []string{"From", "Subject", "Message-ID", "Date", "Authentication-Results"},
		},
		Peek:    true,
		Partial: []int{0, int(m.maxMessageBytes()) + 1},
	}
	fetched := make(chan *imap.Message)
	fetchErr := make(chan error, 1)
	go func() {
		fetchErr <- imapClient.UidFetch(sequenceSet, []imap.FetchItem{
			imap.FetchUid,
			headerSection.FetchItem(),
		}, fetched)
	}()

	authenticatedDates := make(map[uint32]time.Time, len(uids))
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case fetchedMessage, ok := <-fetched:
			if !ok {
				if err := <-fetchErr; err != nil {
					return nil, fmt.Errorf("fetch IMAP message headers: %w", err)
				}
				return authenticatedDates, nil
			}
			if fetchedMessage == nil {
				continue
			}
			header := fetchedMessage.GetBody(headerSection)
			if header == nil {
				continue
			}
			decoded, decodeErr := DecodeMessageHeadersWithLimit(header, m.config.Security, messageDates[fetchedMessage.Uid], m.maxMessageBytes())
			if decodeErr != nil || !decoded.Authenticated || !m.supported(decoded) {
				continue
			}
			if m.messageFilter != nil {
				include, filterErr := m.messageFilter(ctx, decoded)
				if filterErr != nil {
					return nil, fmt.Errorf("filter IMAP message: %w", filterErr)
				}
				if !include {
					continue
				}
			}
			authenticatedDates[fetchedMessage.Uid] = messageDates[fetchedMessage.Uid]
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

func sortedUIDs(messageDates map[uint32]time.Time) []uint32 {
	uids := make([]uint32, 0, len(messageDates))
	for uid := range messageDates {
		uids = append(uids, uid)
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	return uids
}

func (m *IMAPMailbox) searchSupportedUIDs(imapClient *client.Client) ([]uint32, error) {
	unique := make(map[uint32]struct{})
	if len(m.parsers) == 0 {
		criteria := imap.NewSearchCriteria()
		uids, err := imapClient.UidSearch(criteria)
		if err != nil {
			return nil, fmt.Errorf("search recent IMAP messages: %w", err)
		}
		return uids, nil
	}
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
				for _, uid := range uids {
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

func newestMessageDates(messageDates map[uint32]time.Time, limit uint32) map[uint32]time.Time {
	if limit == 0 || uint32(len(messageDates)) <= limit {
		return messageDates
	}
	uids := sortedUIDs(messageDates)
	limited := make(map[uint32]time.Time, limit)
	for _, uid := range uids[len(uids)-int(limit):] {
		limited[uid] = messageDates[uid]
	}
	return limited
}

func (m *IMAPMailbox) supported(message Message) bool {
	if len(m.parsers) == 0 {
		return true
	}
	for _, parser := range m.parsers {
		if parser.Matches(message.Sender, message.Subject) {
			return true
		}
	}
	return false
}
