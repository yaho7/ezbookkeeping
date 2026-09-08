package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"xorm.io/xorm"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/datastore"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/uuid"
)

// EmailBillFinalizer routes, classifies, learns and imports resolved candidates.
type EmailBillFinalizer struct {
	db         *datastore.DataStoreContainer
	uuids      *uuid.UuidContainer
	automation *EmailBillAutomationService
	classifier *EmailBillClassifier
	importer   *EmailBillTransactionImporter
}

// NewEmailBillFinalizer creates the production candidate finalizer.
func NewEmailBillFinalizer() *EmailBillFinalizer {
	return &EmailBillFinalizer{
		db: datastore.Container, uuids: uuid.Container, automation: EmailBillAutomation,
		classifier: NewEmailBillClassifier(NewConfiguredEmailBillLLMClient(), 0.8),
		importer:   NewEmailBillTransactionImporter(),
	}
}

// FinalizeMessage processes every non-conflicted candidate independently.
func (s *EmailBillFinalizer) FinalizeMessage(c core.Context, uid, mailboxID, messageID, runID int64) error {
	var candidates []*models.EmailBillCandidate
	err := s.db.UserDataStore.Choose(uid).NewSession(c).
		Where("uid=? AND message_id=?", uid, messageID).OrderBy("candidate_id asc").Find(&candidates)
	if err != nil {
		return err
	}
	var failures []error
	for _, candidate := range candidates {
		if candidate.SelectedVariantId <= 0 || candidate.Status == "awaiting_confirmation" || candidate.Status == "imported" {
			continue
		}
		if err := s.finalizeCandidate(c, uid, mailboxID, runID, candidate); err != nil {
			failures = append(failures, fmt.Errorf("candidate %d: %w", candidate.CandidateId, err))
		}
	}
	return errors.Join(failures...)
}

func (s *EmailBillFinalizer) finalizeCandidate(c core.Context, uid, mailboxID, runID int64, candidate *models.EmailBillCandidate) error {
	variant := &models.EmailBillCandidateVariant{}
	has, err := s.db.UserDataStore.Choose(uid).NewSession(c).ID(candidate.SelectedVariantId).Get(variant)
	if err != nil || !has {
		if err != nil {
			return err
		}
		return fmt.Errorf("selected variant not found")
	}
	bill := standardBillFromVariant(variant)
	routingInfos, err := s.automation.ListRoutingRules(c, uid)
	if err != nil {
		return err
	}
	routingRules := make([]emailbill.AccountRoutingRule, 0, len(routingInfos))
	for _, info := range routingInfos {
		rule := info.Conditions
		rule.ID, rule.Enabled, rule.Priority, rule.TargetAccountID = info.Version.RoutingRuleVersionId, info.Rule.Enabled, info.Rule.Priority, info.Version.TargetAccountId
		routingRules = append(routingRules, rule)
	}
	accountDecision, routed := emailbill.ResolveAccount(bill, mailboxID, routingRules)
	if err = s.persistAccountDecision(c, uid, runID, candidate, accountDecision, routed); err != nil {
		return err
	}
	if !routed {
		return nil
	}

	classificationInfos, err := s.automation.ListClassificationRules(c, uid)
	if err != nil {
		return err
	}
	classificationRules := make([]emailbill.ClassificationRule, 0, len(classificationInfos))
	for _, info := range classificationInfos {
		classificationRules = append(classificationRules, emailbill.ClassificationRule{
			ID: info.Version.ClassificationRuleVersionId, Enabled: info.Rule.Enabled, Origin: info.Rule.Origin,
			Priority: info.Rule.Priority, MerchantPattern: info.Version.MerchantPattern, MatchType: info.Version.MatchType,
			Bank: info.Scope.Bank, AccountID: info.Scope.AccountID, FlowType: info.Scope.FlowType,
			CategoryID: info.Version.CategoryId, Confidence: info.Version.Confidence,
		})
	}
	categoryType := categoryTypeForEmailBill(bill)
	categories, err := TransactionCategories.GetAllCategoriesByUid(c, uid, categoryType, -1)
	if err != nil {
		return err
	}
	options := make([]EmailBillCategoryOption, 0, len(categories))
	for _, category := range categories {
		options = append(options, EmailBillCategoryOption{ID: category.CategoryId, Name: category.Name})
	}
	classification, err := s.classifier.Classify(c, uid, bill, accountDecision.AccountID, classificationRules, options)
	if err != nil {
		return s.persistAwaitingClassification(c, uid, runID, candidate, err.Error())
	}
	if classification.Source == "llm_new" {
		classification.CategoryID, err = s.createClaimedCategory(c, uid, categoryType, classification.ProposedCategoryName)
		if err != nil {
			classification.Source = "awaiting_confirmation"
			classification.Reason = err.Error()
		}
	}
	llmRunID := int64(0)
	if strings.HasPrefix(classification.Source, "llm_") || classification.Source == "awaiting_confirmation" {
		llmRunID, err = s.persistLLMRun(c, uid, runID, candidate.CandidateId, categoryType, classification, options)
		if err != nil {
			return err
		}
	}
	if classification.CategoryID > 0 && strings.HasPrefix(classification.Source, "llm_") && strings.TrimSpace(bill.Merchant) != "" {
		learned, learnErr := s.automation.SaveClassificationRule(c, uid, EmailBillClassificationRuleInput{
			Origin: emailbill.RuleOriginLearnedLLM, Enabled: true, MerchantPattern: bill.Merchant,
			MatchType: emailbill.MatchExact, Bank: bill.AccountHint.Bank, AccountID: accountDecision.AccountID,
			FlowType: bill.FlowType, CategoryID: classification.CategoryID, Confidence: classification.Confidence,
		})
		if learnErr != nil {
			return learnErr
		}
		classification.RuleVersionID = learned.Version.ClassificationRuleVersionId
	}
	if err = s.persistClassificationDecision(c, uid, runID, candidate, classification, llmRunID); err != nil {
		return err
	}
	if classification.CategoryID <= 0 {
		return nil
	}
	transactionID, err := s.importer.Import(c, uid, runID, candidate.CandidateId, accountDecision.AccountID, classification.CategoryID, bill)
	if err != nil {
		_ = s.updateCandidateStatus(c, uid, candidate.CandidateId, "import_failed")
		return err
	}
	_ = transactionID
	return s.updateCandidateStatus(c, uid, candidate.CandidateId, "imported")
}

