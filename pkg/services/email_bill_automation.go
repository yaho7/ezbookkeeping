package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"xorm.io/xorm"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/datastore"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/uuid"
)

const emailBillParserRuntimeVersion = "starlark-v1"

// EmailBillAutomationService manages user-editable, immutable rule versions.
type EmailBillAutomationService struct {
	db            *datastore.DataStoreContainer
	uuids         *uuid.UuidContainer
	parser        *emailbill.ScriptParser
	generator     EmailBillParserCodeGenerator
	defaultsMutex sync.Mutex
}

// EmailBillAutomation is the shared automation settings service.
var EmailBillAutomation = NewEmailBillAutomationService(datastore.Container, uuid.Container)

// NewEmailBillAutomationService creates the rule and test-bench service.
func NewEmailBillAutomationService(db *datastore.DataStoreContainer, uuids *uuid.UuidContainer) *EmailBillAutomationService {
	return &EmailBillAutomationService{
		db: db, uuids: uuids, parser: emailbill.NewScriptParser(emailbill.ScriptLimits{}),
		generator: NewConfiguredEmailBillParserCodeGenerator(),
	}
}

// EmailBillParserTestRequest is a no-side-effect parser test.
type EmailBillParserTestRequest struct {
	UID        int64
	Matcher    emailbill.ParserMatcher
	SourceCode string
	Mail       EmailBillFetchedMessage
}

// EmailBillParserPreviewBill is a validated standard bill with both identities.
type EmailBillParserPreviewBill struct {
	Bill              emailbill.StandardBill `json:"bill"`
	CandidateIdentity string                 `json:"candidateIdentity"`
	BillFingerprint   string                 `json:"billFingerprint"`
}

// EmailBillParserTestResult is returned by the parser test bench.
type EmailBillParserTestResult struct {
	Matched        bool                         `json:"matched"`
	ExecutionSteps uint64                       `json:"executionSteps"`
	DurationMillis int64                        `json:"durationMillis"`
	Bills          []EmailBillParserPreviewBill `json:"bills"`
}

// TestParser executes exactly one supplied version without any repository access.
func (s *EmailBillAutomationService) TestParser(request EmailBillParserTestRequest) (*EmailBillParserTestResult, error) {
	mail := emailbill.ScriptMail{
		MessageID: request.Mail.RemoteMessageID, Sender: request.Mail.Sender, Subject: request.Mail.Subject,
		ReceivedAt: request.Mail.ReceivedAt, Text: request.Mail.Text, Headers: request.Mail.Headers,
	}
	result := &EmailBillParserTestResult{}
	if !request.Matcher.Matches(mail) {
		return result, nil
	}
	result.Matched = true
	bills, stats, err := s.parser.Parse(request.SourceCode, mail)
	if err != nil {
		return nil, err
	}
	result.ExecutionSteps = stats.ExecutionSteps
	result.DurationMillis = stats.Duration.Milliseconds()
	messageFingerprint, _ := emailbill.MessageFingerprint(emailbill.MessageIdentityInput{
		MessageID: request.Mail.RemoteMessageID, Sender: request.Mail.Sender, Subject: request.Mail.Subject,
		ReceivedAt: request.Mail.ReceivedAt, Body: request.Mail.Text,
	})
	for index, bill := range bills {
		identity, _ := emailbill.CandidateIdentity(emailbill.CandidateIdentityInput{
			UserID: request.UID, Bank: bill.AccountHint.Bank, AccountLast4: bill.AccountHint.Last4,
			ExternalID: bill.ExternalID, MessageFingerprint: messageFingerprint, BillSequence: index,
		})
		fingerprint, _ := emailbill.BillFingerprint(emailbill.BillFingerprintInput{
			IdentityKey: identity, TransactionTime: bill.OccurredAt, AmountMinor: bill.AmountMinor,
			Currency: bill.Currency, Direction: bill.FlowType, Merchant: bill.Merchant, Description: bill.Description,
		})
		result.Bills = append(result.Bills, EmailBillParserPreviewBill{Bill: bill, CandidateIdentity: identity, BillFingerprint: fingerprint})
	}
	return result, nil
}

// EmailBillParserRuleInput contains one editable parser rule revision.
type EmailBillParserRuleInput struct {
	RuleID   int64
	Name     string
	Bank     string
	Enabled  bool
	Priority int32
	Matcher  emailbill.ParserMatcher
	Source   string
}

