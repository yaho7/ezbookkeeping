package emailbill

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	RuleOriginManual        = "manual"
	RuleOriginLearnedManual = "learned_manual"
	RuleOriginLearnedLLM    = "learned_llm"

	MatchExact    = "exact"
	MatchContains = "contains"
	MatchRegex    = "regex"
)

// AccountRoutingRule maps standard parser hints to a native account.
type AccountRoutingRule struct {
	ID              int64
	Enabled         bool
	Priority        int32
	Bank            string
	Kind            string
	Last4           string
	Currency        string
	MailboxID       int64
	TargetAccountID int64
}

// AccountDecision is the explainable result of account routing.
type AccountDecision struct {
	RuleID    int64
	AccountID int64
	Reason    string
}

// ResolveAccount selects the highest priority matching rule.
func ResolveAccount(bill StandardBill, mailboxID int64, rules []AccountRoutingRule) (AccountDecision, bool) {
	candidates := append([]AccountRoutingRule(nil), rules...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		return routingSpecificity(candidates[i]) > routingSpecificity(candidates[j])
	})
	for _, rule := range candidates {
		if !rule.Enabled {
			continue
		}
		if !optionalEqual(rule.Bank, bill.AccountHint.Bank) ||
			!optionalEqual(rule.Kind, bill.AccountHint.Kind) ||
			!optionalEqual(rule.Last4, bill.AccountHint.Last4) ||
			!optionalEqual(rule.Currency, bill.Currency) ||
			(rule.MailboxID > 0 && rule.MailboxID != mailboxID) {
			continue
		}
		return AccountDecision{RuleID: rule.ID, AccountID: rule.TargetAccountID, Reason: "matched account routing rule"}, true
	}
	return AccountDecision{}, false
}

func routingSpecificity(rule AccountRoutingRule) int {
	count := 0
	for _, value := range []string{rule.Bank, rule.Kind, rule.Last4, rule.Currency} {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	if rule.MailboxID > 0 {
		count++
	}
	return count
}

func optionalEqual(expected, actual string) bool {
	return strings.TrimSpace(expected) == "" || strings.EqualFold(strings.TrimSpace(expected), strings.TrimSpace(actual))
}

// ClassificationRule maps merchant patterns to native categories.
type ClassificationRule struct {
	ID              int64
	Enabled         bool
	Origin          string
	Priority        int32
	MerchantPattern string
	MatchType       string
	Bank            string
	AccountID       int64
	FlowType        string
	CategoryID      int64
	Confidence      float64
}

// ClassificationDecision is the explainable rule result.
type ClassificationDecision struct {
	RuleID     int64
	CategoryID int64
	MatchType  string
	Confidence float64
	Reason     string
}

// ResolveCategory evaluates manual, confirmed, then LLM-learned rules.
func ResolveCategory(bill StandardBill, accountID int64, rules []ClassificationRule) (ClassificationDecision, bool, error) {
	candidates := append([]ClassificationRule(nil), rules...)
	sort.SliceStable(candidates, func(i, j int) bool {
		leftRank, rightRank := originRank(candidates[i].Origin), originRank(candidates[j].Origin)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return candidates[i].Priority > candidates[j].Priority
	})
	merchant := normalizeText(bill.Merchant)
	for _, rule := range candidates {
		if !rule.Enabled || !optionalEqual(rule.Bank, bill.AccountHint.Bank) ||
			!optionalEqual(rule.FlowType, bill.FlowType) ||
			(rule.AccountID > 0 && rule.AccountID != accountID) {
			continue
		}
		matched, err := merchantMatches(merchant, rule)
		if err != nil {
			return ClassificationDecision{}, false, err
		}
		if !matched {
			continue
		}
		confidence := rule.Confidence
		if confidence <= 0 {
			confidence = 1
		}
		return ClassificationDecision{
			RuleID: rule.ID, CategoryID: rule.CategoryID, MatchType: rule.MatchType,
			Confidence: confidence, Reason: fmt.Sprintf("matched %s merchant rule", rule.Origin),
		}, true, nil
	}
	return ClassificationDecision{}, false, nil
}

func originRank(origin string) int {
	switch origin {
	case RuleOriginManual:
		return 0
	case RuleOriginLearnedManual:
		return 1
	case RuleOriginLearnedLLM:
		return 2
	default:
		return 3
	}
}

func merchantMatches(merchant string, rule ClassificationRule) (bool, error) {
	pattern := normalizeText(rule.MerchantPattern)
	switch rule.MatchType {
	case MatchExact:
		return merchant == pattern, nil
	case MatchContains:
		return pattern != "" && strings.Contains(merchant, pattern), nil
	case MatchRegex:
		expression, err := regexp.Compile(rule.MerchantPattern)
		if err != nil {
			return false, fmt.Errorf("invalid classification regex: %w", err)
		}
		return expression.MatchString(merchant), nil
	default:
		return false, fmt.Errorf("unsupported classification match type %q", rule.MatchType)
	}
}
