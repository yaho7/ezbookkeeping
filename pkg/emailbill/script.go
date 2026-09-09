package emailbill

import (
	"crypto/sha256"
	"fmt"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.starlark.net/starlark"
)

const defaultMaxScriptSourceBytes = 64 * 1024

var allowedBillFields = map[string]struct{}{
	"external_id": {}, "occurred_at": {}, "amount": {}, "currency": {},
	"flow_type": {}, "merchant": {}, "description": {}, "account_hint": {},
}

// ScriptMail is the immutable, redacted email view exposed to parser code.
type ScriptMail struct {
	MessageID  string
	Sender     string
	Subject    string
	ReceivedAt time.Time
	Text       string
	Headers    map[string]string
}

// ParserMatcher prefilters mail before starting a parser runtime.
type ParserMatcher struct {
	Senders         []string `json:"senders"`
	SubjectContains []string `json:"subjectContains"`
}

// Matches reports whether all configured matcher groups accept a message.
func (m ParserMatcher) Matches(mail ScriptMail) bool {
	if len(m.Senders) > 0 {
		matched := false
		for _, sender := range m.Senders {
			if strings.EqualFold(normalizedEmailAddress(sender), normalizedEmailAddress(mail.Sender)) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(m.SubjectContains) > 0 {
		matched := false
		for _, keyword := range m.SubjectContains {
			if keyword != "" && strings.Contains(mail.Subject, keyword) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func normalizedEmailAddress(value string) string {
	value = strings.TrimSpace(value)
	if address, err := mail.ParseAddress(value); err == nil {
		return strings.ToLower(strings.TrimSpace(address.Address))
	}
	return strings.ToLower(value)
}

// ScriptLimits bounds source, execution, and output resource consumption.
type ScriptLimits struct {
	MaxSourceBytes    int
	MaxExecutionSteps uint64
	Timeout           time.Duration
	MaxOutputs        int
}

// ScriptStats contains safe execution diagnostics for tests and audit records.
type ScriptStats struct {
	ExecutionSteps uint64
	Duration       time.Duration
}

// ScriptParser executes Starlark parser rules in a capability-free environment.
type ScriptParser struct {
	limits ScriptLimits
}

// NewScriptParser creates a parser with safe defaults for unset limits.
func NewScriptParser(limits ScriptLimits) *ScriptParser {
	if limits.MaxSourceBytes <= 0 {
		limits.MaxSourceBytes = defaultMaxScriptSourceBytes
	}
	if limits.MaxExecutionSteps == 0 {
		limits.MaxExecutionSteps = 100000
	}
	if limits.Timeout <= 0 {
		limits.Timeout = 200 * time.Millisecond
	}
	if limits.MaxOutputs <= 0 {
		limits.MaxOutputs = 16
	}
	return &ScriptParser{limits: limits}
}

// Parse executes parse(mail) and validates its standard bill output.
func (p *ScriptParser) Parse(source string, mail ScriptMail) ([]StandardBill, ScriptStats, error) {
	started := time.Now()
	stats := ScriptStats{}
	if len(source) > p.limits.MaxSourceBytes {
		return nil, stats, fmt.Errorf("parser source exceeds %d bytes", p.limits.MaxSourceBytes)
	}

	thread := &starlark.Thread{Name: "email-bill-parser"}
	thread.SetMaxExecutionSteps(p.limits.MaxExecutionSteps)
	timer := time.AfterFunc(p.limits.Timeout, func() { thread.Cancel("execution timeout") })
	defer timer.Stop()

	predeclared := starlark.StringDict{
		"regex_find":    starlark.NewBuiltin("regex_find", scriptRegexFind),
		"sha256":        starlark.NewBuiltin("sha256", scriptSHA256),
		"parse_builtin": starlark.NewBuiltin("parse_builtin", scriptParseBuiltin),
	}
	globals, err := starlark.ExecFile(thread, "parser.star", source, predeclared)
	if err != nil {
		stats.ExecutionSteps = thread.ExecutionSteps()
		stats.Duration = time.Since(started)
		return nil, stats, fmt.Errorf("execute parser: %w", err)
	}
	parseValue, ok := globals["parse"]
	if !ok {
		return nil, stats, fmt.Errorf("parser must define parse(mail)")
	}
	callable, ok := parseValue.(starlark.Callable)
	if !ok {
		return nil, stats, fmt.Errorf("parse must be callable")
	}

	result, err := starlark.Call(thread, callable, starlark.Tuple{scriptMailValue(mail)}, nil)
	stats.ExecutionSteps = thread.ExecutionSteps()
	stats.Duration = time.Since(started)
	if err != nil {
		return nil, stats, fmt.Errorf("call parse: %w", err)
	}
	list, ok := result.(*starlark.List)
	if !ok {
		return nil, stats, fmt.Errorf("parse must return a list")
	}
	if list.Len() > p.limits.MaxOutputs {
		return nil, stats, fmt.Errorf("parse may return at most %d bills", p.limits.MaxOutputs)
	}

	bills := make([]StandardBill, 0, list.Len())
	iter := list.Iterate()
	defer iter.Done()
	var value starlark.Value
	for index := 0; iter.Next(&value); index++ {
		bill, err := standardBillFromValue(value)
		if err != nil {
			return nil, stats, fmt.Errorf("bill %d: %w", index, err)
		}
		bills = append(bills, bill)
	}
	return bills, stats, nil
}

func scriptMailValue(mail ScriptMail) starlark.Value {
	headers := starlark.NewDict(len(mail.Headers))
	for key, value := range mail.Headers {
		_ = headers.SetKey(starlark.String(strings.ToLower(key)), starlark.String(value))
	}
	headers.Freeze()
	receivedAt := ""
	if !mail.ReceivedAt.IsZero() {
		receivedAt = mail.ReceivedAt.Format(time.RFC3339)
	}
	dict := starlark.NewDict(6)
	values := map[string]string{
		"message_id":  mail.MessageID,
		"sender":      mail.Sender,
		"subject":     mail.Subject,
		"received_at": receivedAt,
		"text":        mail.Text,
	}
	for key, value := range values {
		_ = dict.SetKey(starlark.String(key), starlark.String(value))
	}
	_ = dict.SetKey(starlark.String("headers"), headers)
	dict.Freeze()
	return dict
}

func standardBillFromValue(value starlark.Value) (StandardBill, error) {
	dict, ok := value.(*starlark.Dict)
	if !ok {
		return StandardBill{}, fmt.Errorf("output must be a dictionary")
	}
	for _, item := range dict.Items() {
		key, ok := starlark.AsString(item[0])
		if !ok {
			return StandardBill{}, fmt.Errorf("field names must be strings")
		}
		if _, allowed := allowedBillFields[key]; !allowed {
			return StandardBill{}, fmt.Errorf("unknown field %q", key)
		}
	}

	amountText, err := dictString(dict, "amount", true)
	if err != nil {
		return StandardBill{}, err
	}
	amount, err := parseMinorAmount(amountText)
	if err != nil {
		return StandardBill{}, fmt.Errorf("amount: %w", err)
	}
	timeText, err := dictString(dict, "occurred_at", true)
	if err != nil {
		return StandardBill{}, err
	}
	occurredAt, err := time.Parse(time.RFC3339, timeText)
	if err != nil {
		return StandardBill{}, fmt.Errorf("occurred_at: %w", err)
	}
	currency, err := dictString(dict, "currency", true)
	if err != nil {
		return StandardBill{}, err
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if len(currency) != 3 {
		return StandardBill{}, fmt.Errorf("currency must be a three-letter code")
	}
	flowType, err := dictString(dict, "flow_type", true)
	if err != nil {
		return StandardBill{}, err
	}
	if !validFlowType(flowType) {
		return StandardBill{}, fmt.Errorf("unsupported flow_type %q", flowType)
	}

	bill := StandardBill{
		OccurredAt:  occurredAt,
		AmountMinor: amount,
		Currency:    currency,
		FlowType:    flowType,
	}
	bill.ExternalID, _ = dictString(dict, "external_id", false)
	bill.Merchant, _ = dictString(dict, "merchant", false)
	bill.Description, _ = dictString(dict, "description", false)
	if hintValue, found, _ := dict.Get(starlark.String("account_hint")); found {
		hint, ok := hintValue.(*starlark.Dict)
		if !ok {
			return StandardBill{}, fmt.Errorf("account_hint must be a dictionary")
		}
		bill.AccountHint.Bank, _ = dictString(hint, "bank", false)
		bill.AccountHint.Kind, _ = dictString(hint, "kind", false)
		bill.AccountHint.Last4, _ = dictString(hint, "last4", false)
	}
	return bill, nil
}

func dictString(dict *starlark.Dict, key string, required bool) (string, error) {
	value, found, err := dict.Get(starlark.String(key))
	if err != nil {
		return "", err
	}
	if !found {
		if required {
			return "", fmt.Errorf("missing %s", key)
		}
		return "", nil
	}
	text, ok := starlark.AsString(value)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return strings.TrimSpace(text), nil
}

func parseMinorAmount(value string) (int64, error) {
	value = strings.TrimSpace(value)
	negative := strings.HasPrefix(value, "-")
	if negative || strings.HasPrefix(value, "+") {
		value = value[1:]
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, fmt.Errorf("invalid decimal")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 2 {
		return 0, fmt.Errorf("more than two decimal places")
	}
	fraction += strings.Repeat("0", 2-len(fraction))
	minor, err := strconv.ParseInt(parts[0]+fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid decimal: %w", err)
	}
	if negative {
		minor = -minor
	}
	return minor, nil
}

func validFlowType(value string) bool {
	switch value {
	case "expense", "income", "refund", "transfer_in", "transfer_out":
		return true
	default:
		return false
	}
}

func scriptRegexFind(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var textValue, pattern string
	if err := starlark.UnpackArgs("regex_find", args, kwargs, "text", &textValue, "pattern", &pattern); err != nil {
		return nil, err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regular expression: %w", err)
	}
	match := re.FindStringSubmatch(textValue)
	if len(match) > 1 {
		return starlark.String(match[1]), nil
	}
	if len(match) == 1 {
		return starlark.String(match[0]), nil
	}
	return starlark.String(""), nil
}

func scriptSHA256(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value string
	if err := starlark.UnpackArgs("sha256", args, kwargs, "value", &value); err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(value))
	return starlark.String(fmt.Sprintf("%x", digest[:])), nil
}

func scriptParseBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var parserName string
	var mailValue *starlark.Dict
	if err := starlark.UnpackArgs("parse_builtin", args, kwargs, "name", &parserName, "mail", &mailValue); err != nil {
		return nil, err
	}
	var parser Parser
	var bank, kind string
	switch parserName {
	case "cmb_credit":
		parser, bank, kind = NewCMBCreditParser(), "cmb", "credit"
	case "cmb_debit":
		parser, bank, kind = NewCMBDebitParser(), "cmb", "debit"
	default:
		return nil, fmt.Errorf("unsupported built-in parser %q", parserName)
	}
	text, err := dictString(mailValue, "text", true)
	if err != nil {
		return nil, err
	}
	receivedText, err := dictString(mailValue, "received_at", true)
	if err != nil {
		return nil, err
	}
	receivedAt, err := time.Parse(time.RFC3339, receivedText)
	if err != nil {
		return nil, fmt.Errorf("received_at: %w", err)
	}
	transactions, err := parser.Parse(text, receivedAt)
	if err != nil {
		return nil, err
	}
	items := make([]starlark.Value, 0, len(transactions))
	for _, transaction := range transactions {
		items = append(items, parsedTransactionValue(transaction, bank, kind))
	}
	return starlark.NewList(items), nil
}

func parsedTransactionValue(transaction ParsedTransaction, bank, kind string) starlark.Value {
	amount := transaction.AmountMinor
	flowType := "income"
	if amount < 0 {
		amount = -amount
		flowType = "expense"
	} else if transaction.Source == SourceCMBCredit {
		flowType = "refund"
	}
	hint := starlark.NewDict(2)
	_ = hint.SetKey(starlark.String("bank"), starlark.String(bank))
	_ = hint.SetKey(starlark.String("kind"), starlark.String(kind))
	bill := starlark.NewDict(7)
	values := map[string]string{
		"occurred_at": transaction.OccurredAt.Format(time.RFC3339),
		"amount":      fmt.Sprintf("%d.%02d", amount/100, amount%100),
		"currency":    "CNY",
		"flow_type":   flowType,
		"merchant":    transaction.Merchant,
		"description": transaction.Description,
	}
	for key, value := range values {
		_ = bill.SetKey(starlark.String(key), starlark.String(value))
	}
	_ = bill.SetKey(starlark.String("account_hint"), hint)
	return bill
}