// EmailBillParserRuleInfo combines a logical rule with its current immutable version.
type EmailBillParserRuleInfo struct {
	Rule    *models.EmailBillParserRule        `json:"rule"`
	Version *models.EmailBillParserRuleVersion `json:"version"`
}

type defaultEmailBillParserRule struct {
	CreatedBy string
	Input     EmailBillParserRuleInput
}

func defaultEmailBillParserRules() []defaultEmailBillParserRule {
	return []defaultEmailBillParserRule{
		{
			CreatedBy: "preset:cmb_credit",
			Input: EmailBillParserRuleInput{
				Name: "招商银行信用卡（默认）", Bank: "招商银行", Enabled: true, Priority: 100,
				Matcher: emailbill.ParserMatcher{
					Senders:         []string{"ccsvc@message.cmbchina.com", "95555@message.cmbchina.com"},
					SubjectContains: []string{"每日信用管家"},
				},
				Source: "def parse(mail):\n    return parse_builtin(\"cmb_credit\", mail)",
			},
		},
		{
			CreatedBy: "preset:cmb_debit",
			Input: EmailBillParserRuleInput{
				Name: "招商银行储蓄卡（默认）", Bank: "招商银行", Enabled: true, Priority: 90,
				Matcher: emailbill.ParserMatcher{
					Senders: []string{"95555@message.cmbchina.com"}, SubjectContains: []string{"通知"},
				},
				Source: "def parse(mail):\n    return parse_builtin(\"cmb_debit\", mail)",
			},
		},
	}
}

// ListParserRules returns all active logical parser rules and current versions.
func (s *EmailBillAutomationService) ListParserRules(c core.Context, uid int64) ([]*EmailBillParserRuleInfo, error) {
	if err := s.ensureDefaultParserRules(c, uid); err != nil {
		return nil, err
	}
	var rules []*models.EmailBillParserRule
	if err := s.userDB(uid).NewSession(c).Where("uid=? AND deleted_unix_time=?", uid, 0).OrderBy("priority desc, parser_rule_id asc").Find(&rules); err != nil {
		return nil, err
	}
	result := make([]*EmailBillParserRuleInfo, 0, len(rules))
	for _, rule := range rules {
		version := &models.EmailBillParserRuleVersion{}
		has, err := s.userDB(uid).NewSession(c).ID(rule.CurrentVersionId).Get(version)
		if err != nil {
			return nil, err
		}
		if has {
			result = append(result, &EmailBillParserRuleInfo{Rule: rule, Version: version})
		}
	}
	return result, nil
}

// SaveParserRule creates a logical rule or appends a new immutable version.
func (s *EmailBillAutomationService) SaveParserRule(c core.Context, uid int64, input EmailBillParserRuleInput) (*EmailBillParserRuleInfo, error) {
	return s.saveParserRule(c, uid, input, "user")
}

