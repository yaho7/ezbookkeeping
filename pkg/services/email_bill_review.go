package services

import (
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

// EmailBillCandidateInfo is the review projection used by settings UI.
type EmailBillCandidateInfo struct {
	Candidate              *models.EmailBillCandidate
	Variants               []*models.EmailBillCandidateVariant
	AccountDecision        *models.EmailBillAccountRoutingDecision
	ClassificationDecision *models.EmailBillClassificationDecision
	TransactionID          int64
}

// EmailBillReviewService provides pending review, audit, correction and retry.
type EmailBillReviewService struct {
	db         *datastore.DataStoreContainer
	uuids      *uuid.UuidContainer
	automation *EmailBillAutomationService
	finalizer  *EmailBillFinalizer
	importer   *EmailBillTransactionImporter
}

// EmailBillReview is the shared review service.
var EmailBillReview = &EmailBillReviewService{
	db: datastore.Container, uuids: uuid.Container, automation: EmailBillAutomation,
	finalizer: NewEmailBillFinalizer(), importer: NewEmailBillTransactionImporter(),
}

// ListMessages returns recent user-owned mail samples for the parser test bench.
func (s *EmailBillReviewService) ListMessages(c core.Context, uid int64) ([]*models.EmailBillInboundMessage, error) {
	var messages []*models.EmailBillInboundMessage
	err := s.db.UserDataStore.Choose(uid).NewSession(c).Where("uid=?", uid).
		OrderBy("received_unix_time desc").Limit(50).Find(&messages)
	return messages, err
}

// ListCandidates returns recent candidates, optionally filtered by status.
func (s *EmailBillReviewService) ListCandidates(c core.Context, uid int64, status string) ([]*EmailBillCandidateInfo, error) {
	session := s.db.UserDataStore.Choose(uid).NewSession(c).Where("uid=?", uid)
	if strings.TrimSpace(status) != "" {
		session = session.And("status=?", strings.TrimSpace(status))
	}
	var candidates []*models.EmailBillCandidate
	if err := session.OrderBy("updated_unix_time desc").Limit(100).Find(&candidates); err != nil {
		return nil, err
	}
	result := make([]*EmailBillCandidateInfo, 0, len(candidates))
	for _, candidate := range candidates {
		info := &EmailBillCandidateInfo{Candidate: candidate}
		if err := s.db.UserDataStore.Choose(uid).NewSession(c).Where("candidate_id=?", candidate.CandidateId).OrderBy("created_unix_time asc").Find(&info.Variants); err != nil {
			return nil, err
		}
		if candidate.CurrentAccountDecisionId > 0 {
			info.AccountDecision = &models.EmailBillAccountRoutingDecision{}
			_, _ = s.db.UserDataStore.Choose(uid).NewSession(c).ID(candidate.CurrentAccountDecisionId).Get(info.AccountDecision)
		}
		if candidate.CurrentClassificationDecisionId > 0 {
			info.ClassificationDecision = &models.EmailBillClassificationDecision{}
			_, _ = s.db.UserDataStore.Choose(uid).NewSession(c).ID(candidate.CurrentClassificationDecisionId).Get(info.ClassificationDecision)
		}
		intent := &models.EmailBillTransactionImportIntent{}
		if has, _ := s.db.UserDataStore.Choose(uid).NewSession(c).Where("candidate_id=?", candidate.CandidateId).Get(intent); has {
			info.TransactionID = intent.TransactionId
		}
		result = append(result, info)
	}
	return result, nil
}

// ListAudit returns the immutable audit chain after verifying ownership.
func (s *EmailBillReviewService) ListAudit(c core.Context, uid, candidateID int64) ([]*models.EmailBillAuditEvent, error) {
	candidate := &models.EmailBillCandidate{}
	has, err := s.db.UserDataStore.Choose(uid).NewSession(c).Where("uid=? AND candidate_id=?", uid, candidateID).Get(candidate)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, fmt.Errorf("candidate not found")
	}
	var events []*models.EmailBillAuditEvent
	err = s.db.UserDataStore.Choose(uid).NewSession(c).Where("uid=? AND candidate_id=?", uid, candidateID).
		OrderBy("created_unix_time asc, audit_event_id asc").Find(&events)
	return events, err
}

