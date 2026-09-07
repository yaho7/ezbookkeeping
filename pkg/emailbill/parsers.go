package emailbill

import (
	"fmt"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	creditDatePattern        = regexp.MustCompile(`\d{4}/\d{2}/\d{2}`)
	creditTransactionPattern = regexp.MustCompile(`(?s)(\d{2}:\d{2}:\d{2})\s+CNY\s+(-?\d+(?:\.\d+)?)\s+(.+?)(?:\(每日邮件\)|\(Daily Email\))`)
	creditCardPrefixPattern  = regexp.MustCompile(`尾号\d+\s*(?:消费|退货|预授权完成)\s*`)
	shortDatePattern         = regexp.MustCompile(`^(\d{2})月(\d{2})日$`)
	moneyPattern             = regexp.MustCompile(`^(-?)(\d+)(?:\.(\d+))?$`)
)

type cmbCreditParser struct{}

// NewCMBCreditParser returns the parser for CMB daily credit-card emails.
func NewCMBCreditParser() Parser {
	return &cmbCreditParser{}
}

func (p *cmbCreditParser) AllowedSenders() []string {
	return []string{"ccsvc@message.cmbchina.com", "95555@message.cmbchina.com"}
}

func (p *cmbCreditParser) SubjectKeywords() []string {
	return []string{"每日信用管家"}
}

func (p *cmbCreditParser) Matches(sender string, subject string) bool {
	return matchesMail(sender, subject, p.AllowedSenders(), p.SubjectKeywords())
}

func (p *cmbCreditParser) Parse(text string, receivedAt time.Time) ([]ParsedTransaction, error) {
	dateText := receivedAt.Format("2006/01/02")
	if match := creditDatePattern.FindString(text); match != "" {
		dateText = match
	}

	matches := creditTransactionPattern.FindAllStringSubmatch(text, -1)
	transactions := make([]ParsedTransaction, 0, len(matches))

	for _, match := range matches {
		amountMinor, err := moneyToMinor(match[2])
		if err != nil {
			return nil, err
		}

		merchant := strings.TrimSpace(creditCardPrefixPattern.ReplaceAllString(strings.Join(strings.Fields(match[3]), " "), ""))
		if merchant == "" || amountMinor == 0 {
			continue
		}

		occurredAt, err := transactionDateTime(dateText, match[1], receivedAt)
		if err != nil {
			return nil, err
		}

		transactions = append(transactions, ParsedTransaction{
			Source:      SourceCMBCredit,
			OccurredAt:  occurredAt,
			AmountMinor: -amountMinor,
			Merchant:    merchant,
			Description: strings.Join(strings.Fields(match[3]), " "),
		})
	}

	return transactions, nil
}

type debitRule struct {
	pattern        *regexp.Regexp
	dateGroup      int
	timeGroup      int
	merchantGroup  int
	amountGroup    int
	merchantPrefix bool
	sign           int64
	label          string
}

type cmbDebitParser struct {
	rules []debitRule
}

// NewCMBDebitParser returns the parser for CMB debit-card notifications.
func NewCMBDebitParser() Parser {
	return &cmbDebitParser{rules: []debitRule{
		{
			pattern:   regexp.MustCompile(`(?s)于(\d{2}月\d{2}日)(\d{2}:\d{2}).*?在(.*?)(?:快捷支付|支付|消费)(?:人民币)?(\d+(?:\.\d+)?)元`),
			dateGroup: 1, timeGroup: 2, merchantGroup: 3, amountGroup: 4, sign: -1, label: "支出",
		},
		{
			pattern:   regexp.MustCompile(`(?s)于(\d{2}月\d{2}日)(\d{2}:\d{2}).*?向(.*?)(?:转账|汇款)(?:人民币)?(\d+(?:\.\d+)?)元`),
			dateGroup: 1, timeGroup: 2, merchantGroup: 3, amountGroup: 4, sign: -1, label: "转账支出",
		},
		{
			pattern:   regexp.MustCompile(`(?s)于(\d{2}月\d{2}日)(\d{2}:\d{2}).*?(?:在(.*?))?取出(?:人民币)?(\d+(?:\.\d+)?)元`),
			dateGroup: 1, timeGroup: 2, merchantGroup: 3, amountGroup: 4, sign: -1, label: "取款",
		},
		{
			pattern:   regexp.MustCompile(`于(\d{2}月\d{2}日)(\d{2}:\d{2})([^。；]*?)(?:入账|存入|退款)(?:人民币)?(\d+(?:\.\d+)?)元`),
			dateGroup: 1, timeGroup: 2, merchantGroup: 3, amountGroup: 4, sign: 1, label: "入账",
		},
		{
			pattern:   regexp.MustCompile(`(?s)(资金归集[^。；]*?).*?(?:向|从|由|转账).*?(?:人民币)?(\d+(?:\.\d+)?)元.*?截至(\d{2}月\d{2}日)(\d{2}:\d{2})`),
			dateGroup: 3, timeGroup: 4, merchantGroup: 1, amountGroup: 2, merchantPrefix: true, sign: 1, label: "入账",
		},
	}}
}

