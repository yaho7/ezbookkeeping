package services

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"xorm.io/xorm"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/models"
)

// EmailBillRoutingRuleInput is one editable account route revision.
type EmailBillRoutingRuleInput struct {
	RuleID          int64
	Enabled         bool
	Priority        int32
	Bank            string
	Kind            string
	Last4           string
	Currency        string
	MailboxID       int64
	TargetAccountID int64
}

// EmailBillRoutingRuleInfo combines a logical route with its current version.
type EmailBillRoutingRuleInfo struct {
	Rule       *models.EmailBillAccountRoutingRule        `json:"rule"`
	Version    *models.EmailBillAccountRoutingRuleVersion `json:"version"`
	Conditions emailbill.AccountRoutingRule               `json:"conditions"`
}

// EmailBillClassificationRuleInput is one editable merchant mapping revision.
type EmailBillClassificationRuleInput struct {
	RuleID          int64
	Origin          string
	Enabled         bool
	Priority        int32
	MerchantPattern string
	MatchType       string
	Bank            string
	AccountID       int64
	FlowType        string
	CategoryID      int64
	Confidence      float64
}

type emailBillClassificationScope struct {
	Bank      string `json:"bank"`
	AccountID int64  `json:"accountId,string"`
	FlowType  string `json:"flowType"`
}

// EmailBillClassificationRuleInfo combines an editable rule and immutable version.
type EmailBillClassificationRuleInfo struct {
	Rule    *models.EmailBillClassificationRule        `json:"rule"`
	Version *models.EmailBillClassificationRuleVersion `json:"version"`
	Scope   emailBillClassificationScope               `json:"scope"`
}

// ListRoutingRules returns all non-deleted account routing rules.
func (s *EmailBillAutomationService) ListRoutingRules(c core.Context, uid int64) ([]*EmailBillRoutingRuleInfo, error) {
	var rules []*models.EmailBillAccountRoutingRule
	if err := s.userDB(uid).NewSession(c).Where("uid=? AND deleted_unix_time=?", uid, 0).OrderBy("priority desc, routing_rule_id asc").Find(&rules); err != nil {
		return nil, err
	}
	result := make([]*EmailBillRoutingRuleInfo, 0, len(rules))
	for _, rule := range rules {
		version := &models.EmailBillAccountRoutingRuleVersion{}
		has, err := s.userDB(uid).NewSession(c).ID(rule.CurrentVersionId).Get(version)
		if err != nil {
			return nil, err
		}
		if !has {
			continue
		}
		conditions := emailbill.AccountRoutingRule{}
		if err = json.Unmarshal([]byte(version.ConditionsJson), &conditions); err != nil {
			return nil, err
		}
		conditions.ID, conditions.Enabled, conditions.Priority, conditions.TargetAccountID = version.RoutingRuleVersionId, rule.Enabled, rule.Priority, version.TargetAccountId
		result = append(result, &EmailBillRoutingRuleInfo{Rule: rule, Version: version, Conditions: conditions})
	}
	return result, nil
}

// SaveRoutingRule creates a route or appends an immutable version.
func (s *EmailBillAutomationService) SaveRoutingRule(c core.Context, uid int64, input EmailBillRoutingRuleInput) (*EmailBillRoutingRuleInfo, error) {
	if input.TargetAccountID <= 0 {
		return nil, fmt.Errorf("target account is required")
	}
	normalizeEmailBillRoutingInput(&input)
	conditions := emailbill.AccountRoutingRule{
		Bank: input.Bank, Kind: input.Kind, Last4: input.Last4, Currency: input.Currency,
		MailboxID: input.MailboxID, TargetAccountID: input.TargetAccountID,
	}
	raw, err := json.Marshal(conditions)
	if err != nil {
		return nil, err
	}
	signature, _ := emailBillRoutingRuleSignature(input)
	now := time.Now().Unix()
	rule := &models.EmailBillAccountRoutingRule{RoutingRuleId: input.RuleID, Uid: uid}
	versionNumber := int32(1)
	if input.RuleID <= 0 {
		rule.RoutingRuleId, rule.CreatedUnixTime = s.newID(), now
	} else {
		has, err := s.userDB(uid).NewSession(c).Where("uid=? AND routing_rule_id=? AND deleted_unix_time=?", uid, input.RuleID, 0).Get(rule)
		if err != nil || !has {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("routing rule not found")
		}
		current := &models.EmailBillAccountRoutingRuleVersion{}
		if has, err = s.userDB(uid).NewSession(c).ID(rule.CurrentVersionId).Get(current); err != nil {
			return nil, err
		} else if has {
			versionNumber = current.Version + 1
		}
	}
	version := &models.EmailBillAccountRoutingRuleVersion{
		RoutingRuleVersionId: s.newID(), RoutingRuleId: rule.RoutingRuleId, Version: versionNumber,
		ConditionsJson: string(raw), RuleSignature: signature, TargetAccountId: input.TargetAccountID, CreatedUnixTime: now,
	}
	rule.Enabled, rule.Priority, rule.CurrentVersionId, rule.UpdatedUnixTime = input.Enabled, input.Priority, version.RoutingRuleVersionId, now
	err = s.userDB(uid).DoTransaction(c, func(sess *xorm.Session) error {
		if input.RuleID <= 0 {
			if _, err := sess.Insert(rule); err != nil {
				return err
			}
		} else if _, err := sess.ID(rule.RoutingRuleId).Cols("enabled", "priority", "current_version_id", "updated_unix_time").Update(rule); err != nil {
			return err
		}
		_, err := sess.Insert(version)
		return err
	})
	return &EmailBillRoutingRuleInfo{Rule: rule, Version: version, Conditions: conditions}, err
}

