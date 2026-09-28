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
	"github.com/mayswind/ezbookkeeping/pkg/locales"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/uuid"
)

// EmailBillFinalizer routes, classifies, learns and imports resolved candidates.
type EmailBillFinalizer struct {
	db              *datastore.DataStoreContainer
	uuids           *uuid.UuidContainer
	automation      *EmailBillAutomationService
	classifier      *EmailBillClassifier
	importer        *EmailBillTransactionImporter
	defaultAccounts emailBillDefaultAccountResolver
}

// NewEmailBillFinalizer creates the production candidate finalizer.
func NewEmailBillFinalizer() *EmailBillFinalizer {
	return &EmailBillFinalizer{
		db: datastore.Container, uuids: uuid.Container, automation: EmailBillAutomation,
		classifier:      NewEmailBillClassifier(NewConfiguredEmailBillLLMClient(), 0.8),
		importer:        NewEmailBillTransactionImporter(),
		defaultAccounts: EmailBillDefaultAccounts,
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
		if candidate.SelectedVariantId <= 0 || candidate.Status == "imported" {
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
	accountDecision, routed, accountErr := s.resolveAccount(c, uid, mailboxID, bill, routingRules)
	if err = s.persistAccountDecision(c, uid, runID, candidate, accountDecision, routed); err != nil {
		return err
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
	options := emailBillUsableCategoryOptions(categories)
	validCategories := make(map[int64]bool, len(options))
	for _, option := range options {
		validCategories[option.ID] = true
	}
	for index := range classificationRules {
		if !validCategories[classificationRules[index].CategoryID] {
			classificationRules[index].Enabled = false
		}
	}
	classification, llmRunID, reused, err := s.classificationForAccountRetry(c, uid, candidate, options)
	if err != nil {
		return err
	}
	if !reused {
		classification, err = s.classifier.Classify(c, uid, bill, accountDecision.AccountID, classificationRules, options)
		if err != nil {
			classification = EmailBillClassificationResult{Source: "fallback", Reason: err.Error()}
		}
		if classification.Source == "llm_new" {
			classification.CategoryID, err = s.createClaimedCategory(c, uid, categoryType, classification.ProposedCategoryName)
			if err != nil {
				classification.CategoryID = 0
				classification.Source = "fallback"
				classification.Reason = err.Error()
			}
		}
		if strings.HasPrefix(classification.Source, "llm_") || classification.Source == "fallback" {
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
				classification.Reason += "; save merchant mapping: " + learnErr.Error()
			} else {
				classification.RuleVersionID = learned.Version.ClassificationRuleVersionId
			}
		}
	}
	if classification.CategoryID <= 0 {
		user, userErr := Users.GetUserById(c, uid)
		if userErr != nil {
			return userErr
		}
		name := locales.GetLocaleTextItems(user.Language).GlobalTextItems.UncategorizedEmailBillCategoryName
		if name == "" {
			name = "Uncategorized"
		}
		classification.CategoryID, err = s.createClaimedCategory(c, uid, categoryType, name)
		classification.Source = "fallback"
		if err != nil {
			classification.Reason = err.Error()
			return errors.Join(err, s.persistClassificationDecision(c, uid, runID, candidate, classification, llmRunID, routed))
		}
	}
	if err = s.persistClassificationDecision(c, uid, runID, candidate, classification, llmRunID, routed); err != nil {
		return err
	}
	if !routed || classification.CategoryID <= 0 {
		return accountErr
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
	if found && decision.RuleID == 0 {
		decisionType = "default"
	}
	if !found {
		status, decisionType = "awaiting_account", "unresolved"
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
		if err != nil {
			return err
		}
		return EmailBillAutomationStore.insertAuditEvent(sess, uid, candidate.MessageId, candidate.CandidateId, runID, "account_routed", decisionType, map[string]any{
			"decisionId": model.AccountDecisionId, "ruleVersionId": decision.RuleID, "accountId": decision.AccountID, "status": status,
		})
	})
}

func (s *EmailBillFinalizer) persistClassificationDecision(c core.Context, uid, runID int64, candidate *models.EmailBillCandidate, decision EmailBillClassificationResult, llmRunID int64, accountResolved bool) error {
	status := emailBillClassifiedCandidateStatus(accountResolved, decision.CategoryID)
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
		if err != nil {
			return err
		}
		return EmailBillAutomationStore.insertAuditEvent(sess, uid, candidate.MessageId, candidate.CandidateId, runID, "category_classified", decision.Source, map[string]any{
			"decisionId": model.ClassificationDecisionId, "ruleVersionId": decision.RuleVersionID,
			"llmRunId": llmRunID, "categoryId": decision.CategoryID, "status": status,
		})
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

// Only visible secondary categories with a visible parent can be used by native transactions.
func emailBillUsableCategoryOptions(categories []*models.TransactionCategory) []EmailBillCategoryOption {
	parents := make(map[int64]*models.TransactionCategory)
	for _, category := range categories {
		parents[category.CategoryId] = category
	}
	options := make([]EmailBillCategoryOption, 0, len(categories))
	for _, category := range categories {
		parent := parents[category.ParentCategoryId]
		if category.Deleted || category.Hidden || category.ParentCategoryId == 0 || parent == nil || parent.Deleted || parent.Hidden {
			continue
		}
		options = append(options, EmailBillCategoryOption{ID: category.CategoryId, Name: parent.Name + " / " + category.Name})
	}
	return options
}

func (s *EmailBillFinalizer) createClaimedCategory(c core.Context, uid int64, categoryType models.TransactionCategoryType, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) > 64 {
		name = string([]rune(name)[:64])
	}
	if name == "" {
		return 0, fmt.Errorf("empty category name")
	}
	user, err := Users.GetUserById(c, uid)
	if err != nil {
		return 0, err
	}
	parentName := locales.GetLocaleTextItems(user.Language).GlobalTextItems.DefaultEmailBillCategoryParentName
	if parentName == "" {
		parentName = "Email bills"
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(name), " "))
	categoryID := int64(0)
	err = s.db.UserDataStore.Choose(uid).DoTransaction(c, func(sess *xorm.Session) error {
		claim := &models.EmailBillCategoryCreationClaim{}
		claimExists, err := sess.Where("uid=? AND category_type=? AND parent_category_id=? AND normalized_name=?", uid, categoryType, 0, normalized).Get(claim)
		if err != nil {
			return err
		}
		var categories []*models.TransactionCategory
		if err := sess.Where("uid=? AND deleted=? AND type=?", uid, false, categoryType).Find(&categories); err != nil {
			return err
		}
		for _, option := range emailBillUsableCategoryOptions(categories) {
			for _, category := range categories {
				if category.CategoryId == option.ID && ((claimExists && category.CategoryId == claim.CategoryId) || strings.EqualFold(category.Name, name)) {
					categoryID = category.CategoryId
					break
				}
			}
			if categoryID > 0 {
				break
			}
		}
		if categoryID == 0 {
			parentID := int64(0)
			parentOrder := int32(0)
			for _, category := range categories {
				if category.ParentCategoryId != 0 {
					continue
				}
				if category.DisplayOrder > parentOrder {
					parentOrder = category.DisplayOrder
				}
				if !category.Hidden && category.Name == parentName {
					parentID = category.CategoryId
				}
			}
			now := time.Now().Unix()
			if parentID == 0 {
				parentID = s.uuids.GenerateUuid(uuid.UUID_TYPE_CATEGORY)
				_, err := sess.Insert(&models.TransactionCategory{CategoryId: parentID, Uid: uid, Type: categoryType,
					Name: parentName, DisplayOrder: parentOrder + 1, Icon: 1, Color: "607D8B", CreatedUnixTime: now, UpdatedUnixTime: now})
				if err != nil {
					return err
				}
			}
			order := int32(0)
			for _, category := range categories {
				if category.ParentCategoryId == parentID && category.DisplayOrder > order {
					order = category.DisplayOrder
				}
			}
			categoryID = s.uuids.GenerateUuid(uuid.UUID_TYPE_CATEGORY)
			_, err := sess.Insert(&models.TransactionCategory{CategoryId: categoryID, Uid: uid, Type: categoryType,
				ParentCategoryId: parentID, Name: name, DisplayOrder: order + 1, Icon: 1, Color: "607D8B", CreatedUnixTime: now, UpdatedUnixTime: now})
			if err != nil {
				return err
			}
		}
		if claimExists {
			_, err = sess.ID(claim.CategoryClaimId).Cols("category_id").Update(&models.EmailBillCategoryCreationClaim{CategoryId: categoryID})
		} else {
			_, err = sess.Insert(&models.EmailBillCategoryCreationClaim{CategoryClaimId: s.uuids.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL),
				Uid: uid, CategoryType: categoryType, NormalizedName: normalized, CategoryId: categoryID, CreatedUnixTime: time.Now().Unix()})
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	return categoryID, nil
}

func emailBillClassifiedCandidateStatus(accountResolved bool, categoryID int64) string {
	if !accountResolved {
		return "awaiting_account"
	}
	if categoryID <= 0 {
		return "awaiting_classification"
	}
	return "ready"
}

func (s *EmailBillFinalizer) classificationForAccountRetry(c core.Context, uid int64, candidate *models.EmailBillCandidate, options []EmailBillCategoryOption) (EmailBillClassificationResult, int64, bool, error) {
	if candidate.CurrentClassificationDecisionId <= 0 {
		return EmailBillClassificationResult{}, 0, false, nil
	}
	decision := &models.EmailBillClassificationDecision{}
	has, err := s.db.UserDataStore.Choose(uid).NewSession(c).
		Where("classification_decision_id=? AND candidate_id=?", candidate.CurrentClassificationDecisionId, candidate.CandidateId).Get(decision)
	if err != nil || !has {
		return EmailBillClassificationResult{}, 0, false, err
	}
	for _, option := range options {
		if option.ID == decision.CategoryId {
			return EmailBillClassificationResult{Source: decision.DecisionType, CategoryID: decision.CategoryId,
				RuleVersionID: decision.ClassificationRuleVersionId, Confidence: decision.Confidence, Reason: decision.Reason}, decision.LLMRunId, true, nil
		}
	}
	return EmailBillClassificationResult{}, 0, false, nil
}

func (s *EmailBillFinalizer) resolveAccount(c core.Context, uid, mailboxID int64, bill emailbill.StandardBill, rules []emailbill.AccountRoutingRule) (emailbill.AccountDecision, bool, error) {
	if decision, found := emailbill.ResolveAccount(bill, mailboxID, rules); found {
		return decision, true, nil
	}
	accountID, err := s.defaultAccounts.Resolve(c, uid, bill.Currency)
	if err != nil {
		return emailbill.AccountDecision{Reason: "prepare default account: " + err.Error()}, false, err
	}
	return emailbill.AccountDecision{AccountID: accountID, Reason: "used default bookkeeping account"}, true, nil
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