func (s *EmailBillAutomationService) saveParserRule(c core.Context, uid int64, input EmailBillParserRuleInput, createdBy string) (*EmailBillParserRuleInfo, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Source = strings.TrimSpace(input.Source)
	if input.Name == "" || input.Source == "" {
		return nil, fmt.Errorf("parser name and source are required")
	}
	matcherJSON, err := json.Marshal(input.Matcher)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	rule := &models.EmailBillParserRule{ParserRuleId: input.RuleID, Uid: uid}
	versionNumber := int32(1)
	if input.RuleID <= 0 {
		rule.ParserRuleId = s.newID()
		rule.CreatedUnixTime = now
	} else {
		has, err := s.userDB(uid).NewSession(c).Where("uid=? AND parser_rule_id=? AND deleted_unix_time=?", uid, input.RuleID, 0).Get(rule)
		if err != nil || !has {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("parser rule not found")
		}
		current := &models.EmailBillParserRuleVersion{}
		if has, err = s.userDB(uid).NewSession(c).ID(rule.CurrentVersionId).Get(current); err != nil {
			return nil, err
		} else if has {
			versionNumber = current.Version + 1
		}
	}
	rule.Name, rule.Bank, rule.Enabled, rule.Priority = input.Name, strings.TrimSpace(input.Bank), input.Enabled, input.Priority
	rule.UpdatedUnixTime = now
	matcherHash := hashEmailBillValue(matcherJSON)
	sourceHash := hashEmailBillValue([]byte(input.Source))
	version := &models.EmailBillParserRuleVersion{
		ParserRuleVersionId: s.newID(), ParserRuleId: rule.ParserRuleId, Version: versionNumber,
		MatcherJson: string(matcherJSON), MatcherHash: matcherHash, SourceCode: input.Source, SourceHash: sourceHash,
		Runtime: "starlark", RuntimeVersion: emailBillParserRuntimeVersion, ParserApiVersion: 1,
		VersionHash:        hashEmailBillValue([]byte(fmt.Sprintf("%d\x00%d\x00%s\x00%s", rule.ParserRuleId, versionNumber, matcherHash, sourceHash))),
		ExecutionTimeoutMs: 200, InstructionLimit: 100000, CreatedBy: createdBy, CreatedUnixTime: now,
	}
	rule.CurrentVersionId = version.ParserRuleVersionId
	err = s.userDB(uid).DoTransaction(c, func(sess *xorm.Session) error {
		if input.RuleID <= 0 {
			if _, err := sess.Insert(rule); err != nil {
				return err
			}
		} else if _, err := sess.ID(rule.ParserRuleId).Cols("name", "bank", "enabled", "priority", "current_version_id", "updated_unix_time").Update(rule); err != nil {
			return err
		}
		_, err := sess.Insert(version)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &EmailBillParserRuleInfo{Rule: rule, Version: version}, nil
}

func (s *EmailBillAutomationService) ensureDefaultParserRules(c core.Context, uid int64) error {
	s.defaultsMutex.Lock()
	defer s.defaultsMutex.Unlock()

	var userRules []*models.EmailBillParserRule
	if err := s.userDB(uid).NewSession(c).Where("uid=?", uid).Find(&userRules); err != nil {
		return err
	}
	for _, preset := range defaultEmailBillParserRules() {
		found := false
		for _, rule := range userRules {
			has, err := s.userDB(uid).NewSession(c).
				Where("parser_rule_id=? AND created_by=?", rule.ParserRuleId, preset.CreatedBy).
				Exist(&models.EmailBillParserRuleVersion{})
			if err != nil {
				return err
			}
			if has {
				found = true
				break
			}
		}
		if !found {
			info, err := s.saveParserRule(c, uid, preset.Input, preset.CreatedBy)
			if err != nil {
				return err
			}
			userRules = append(userRules, info.Rule)
		}
	}
	return nil
}

// DisableParserRule disables a rule without deleting its version history.
func (s *EmailBillAutomationService) DisableParserRule(c core.Context, uid, ruleID int64) error {
	now := time.Now().Unix()
	updated, err := s.userDB(uid).NewSession(c).Cols("enabled", "updated_unix_time").Where("uid=? AND parser_rule_id=? AND deleted_unix_time=?", uid, ruleID, 0).
		Update(&models.EmailBillParserRule{Enabled: false, UpdatedUnixTime: now})
	if err != nil {
		return err
	}
	if updated != 1 {
		return fmt.Errorf("parser rule not found")
	}
	return nil
}

// RunnableParserRules loads the enabled immutable snapshots for a pipeline run.
func (s *EmailBillAutomationService) RunnableParserRules(c core.Context, uid int64) ([]emailbill.RunnableParserRule, error) {
	infos, err := s.ListParserRules(c, uid)
	if err != nil {
		return nil, err
	}
	rules := make([]emailbill.RunnableParserRule, 0, len(infos))
	for _, info := range infos {
		if !info.Rule.Enabled {
			continue
		}
		matcher := emailbill.ParserMatcher{}
		if err := json.Unmarshal([]byte(info.Version.MatcherJson), &matcher); err != nil {
			return nil, err
		}
		rules = append(rules, emailbill.RunnableParserRule{VersionID: info.Version.ParserRuleVersionId, Matcher: matcher, Source: info.Version.SourceCode})
	}
	return rules, nil
}

func (s *EmailBillAutomationService) userDB(uid int64) *datastore.Database {
	return s.db.UserDataStore.Choose(uid)
}

func (s *EmailBillAutomationService) newID() int64 {
	return s.uuids.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL)
}

func hashEmailBillValue(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}