// DisableRoutingRule makes a route unavailable while preserving all versions.
func (s *EmailBillAutomationService) DisableRoutingRule(c core.Context, uid, ruleID int64) error {
	updated, err := s.userDB(uid).NewSession(c).Cols("enabled", "updated_unix_time").Where("uid=? AND routing_rule_id=? AND deleted_unix_time=?", uid, ruleID, 0).
		Update(&models.EmailBillAccountRoutingRule{Enabled: false, UpdatedUnixTime: time.Now().Unix()})
	if err != nil {
		return err
	}
	if updated != 1 {
		return fmt.Errorf("routing rule not found")
	}
	return nil
}

// ListClassificationRules returns manual and learned merchant mappings.
func (s *EmailBillAutomationService) ListClassificationRules(c core.Context, uid int64) ([]*EmailBillClassificationRuleInfo, error) {
	var rules []*models.EmailBillClassificationRule
	if err := s.userDB(uid).NewSession(c).Where("uid=? AND deleted_unix_time=?", uid, 0).OrderBy("priority desc, classification_rule_id asc").Find(&rules); err != nil {
		return nil, err
	}
	result := make([]*EmailBillClassificationRuleInfo, 0, len(rules))
	for _, rule := range rules {
		version := &models.EmailBillClassificationRuleVersion{}
		has, err := s.userDB(uid).NewSession(c).ID(rule.CurrentVersionId).Get(version)
		if err != nil {
			return nil, err
		}
		if !has {
			continue
		}
		scope := emailBillClassificationScope{}
		if err = json.Unmarshal([]byte(version.ScopeJson), &scope); err != nil {
			return nil, err
		}
		result = append(result, &EmailBillClassificationRuleInfo{Rule: rule, Version: version, Scope: scope})
	}
	return result, nil
}

