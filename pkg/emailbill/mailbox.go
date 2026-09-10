package emailbill

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/textproto"
	"sort"
	"strconv"
	"strings"
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
	seenMessages  map[string]struct{}
	observer      ScanObserver
	handler       func(Message) error
	folder        string
	uidValidity   uint32
	matchers      []ParserMatcher
}

// NewIMAPMailbox creates a read-only IMAP mailbox client.
func NewIMAPMailbox(config MailboxConfig, parsers []Parser) *IMAPMailbox {
	return &IMAPMailbox{config: config, parsers: parsers}
}

// SetMessageFilter installs a metadata-only filter that runs before body download.
func (m *IMAPMailbox) SetMessageFilter(filter MessageFilter) {
	m.messageFilter = filter
}

func (m *IMAPMailbox) SetParserMatchers(matchers []ParserMatcher) { m.matchers = matchers }

// TestConnection verifies TLS, credentials, and read-only INBOX access.
func (m *IMAPMailbox) TestConnection(ctx context.Context) error {
	imapClient, err := m.openInbox(ctx)
	if err != nil {
		return err
	}
	_ = imapClient.Logout()
	return nil
}

// FetchRecent scans all selectable folders with one shared processing limit.
func (m *IMAPMailbox) FetchRecent(ctx context.Context) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	imapClient, err := m.openConnection(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = imapClient.Logout() }()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			_ = imapClient.Terminate()
		case <-finished:
		}
	}()
	m.seenMessages = make(map[string]struct{})

	folders, err := listMailboxFolders(imapClient)
	if err != nil {
		return nil, err
	}
	for _, folder := range folders {
		if err := m.report(ScanEvent{Kind: "folder", Folder: folder, Status: "waiting"}); err != nil {
			return nil, err
		}
	}
	var messages []Message
	for _, folder := range folders {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := uint32(0)
		if m.config.MaxEmails > 0 {
			remaining = m.config.MaxEmails - uint32(len(messages))
		}
		batch, err := m.fetchFolder(ctx, imapClient, folder, remaining)
		if err != nil {
			return nil, fmt.Errorf("fetch IMAP folder %q: %w", folder, err)
		}
		messages = append(messages, batch...)
		if m.config.MaxEmails > 0 && uint32(len(messages)) >= m.config.MaxEmails {
			break
		}
	}
	return messages, nil
}

func (m *IMAPMailbox) fetchFolder(ctx context.Context, imapClient *client.Client, folder string, limit uint32) ([]Message, error) {
	m.folder = folder
	if err := m.report(ScanEvent{Kind: "folder", Folder: folder, Status: "opening"}); err != nil {
		return nil, err
	}
	status, err := imapClient.Select(folder, true)
	if err != nil {
		return nil, err
	}
	m.uidValidity = status.UidValidity
	if err := m.report(ScanEvent{Kind: "folder", Folder: folder, Status: "scanning", Total: status.Messages}); err != nil {
		return nil, err
	}
	if status.Messages == 0 {
		return nil, m.report(ScanEvent{Kind: "folder", Folder: folder, Status: "completed"})
	}

	uids, err := m.searchSupportedUIDs(imapClient)
	if err != nil {
		return nil, err
	}
	if len(uids) == 0 {
		return nil, m.report(ScanEvent{Kind: "folder", Folder: folder, Status: "completed"})
	}

	messages, err := fetchRecentMessageBatches(ctx, uids, limit, func(batch []uint32, remaining uint32) ([]Message, error) {
		messageDates, err := m.fetchMessageDatesWithinLimit(ctx, imapClient, batch)
		if err != nil || len(messageDates) == 0 {
			return nil, err
		}
		messageDates, err = m.fetchAuthenticatedHeaders(ctx, imapClient, sortedUIDs(messageDates), messageDates)
		if err != nil || len(messageDates) == 0 {
			return nil, err
		}
		messageDates = newestMessageDates(messageDates, remaining)
		messages, err := m.fetchAuthenticatedBodies(ctx, imapClient, sortedUIDs(messageDates), messageDates)
		// No IMAP response channel is active while parsing or calling the LLM.
		if m.handler != nil {
			for index, message := range messages {
				if err := m.handler(message); err != nil {
					return nil, err
				}
				// Preserve the global count without retaining every downloaded body.
				messages[index].Text, messages[index].Headers = "", nil
			}
		}
		return messages, err
	})
	if err != nil {
		return nil, err
	}
	outcome := "completed"
	if limit > 0 && uint32(len(messages)) >= limit {
		outcome = "limited"
	}
	return messages, m.report(ScanEvent{Kind: "folder", Folder: folder, Status: outcome})
}