func (s *EmailBillFinalizer) persistAccountDecision(c core.Context, uid, runID int64, candidate *models.EmailBillCandidate, decision emailbill.AccountDecision, found bool) error {
	status, decisionType, reason := "awaiting_classification", "rule", decision.Reason
	if !found {
		status, decisionType, reason = "awaiting_account", "unresolved", "no account routing rule matched"
	}
	database := s.db.UserDataStore.Choose(uid)
	return database.DoTransaction(c, func(sess *xorm.Session) error {
		model := &models.EmailBillAccountRoutingDecision{
			AccountDecisionId: s.uuids.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), CandidateId: candidate.CandidateId,
			ImportRunId: runID, DecisionType: decisionType, RoutingRuleVersionId: decision.RuleID,
			AccountId: decision.AccountID, Confidence: boolConfidence(found), Reason: reason, CreatedUnixTime: time.Now().Unix(),
		}
		if _, err := sess.Insert(model); err != nil {
			return err
		}
		_, err := sess.ID(candidate.CandidateId).Cols("status", "current_account_decision_id", "updated_unix_time").Update(&models.EmailBillCandidate{
			Status: status, CurrentAccountDecisionId: model.AccountDecisionId, UpdatedUnixTime: time.Now().Unix(),
		})
		return err
	})
}

func (s *EmailBillFinalizer) persistClassificationDecision(c core.Context, uid, runID int64, candidate *models.EmailBillCandidate, decision EmailBillClassificationResult, llmRunID int64) error {
	status := "ready"
	if decision.CategoryID <= 0 {
		status = "awaiting_confirmation"
	}
	database := s.db.UserDataStore.Choose(uid)
	return database.DoTransaction(c, func(sess *xorm.Session) error {
		model := &models.EmailBillClassificationDecision{
			ClassificationDecisionId: s.uuids.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), CandidateId: candidate.CandidateId,
			ImportRunId: runID, DecisionType: decision.Source, ClassificationRuleVersionId: decision.RuleVersionID,
			LLMRunId: llmRunID, CategoryId: decision.CategoryID, Confidence: decision.Confidence,
			Reason: decision.Reason, CreatedUnixTime: time.Now().Unix(),
		}
		if _, err := sess.Insert(model); err != nil {
			return err
		}
		_, err := sess.ID(candidate.CandidateId).Cols("status", "current_classification_decision_id", "updated_unix_time").Update(&models.EmailBillCandidate{
			Status: status, CurrentClassificationDecisionId: model.ClassificationDecisionId, UpdatedUnixTime: time.Now().Unix(),
		})
		return err
	})
}

func (s *EmailBillFinalizer) persistLLMRun(c core.Context, uid, runID, candidateID int64, categoryType models.TransactionCategoryType, decision EmailBillClassificationResult, categories []EmailBillCategoryOption) (int64, error) {
	snapshot, _ := json.Marshal(categories)
	model := &models.EmailBillLLMClassificationRun{
		LLMRunId: s.uuids.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), CandidateId: candidateID, ImportRunId: runID,
		Provider: "configured_text_recognition", Model: "configured", PromptVersion: 1,
		InputHash: hashEmailBillValue(snapshot), ExistingCategorySnapshot: string(snapshot), ResultType: decision.Source,
		SelectedCategoryId: decision.CategoryID, ProposedCategoryName: decision.ProposedCategoryName,
		Confidence: decision.Confidence, Reason: decision.Reason, CreatedUnixTime: time.Now().Unix(),
	}
	err := s.db.UserDataStore.Choose(uid).DoTransaction(c, func(sess *xorm.Session) error {
		if _, err := sess.Insert(model); err != nil {
			return err
		}
		if strings.TrimSpace(decision.ProposedCategoryName) != "" {
			status := "pending"
			if decision.CategoryID > 0 {
				status = "created"
			}
			proposal := &models.EmailBillCategoryCreationProposal{
				ProposalId: s.uuids.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), CandidateId: candidateID,
				ImportRunId: runID, LLMRunId: model.LLMRunId, ProposedName: decision.ProposedCategoryName,
				CategoryType: categoryType, Confidence: decision.Confidence,
				Status: status, CreatedCategoryId: decision.CategoryID, CreatedUnixTime: time.Now().Unix(),
			}
			if _, err := sess.Insert(proposal); err != nil {
				return err
			}
		}
		return nil
	})
	return model.LLMRunId, err
}