// ConfirmCandidate applies an explicit variant/account/category correction and imports it.
func (s *EmailBillReviewService) ConfirmCandidate(c core.Context, uid, candidateID, variantID, accountID, categoryID int64) (int64, error) {
	database := s.db.UserDataStore.Choose(uid)
	candidate := &models.EmailBillCandidate{}
	has, err := database.NewSession(c).Where("uid=? AND candidate_id=?", uid, candidateID).Get(candidate)
	if err != nil || !has {
		if err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("candidate not found")
	}
	variant := &models.EmailBillCandidateVariant{}
	has, err = database.NewSession(c).Where("candidate_id=? AND variant_id=?", candidateID, variantID).Get(variant)
	if err != nil || !has {
		if err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("candidate variant not found")
	}
	run := &models.EmailBillImportRun{}
	has, err = database.NewSession(c).Where("message_id=?", candidate.MessageId).OrderBy("started_unix_time desc").Limit(1).Get(run)
	if err != nil || !has {
		if err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("import run not found")
	}
	now := time.Now().Unix()
	err = database.DoTransaction(c, func(sess *xorm.Session) error {
		action := &models.EmailBillConfirmationAction{
			ConfirmationActionId: s.uuids.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), CandidateId: candidateID,
			ImportRunId: run.ImportRunId, Uid: uid, Action: "confirm", PreviousVariantId: candidate.SelectedVariantId,
			SelectedVariantId: variantID, SelectedAccountId: accountID, SelectedCategoryId: categoryID, CreatedUnixTime: now,
		}
		if _, err := sess.Insert(action); err != nil {
			return err
		}
		accountDecision := &models.EmailBillAccountRoutingDecision{
			AccountDecisionId: s.uuids.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), CandidateId: candidateID,
			ImportRunId: run.ImportRunId, DecisionType: "user", AccountId: accountID,
			Confidence: 1, Reason: "confirmed by user", CreatedUnixTime: now,
		}
		if _, err := sess.Insert(accountDecision); err != nil {
			return err
		}
		classificationDecision := &models.EmailBillClassificationDecision{
			ClassificationDecisionId: s.uuids.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), CandidateId: candidateID,
			ImportRunId: run.ImportRunId, DecisionType: "user", CategoryId: categoryID,
			Confidence: 1, Reason: "confirmed by user", CreatedUnixTime: now,
		}
		if _, err := sess.Insert(classificationDecision); err != nil {
			return err
		}
		_, err := sess.ID(candidateID).Cols("selected_variant_id", "current_account_decision_id", "current_classification_decision_id", "status", "updated_unix_time").Update(&models.EmailBillCandidate{
			SelectedVariantId: variantID, CurrentAccountDecisionId: accountDecision.AccountDecisionId,
			CurrentClassificationDecisionId: classificationDecision.ClassificationDecisionId,
			Status:                          "ready", UpdatedUnixTime: now,
		})
		if err != nil {
			return err
		}
		return EmailBillAutomationStore.insertAuditEvent(sess, uid, candidate.MessageId, candidateID, run.ImportRunId, "candidate_confirmed", "user", map[string]any{
			"variantId": variantID, "accountId": accountID, "categoryId": categoryID,
		})
	})
	if err != nil {
		return 0, err
	}
	bill := standardBillFromVariant(variant)
	if strings.TrimSpace(bill.Merchant) != "" {
		_, _ = s.automation.SaveClassificationRule(c, uid, EmailBillClassificationRuleInput{
			Origin: emailbill.RuleOriginLearnedManual, Enabled: true, MerchantPattern: bill.Merchant,
			MatchType: emailbill.MatchExact, Bank: bill.AccountHint.Bank, AccountID: accountID,
			FlowType: bill.FlowType, CategoryID: categoryID, Confidence: 1,
		})
	}
	transactionID, err := s.importer.Import(c, uid, run.ImportRunId, candidateID, accountID, categoryID, bill)
	if err != nil {
		_ = s.updateStatus(c, database, candidateID, "import_failed")
		return 0, err
	}
	return transactionID, s.updateStatus(c, database, candidateID, "imported")
}

// RetryCandidate resumes routing/classification/import from persisted evidence.
func (s *EmailBillReviewService) RetryCandidate(c core.Context, uid, candidateID int64) error {
	candidate := &models.EmailBillCandidate{}
	database := s.db.UserDataStore.Choose(uid)
	has, err := database.NewSession(c).Where("uid=? AND candidate_id=?", uid, candidateID).Get(candidate)
	if err != nil || !has {
		if err != nil {
			return err
		}
		return fmt.Errorf("candidate not found")
	}
	run := &models.EmailBillImportRun{}
	has, err = database.NewSession(c).Where("message_id=?", candidate.MessageId).OrderBy("started_unix_time desc").Limit(1).Get(run)
	if err != nil || !has {
		if err != nil {
			return err
		}
		return fmt.Errorf("import run not found")
	}
	message := &models.EmailBillInboundMessage{}
	has, err = database.NewSession(c).Where("uid=? AND message_id=?", uid, candidate.MessageId).Get(message)
	if err != nil || !has {
		if err != nil {
			return err
		}
		return fmt.Errorf("inbound message not found")
	}
	return s.finalizer.FinalizeMessage(c, uid, message.MailboxId, candidate.MessageId, run.ImportRunId)
}

func (s *EmailBillReviewService) updateStatus(c core.Context, database *datastore.Database, candidateID int64, status string) error {
	_, err := database.NewSession(c).ID(candidateID).Cols("status", "updated_unix_time").Update(&models.EmailBillCandidate{Status: status, UpdatedUnixTime: time.Now().Unix()})
	return err
}