func listMailboxFolders(imapClient *client.Client) ([]string, error) {
	listed := make(chan *imap.MailboxInfo)
	done := make(chan error, 1)
	go func() { done <- imapClient.List("", "*", listed) }()
	var infos []*imap.MailboxInfo
	for info := range listed {
		infos = append(infos, info)
	}
	if err := <-done; err != nil {
		return nil, fmt.Errorf("list IMAP folders: %w", err)
	}
	return selectableMailboxNames(infos), nil
}

func selectableMailboxNames(infos []*imap.MailboxInfo) []string {
	var names []string
	seen := make(map[string]bool)
	for _, info := range infos {
		if info == nil || info.Name == "" {
			continue
		}
		selectable := true
		for _, attr := range info.Attributes {
			if strings.EqualFold(attr, imap.NoSelectAttr) {
				selectable = false
			}
		}
		name := imap.CanonicalMailboxName(info.Name)
		if selectable && !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	// Scan user filing folders before the often much larger inbox and system folders.
	sort.Slice(names, func(i, j int) bool {
		left, right := mailboxScanPriority(names[i]), mailboxScanPriority(names[j])
		if left != right {
			return left < right
		}
		return names[i] < names[j]
	})
	return names
}

func mailboxScanPriority(name string) int {
	switch strings.ToLower(name) {
	case "inbox":
		return 1
	case "sent", "sent messages", "drafts", "deleted messages", "trash", "junk", "spam":
		return 2
	default:
		return 0
	}
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
	imapClient, err := m.openConnection(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = imapClient.Select("INBOX", true); err != nil {
		_ = imapClient.Logout()
		return nil, fmt.Errorf("select IMAP inbox: %w", err)
	}
	return imapClient, nil
}

func (m *IMAPMailbox) openConnection(ctx context.Context) (*client.Client, error) {
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
	return imapClient, nil
}

func (m *IMAPMailbox) fetchMessageDatesWithinLimit(ctx context.Context, imapClient *client.Client, uids []uint32) (map[uint32]time.Time, error) {
	sequenceSet := sequenceSetForUIDs(uids)
	fetched := make(chan *imap.Message, len(uids))
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
				message := m.located(Message{ReceivedAt: fetchedMessage.InternalDate}, fetchedMessage.Uid)
				if err := m.report(ScanEvent{Kind: "message", Message: &message, Status: "oversized", Reason: "Message exceeds the configured size limit", Scanned: true}); err != nil {
					return nil, err
				}
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
	fetched := make(chan *imap.Message, len(uids))
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
				message := m.located(Message{ReceivedAt: messageDates[fetchedMessage.Uid]}, fetchedMessage.Uid)
				if err := m.report(ScanEvent{Kind: "message", Message: &message, Status: "failed", Reason: "IMAP returned no message headers", Scanned: true}); err != nil {
					return nil, err
				}
				continue
			}
			decoded, decodeErr := DecodeMessageHeadersWithLimit(header, m.config.Security, messageDates[fetchedMessage.Uid], m.maxMessageBytes())
			decoded = m.located(decoded, fetchedMessage.Uid)
			status, reason := "ready", ""
			if decodeErr != nil {
				status, reason = "failed", decodeErr.Error()
			} else if !decoded.Authenticated {
				status, reason = "rejected", "Email authentication failed"
			} else if !m.supported(decoded) {
				status, reason = "not_matched", "No enabled parser rule matches this sender and subject"
			}
			if status != "ready" {
				if err := m.report(ScanEvent{Kind: "message", Message: &decoded, Status: status, Reason: reason, Scanned: true}); err != nil {
					return nil, err
				}
				continue
			}
			if decoded.MessageID != "" {
				if _, seen := m.seenMessages[mailboxMessageIdentity(decoded)]; seen {
					if err := m.report(ScanEvent{Kind: "message", Message: &decoded, Status: "duplicate", Reason: "Message already downloaded from another folder", Scanned: true}); err != nil {
						return nil, err
					}
					continue
				}
			}
			if m.messageFilter != nil {
				include, filterErr := m.messageFilter(ctx, decoded)
				if filterErr != nil {
					return nil, fmt.Errorf("filter IMAP message: %w", filterErr)
				}
				if !include {
					if err := m.report(ScanEvent{Kind: "message", Message: &decoded, Status: "duplicate", Reason: "Message already processed", Scanned: true}); err != nil {
						return nil, err
					}
					continue
				}
			}
			if err := m.report(ScanEvent{Kind: "message", Message: &decoded, Status: "ready", Scanned: true}); err != nil {
				return nil, err
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
	fetched := make(chan *imap.Message, len(uids))
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
					return messages, fmt.Errorf("fetch IMAP message bodies: %w", err)
				}
				return messages, nil
			}
			if fetchedMessage == nil {
				continue
			}
			body := fetchedMessage.GetBody(section)
			if body == nil {
				message := m.located(Message{ReceivedAt: messageDates[fetchedMessage.Uid]}, fetchedMessage.Uid)
				if err := m.report(ScanEvent{Kind: "message", Message: &message, Status: "failed", Reason: "IMAP returned no message body"}); err != nil {
					return messages, err
				}
				continue
			}
			decoded, decodeErr := DecodeMessageWithLimit(body, m.config.Security, messageDates[fetchedMessage.Uid], m.maxMessageBytes())
			decoded = m.located(decoded, fetchedMessage.Uid)
			if decodeErr != nil || !decoded.Authenticated || !m.supported(decoded) {
				reason := "Email authentication or parser matching failed after downloading"
				if decodeErr != nil {
					reason = decodeErr.Error()
				}
				if err := m.report(ScanEvent{Kind: "message", Message: &decoded, Status: "failed", Reason: reason}); err != nil {
					return nil, err
				}
				continue
			}
			if m.acceptFetchedMessage(decoded) {
				if err := m.report(ScanEvent{Kind: "message", Message: &decoded, Status: "downloaded", Downloaded: true}); err != nil {
					return nil, err
				}
				messages = append(messages, decoded)
			} else if err := m.report(ScanEvent{Kind: "message", Message: &decoded, Status: "duplicate", Reason: "Message already downloaded"}); err != nil {
				return messages, err
			}
		}
	}
}

func mailboxMessageIdentity(message Message) string {
	identity, _ := MessageFingerprint(MessageIdentityInput{
		MessageID: message.MessageID, Sender: message.Sender, Subject: message.Subject,
		ReceivedAt: message.ReceivedAt, Body: message.Text,
	})
	return identity
}

func (m *IMAPMailbox) acceptFetchedMessage(message Message) bool {
	if m.seenMessages == nil {
		m.seenMessages = make(map[string]struct{})
	}
	identity := mailboxMessageIdentity(message)
	if _, seen := m.seenMessages[identity]; seen {
		return false
	}
	m.seenMessages[identity] = struct{}{}
	return true
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
	if m.matchers != nil {
		for _, matcher := range m.matchers {
			if matcher.Matches(ScriptMail{Sender: message.Sender, Subject: message.Subject}) {
				return true
			}
		}
		return false
	}
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
