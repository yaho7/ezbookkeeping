package emailbill

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

var (
	dkimDomainPattern = regexp.MustCompile(`header\.(?:d|i)\s*=\s*([^\s;]+)`)
	spfSenderPattern  = regexp.MustCompile(`smtp\.(?:mailfrom|helo)\s*=\s*([^\s;]+)`)
)

// DefaultMaxMessageBytes is the maximum RFC 5322 message size decoded by default.
const DefaultMaxMessageBytes uint32 = 2 * 1024 * 1024

// MessageSecurity controls validation of received Authentication-Results headers.
type MessageSecurity struct {
	RequireAuthenticationResults bool
	TrustedAuthservDomains       []string
}

// DecodeMessage decodes one RFC 5322 message into parser input.
func DecodeMessage(reader io.Reader, security MessageSecurity, fallbackTime time.Time) (Message, error) {
	return DecodeMessageWithLimit(reader, security, fallbackTime, DefaultMaxMessageBytes)
}

// DecodeMessageWithLimit decodes one RFC 5322 message without reading beyond maxBytes.
func DecodeMessageWithLimit(reader io.Reader, security MessageSecurity, fallbackTime time.Time, maxBytes uint32) (Message, error) {
	return decodeMessageWithLimit(reader, security, fallbackTime, maxBytes, false)
}

// DecodeMessageHeadersWithLimit decodes IMAP header-only responses without trying
// to parse a MIME body that has not been downloaded yet.
func DecodeMessageHeadersWithLimit(reader io.Reader, security MessageSecurity, fallbackTime time.Time, maxBytes uint32) (Message, error) {
	return decodeMessageWithLimit(reader, security, fallbackTime, maxBytes, true)
}

func decodeMessageWithLimit(reader io.Reader, security MessageSecurity, fallbackTime time.Time, maxBytes uint32, headersOnly bool) (Message, error) {
	limitedReader := &io.LimitedReader{R: reader, N: int64(maxBytes) + 1}
	content, err := io.ReadAll(limitedReader)
	if err != nil {
		return Message{}, fmt.Errorf("read email message: %w", err)
	}
	if uint64(len(content)) > uint64(maxBytes) {
		return Message{}, fmt.Errorf("email message exceeds size limit of %d bytes", maxBytes)
	}
	return decodeMessage(bytes.NewReader(content), security, fallbackTime, headersOnly)
}

func decodeMessage(reader io.Reader, security MessageSecurity, fallbackTime time.Time, headersOnly bool) (Message, error) {
	mailMessage, err := mail.ReadMessage(reader)
	if err != nil {
		return Message{}, fmt.Errorf("read email message: %w", err)
	}

	decoder := &mime.WordDecoder{CharsetReader: charset.NewReaderLabel}
	subject, err := decoder.DecodeHeader(mailMessage.Header.Get("Subject"))
	if err != nil {
		return Message{}, fmt.Errorf("decode email subject: %w", err)
	}

	sender := mailMessage.Header.Get("From")
	if address, parseErr := mail.ParseAddress(sender); parseErr == nil {
		sender = strings.ToLower(address.Address)
	} else {
		sender = strings.ToLower(strings.TrimSpace(sender))
	}

	text := ""
	if !headersOnly {
		text, err = decodeMIMEBody(mailMessage.Header, mailMessage.Body)
		if err != nil {
			return Message{}, err
		}
	}

	receivedAt := fallbackTime
	if parsedDate, dateErr := mailMessage.Header.Date(); dateErr == nil {
		receivedAt = parsedDate
	}
	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}

	messageID := strings.TrimSpace(mailMessage.Header.Get("Message-ID"))
	fingerprint := messageID
	if fingerprint == "" {
		digest := sha256.Sum256([]byte(receivedAt.Format(time.RFC3339Nano) + "\x00" + subject + "\x00" + text))
		fingerprint = fmt.Sprintf("sha256:%x", digest[:])
	}

	return Message{
		Fingerprint:   fingerprint,
		MessageID:     messageID,
		Sender:        sender,
		Subject:       subject,
		Text:          strings.TrimSpace(text),
		Headers:       safeParserHeaders(mailMessage.Header),
		ReceivedAt:    receivedAt,
		Authenticated: authenticationResultsValid(mailMessage.Header.Get("Authentication-Results"), security),
	}, nil
}