func (p *cmbDebitParser) AllowedSenders() []string {
	return []string{"95555@message.cmbchina.com"}
}

func (p *cmbDebitParser) SubjectKeywords() []string {
	return []string{"通知"}
}

func (p *cmbDebitParser) Matches(sender string, subject string) bool {
	return matchesMail(sender, subject, p.AllowedSenders(), p.SubjectKeywords())
}

func (p *cmbDebitParser) Parse(text string, receivedAt time.Time) ([]ParsedTransaction, error) {
	transactions := make([]ParsedTransaction, 0)
	matchedRanges := make([][2]int, 0)

	for _, rule := range p.rules {
		matches := rule.pattern.FindAllStringSubmatchIndex(text, -1)
		for _, indexes := range matches {
			start, end := indexes[0], indexes[1]
			if overlapsAny(start, end, matchedRanges) {
				continue
			}
			matchedRanges = append(matchedRanges, [2]int{start, end})

			group := func(index int) string {
				groupStart, groupEnd := indexes[index*2], indexes[index*2+1]
				if groupStart < 0 || groupEnd < 0 {
					return ""
				}
				return text[groupStart:groupEnd]
			}

			merchant := strings.Trim(group(rule.merchantGroup), " ，,")
			if merchant == "" {
				merchant = "招商银行"
			} else if rule.merchantPrefix {
				merchantRunes := []rune(merchant)
				if len(merchantRunes) > 20 {
					merchantRunes = merchantRunes[:20]
				}
				merchant = "招行-" + string(merchantRunes)
			}

			amountMinor, err := moneyToMinor(group(rule.amountGroup))
			if err != nil {
				return nil, err
			}
			amountMinor *= rule.sign
			if amountMinor == 0 {
				continue
			}

			occurredAt, err := transactionDateTime(group(rule.dateGroup), group(rule.timeGroup), receivedAt)
			if err != nil {
				return nil, err
			}

			transactions = append(transactions, ParsedTransaction{
				Source:      SourceCMBDebit,
				OccurredAt:  occurredAt,
				AmountMinor: amountMinor,
				Merchant:    merchant,
				Description: merchant + " - " + rule.label,
			})
		}
	}

	return transactions, nil
}

func matchesMail(sender string, subject string, allowedSenders []string, subjectKeywords []string) bool {
	address := strings.ToLower(strings.TrimSpace(sender))
	if parsedAddress, err := mail.ParseAddress(sender); err == nil {
		address = strings.ToLower(parsedAddress.Address)
	}

	allowed := false
	for _, candidate := range allowedSenders {
		if address == candidate {
			allowed = true
			break
		}
	}
	if !allowed {
		return false
	}

	for _, keyword := range subjectKeywords {
		if strings.Contains(subject, keyword) {
			return true
		}
	}
	return false
}

func moneyToMinor(value string) (int64, error) {
	match := moneyPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return 0, fmt.Errorf("invalid money value %q", value)
	}

	whole, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid money value %q: %w", value, err)
	}

	fraction := match[3] + "000"
	cents, err := strconv.ParseInt(fraction[:2], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid money value %q: %w", value, err)
	}
	if fraction[2] >= '5' {
		cents++
		if cents == 100 {
			whole++
			cents = 0
		}
	}

	minor := whole*100 + cents
	if match[1] == "-" {
		minor = -minor
	}
	return minor, nil
}

func transactionDateTime(dateText string, timeText string, receivedAt time.Time) (time.Time, error) {
	location := receivedAt.Location()
	if strings.Contains(dateText, "/") {
		return time.ParseInLocation("2006/01/02 15:04:05", dateText+" "+normalizeTime(timeText), location)
	}

	dateMatch := shortDatePattern.FindStringSubmatch(dateText)
	if dateMatch == nil {
		return time.Time{}, fmt.Errorf("invalid transaction date %q", dateText)
	}
	month, _ := strconv.Atoi(dateMatch[1])
	day, _ := strconv.Atoi(dateMatch[2])
	clock, err := time.ParseInLocation("15:04:05", normalizeTime(timeText), location)
	if err != nil {
		return time.Time{}, err
	}
	candidate := time.Date(receivedAt.Year(), time.Month(month), day, clock.Hour(), clock.Minute(), clock.Second(), 0, location)
	if candidate.After(receivedAt.AddDate(0, 0, 31)) {
		candidate = candidate.AddDate(-1, 0, 0)
	}
	return candidate, nil
}

func normalizeTime(value string) string {
	if strings.Count(value, ":") == 1 {
		return value + ":00"
	}
	return value
}

func overlapsAny(start int, end int, ranges [][2]int) bool {
	for _, candidate := range ranges {
		if start < candidate[1] && end > candidate[0] {
			return true
		}
	}
	return false
}