func (s *EmailBillFinalizer) createClaimedCategory(c core.Context, uid int64, categoryType models.TransactionCategoryType, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) > 64 {
		name = string([]rune(name)[:64])
	}
	if name == "" {
		return 0, fmt.Errorf("LLM proposed an empty category")
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(name), " "))
	database := s.db.UserDataStore.Choose(uid)
	categoryID := int64(0)
	err := database.DoTransaction(c, func(sess *xorm.Session) error {
		claim := &models.EmailBillCategoryCreationClaim{}
		claimExists, err := sess.Where("uid=? AND category_type=? AND parent_category_id=? AND normalized_name=?", uid, categoryType, 0, normalized).Get(claim)
		if err != nil {
			return err
		}
		if claimExists && claim.CategoryId > 0 {
			categoryID = claim.CategoryId
			return nil
		}
		existing := &models.TransactionCategory{}
		existingFound, err := sess.Where("uid=? AND deleted=? AND type=? AND name=?", uid, false, categoryType, name).Get(existing)
		if err != nil {
			return err
		}
		if existingFound {
			categoryID = existing.CategoryId
		} else {
			last := &models.TransactionCategory{}
			_, err = sess.Where("uid=? AND deleted=? AND type=? AND parent_category_id=?", uid, false, categoryType, 0).OrderBy("display_order desc").Limit(1).Get(last)
			if err != nil {
				return err
			}
			categoryID = s.uuids.GenerateUuid(uuid.UUID_TYPE_CATEGORY)
			now := time.Now().Unix()
			category := &models.TransactionCategory{
				CategoryId: categoryID, Uid: uid, Type: categoryType, ParentCategoryId: 0, Name: name,
				DisplayOrder: last.DisplayOrder + 1, Icon: 1, Color: "000000", CreatedUnixTime: now, UpdatedUnixTime: now,
			}
			if _, err = sess.Insert(category); err != nil {
				return err
			}
		}
		if !claimExists {
			claim = &models.EmailBillCategoryCreationClaim{
				CategoryClaimId: s.uuids.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), Uid: uid, CategoryType: categoryType,
				ParentCategoryId: 0, NormalizedName: normalized, CategoryId: categoryID, CreatedUnixTime: time.Now().Unix(),
			}
			_, err = sess.Insert(claim)
			return err
		}
		_, err = sess.ID(claim.CategoryClaimId).Cols("category_id").Update(&models.EmailBillCategoryCreationClaim{CategoryId: categoryID})
		return err
	})
	return categoryID, err
}

func (s *EmailBillFinalizer) persistAwaitingClassification(c core.Context, uid, runID int64, candidate *models.EmailBillCandidate, reason string) error {
	return s.persistClassificationDecision(c, uid, runID, candidate, EmailBillClassificationResult{Source: "awaiting_confirmation", Reason: reason}, 0)
}

func (s *EmailBillFinalizer) updateCandidateStatus(c core.Context, uid, candidateID int64, status string) error {
	_, err := s.db.UserDataStore.Choose(uid).NewSession(c).ID(candidateID).Cols("status", "updated_unix_time").Update(&models.EmailBillCandidate{Status: status, UpdatedUnixTime: time.Now().Unix()})
	return err
}

func standardBillFromVariant(variant *models.EmailBillCandidateVariant) emailbill.StandardBill {
	return emailbill.StandardBill{
		ExternalID: variant.ExternalId, OccurredAt: time.Unix(variant.TransactionUnixTime, 0),
		AmountMinor: variant.Amount, Currency: variant.Currency, FlowType: variant.Direction,
		Merchant: variant.Merchant, Description: variant.Description,
		AccountHint: emailbill.AccountHint{Bank: variant.Bank, Kind: variant.CardType, Last4: variant.CardLast4},
	}
}

func categoryTypeForEmailBill(bill emailbill.StandardBill) models.TransactionCategoryType {
	if strings.EqualFold(bill.FlowType, "income") || strings.EqualFold(bill.FlowType, "refund") || strings.EqualFold(bill.FlowType, "transfer_in") {
		return models.CATEGORY_TYPE_INCOME
	}
	return models.CATEGORY_TYPE_EXPENSE
}

func boolConfidence(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