func safeParserHeaders(header mail.Header) map[string]string {
	allowed := []string{"From", "To", "Cc", "Reply-To", "Date", "Subject", "Message-ID", "Authentication-Results"}
	result := make(map[string]string, len(allowed))
	for _, key := range allowed {
		if value := strings.TrimSpace(header.Get(key)); value != "" {
			result[strings.ToLower(key)] = value
		}
	}
	return result
}

func decodeMIMEBody(header mail.Header, body io.Reader) (string, error) {
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return "", fmt.Errorf("multipart email has no boundary")
		}

		multipartReader := multipart.NewReader(body, boundary)
		var plainParts []string
		var htmlParts []string
		for {
			part, partErr := multipartReader.NextPart()
			if partErr == io.EOF {
				break
			}
			if partErr != nil {
				return "", fmt.Errorf("read MIME part: %w", partErr)
			}

			partText, decodeErr := decodeMIMEBody(mail.Header(part.Header), part)
			_ = part.Close()
			if decodeErr != nil {
				return "", decodeErr
			}
			partMediaType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if strings.HasPrefix(partMediaType, "text/html") {
				htmlParts = append(htmlParts, partText)
			} else if partText != "" {
				plainParts = append(plainParts, partText)
			}
		}
		if len(plainParts) > 0 {
			return strings.Join(plainParts, "\n"), nil
		}
		return strings.Join(htmlParts, "\n"), nil
	}

	if !strings.HasPrefix(mediaType, "text/plain") && !strings.HasPrefix(mediaType, "text/html") {
		return "", nil
	}

	decodedBody := transferDecodedReader(body, header.Get("Content-Transfer-Encoding"))
	if charsetName := params["charset"]; charsetName != "" {
		decodedBody, err = charset.NewReaderLabel(charsetName, decodedBody)
		if err != nil {
			return "", fmt.Errorf("decode email charset %q: %w", charsetName, err)
		}
	}
	content, err := io.ReadAll(decodedBody)
	if err != nil {
		return "", fmt.Errorf("read email body: %w", err)
	}
	if strings.HasPrefix(mediaType, "text/html") {
		return htmlToText(string(content)), nil
	}
	return string(content), nil
}

func transferDecodedReader(reader io.Reader, encoding string) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, reader)
	case "quoted-printable":
		return quotedprintable.NewReader(reader)
	default:
		return reader
	}
}

func htmlToText(content string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(content))
	var text strings.Builder
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}
		if tokenType == html.TextToken {
			value := strings.TrimSpace(string(tokenizer.Text()))
			if value != "" {
				if text.Len() > 0 {
					text.WriteByte(' ')
				}
				text.WriteString(value)
			}
		}
	}
	return text.String()
}

func authenticationResultsValid(header string, security MessageSecurity) bool {
	if !security.RequireAuthenticationResults {
		return true
	}
	parts := strings.Split(strings.ToLower(header), ";")
	authservFields := strings.Fields(parts[0])
	if len(parts) < 2 || len(authservFields) == 0 || !domainMatchesAny(authservFields[0], security.TrustedAuthservDomains) {
		return false
	}

	for _, result := range parts[1:] {
		if strings.Contains(result, "dkim=pass") {
			if match := dkimDomainPattern.FindStringSubmatch(result); match != nil && domainMatches(strings.TrimPrefix(match[1], "@"), "cmbchina.com") {
				return true
			}
		}
		if strings.Contains(result, "spf=pass") {
			if match := spfSenderPattern.FindStringSubmatch(result); match != nil {
				identity := strings.Trim(match[1], "<>\"")
				if at := strings.LastIndex(identity, "@"); at >= 0 {
					identity = identity[at+1:]
				}
				if domainMatches(identity, "cmbchina.com") {
					return true
				}
			}
		}
	}
	return false
}

func domainMatchesAny(domain string, allowed []string) bool {
	for _, candidate := range allowed {
		if domainMatches(domain, candidate) {
			return true
		}
	}
	return false
}

func domainMatches(domain string, suffix string) bool {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	suffix = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(suffix)), ".")
	return domain == suffix || strings.HasSuffix(domain, "."+suffix)
}