// SaveClassificationRule creates a rule or appends an immutable version.
func (s *EmailBillAutomationService) SaveClassificationRule(c core.Context, uid int64, input EmailBillClassificationRuleInput) (*EmailBillClassificationRuleInfo, error) {
	if err := validateEmailBillClassificationRule(input); err != nil {
		return nil, err
	}
	input.Origin = strings.ToLower(strings.TrimSpace(input.Origin))
	input.MatchType = strings.ToLower(strings.TrimSpace(input.MatchType))
	input.MerchantPattern = strings.TrimSpace(input.MerchantPattern)
	scope := emailBillClassificationScope{Bank: strings.ToLower(strings.TrimSpace(input.Bank)), AccountID: input.AccountID, FlowType: strings.ToLower(strings.TrimSpace(input.FlowType))}
	scopeJSON, err := json.Marshal(scope)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	rule := &models.EmailBillClassificationRule{ClassificationRuleId: input.RuleID, Uid: uid}
	versionNumber := int32(1)
	if input.RuleID <= 0 {
		rule.ClassificationRuleId, rule.CreatedUnixTime = s.newID(), now
	} else {
		has, err := s.userDB(uid).NewSession(c).Where("uid=? AND classification_rule_id=? AND deleted_unix_time=?", uid, input.RuleID, 0).Get(rule)
		if err != nil || !has {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("classification rule not found")
		}
		current := &models.EmailBillClassificationRuleVersion{}
		if has, err = s.userDB(uid).NewSession(c).ID(rule.CurrentVersionId).Get(current); err != nil {
			return nil, err
		} else if has {
			versionNumber = current.Version + 1
		}
	}
	signature := hashEmailBillValue([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d", strings.ToLower(input.MerchantPattern), input.MatchType, scopeJSON, input.CategoryID)))
	version := &models.EmailBillClassificationRuleVersion{
		ClassificationRuleVersionId: s.newID(), ClassificationRuleId: rule.ClassificationRuleId, Version: versionNumber,
		MerchantPattern: input.MerchantPattern, MatchType: input.MatchType, ScopeJson: string(scopeJSON),
		RuleSignature: signature, CategoryId: input.CategoryID, Confidence: input.Confidence, CreatedUnixTime: now,
	}
	rule.Origin, rule.Enabled, rule.Priority = input.Origin, input.Enabled, input.Priority
	rule.CurrentVersionId, rule.UpdatedUnixTime = version.ClassificationRuleVersionId, now
	err = s.userDB(uid).DoTransaction(c, func(sess *xorm.Session) error {
		if input.RuleID <= 0 {
			if _, err := sess.Insert(rule); err != nil {
				return err
			}
		} else if _, err := sess.ID(rule.ClassificationRuleId).Cols("origin", "enabled", "priority", "current_version_id", "updated_unix_time", "disabled_unix_time").Update(rule); err != nil {
			return err
		}
		_, err := sess.Insert(version)
		return err
	})
	return &EmailBillClassificationRuleInfo{Rule: rule, Version: version, Scope: scope}, err
}

// DisableClassificationRule makes manual or learned mappings reversible.
func (s *EmailBillAutomationService) DisableClassificationRule(c core.Context, uid, ruleID int64) error {
	now := time.Now().Unix()
	updated, err := s.userDB(uid).NewSession(c).Cols("enabled", "updated_unix_time", "disabled_unix_time").Where("uid=? AND classification_rule_id=? AND deleted_unix_time=?", uid, ruleID, 0).
		Update(&models.EmailBillClassificationRule{Enabled: false, UpdatedUnixTime: now, DisabledUnixTime: now})
	if err != nil {
		return err
	}
	if updated != 1 {
		return fmt.Errorf("classification rule not found")
	}
	return nil
}

// DeleteClassificationRule removes a mapping from active management while retaining immutable versions for audit references.
func (s *EmailBillAutomationService) DeleteClassificationRule(c core.Context, uid, ruleID int64) error {
	now := time.Now().Unix()
	updated, err := s.userDB(uid).NewSession(c).Cols("enabled", "updated_unix_time", "disabled_unix_time", "deleted_unix_time").
		Where("uid=? AND classification_rule_id=? AND deleted_unix_time=?", uid, ruleID, 0).
		Update(&models.EmailBillClassificationRule{Enabled: false, UpdatedUnixTime: now, DisabledUnixTime: now, DeletedUnixTime: now})
	if err != nil {
		return err
	}
	if updated != 1 {
		return fmt.Errorf("classification rule not found")
	}
	return nil
}

func validateEmailBillClassificationRule(input EmailBillClassificationRuleInput) error {
	if input.CategoryID <= 0 || strings.TrimSpace(input.MerchantPattern) == "" {
		return fmt.Errorf("merchant pattern and category are required")
	}
	origin := strings.ToLower(strings.TrimSpace(input.Origin))
	if origin != emailbill.RuleOriginManual && origin != emailbill.RuleOriginLearnedManual && origin != emailbill.RuleOriginLearnedLLM {
		return fmt.Errorf("unsupported rule origin %q", input.Origin)
	}
	matchType := strings.ToLower(strings.TrimSpace(input.MatchType))
	if matchType != emailbill.MatchExact && matchType != emailbill.MatchContains && matchType != emailbill.MatchRegex {
		return fmt.Errorf("unsupported match type %q", input.MatchType)
	}
	if matchType == emailbill.MatchRegex {
		if _, err := regexp.Compile(input.MerchantPattern); err != nil {
			return fmt.Errorf("invalid classification regex: %w", err)
		}
	}
	if input.Confidence < 0 || input.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	return nil
}

func emailBillRoutingRuleSignature(input EmailBillRoutingRuleInput) (string, error) {
	normalizeEmailBillRoutingInput(&input)
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	return hashEmailBillValue(raw), nil
}

func normalizeEmailBillRoutingInput(input *EmailBillRoutingRuleInput) {
	input.Bank = strings.ToLower(strings.TrimSpace(input.Bank))
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	input.Last4 = strings.TrimSpace(input.Last4)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
}
