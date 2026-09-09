package services

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"xorm.io/xorm"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/datastore"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/utils"
	"github.com/mayswind/ezbookkeeping/pkg/uuid"
)

// EmailBillTransactionImporter owns the exactly-once native transaction boundary.
type EmailBillTransactionImporter struct {
	db           *datastore.DataStoreContainer
	uuids        *uuid.UuidContainer
	transactions *TransactionService
}

// NewEmailBillTransactionImporter creates a native importer.
func NewEmailBillTransactionImporter() *EmailBillTransactionImporter {
	return &EmailBillTransactionImporter{db: datastore.Container, uuids: uuid.Container, transactions: Transactions}
}

// Import creates or returns the one transaction owned by a candidate.
func (s *EmailBillTransactionImporter) Import(c core.Context, uid, runID, candidateID, accountID, categoryID int64, bill emailbill.StandardBill) (int64, error) {
	database := s.db.UserDataStore.Choose(uid)
	key := hashEmailBillValue([]byte(fmt.Sprintf("transaction\x00%d\x00%d", uid, candidateID)))
	intent, err := s.ensureIntent(c, database, candidateID, key)
	if err != nil {
		return 0, err
	}
	if intent.Status == "completed" {
		return intent.TransactionId, nil
	}
	claimed, err := database.NewSession(c).Cols("status").Where("import_intent_id=? AND (status=? OR status=?)", intent.ImportIntentId, "pending", "failed").
		Update(&models.EmailBillTransactionImportIntent{Status: "processing"})
	if err != nil {
		return 0, err
	}
	if claimed != 1 {
		current := &models.EmailBillTransactionImportIntent{}
		if has, getErr := database.NewSession(c).ID(intent.ImportIntentId).Get(current); getErr == nil && has && current.Status == "completed" {
			return current.TransactionId, nil
		}
		return 0, fmt.Errorf("email bill candidate is already being imported")
	}

	attempt, err := s.startAttempt(c, database, intent.ImportIntentId, runID)
	if err != nil {
		_ = s.markFailed(c, database, intent.ImportIntentId, 0, err)
		return 0, err
	}
	marker := emailBillCandidateMarker(candidateID)
	transaction := buildNativeEmailBillTransaction(c, uid, accountID, categoryID, bill, marker)
	err = database.DoTransaction(c, func(sess *xorm.Session) error {
		if err := chooseEmailBillTransactionTime(sess, transaction, marker); err != nil {
			return err
		}
		if err := s.transactions.createEmailBillTransactionInSession(c, database, sess, transaction); err != nil {
			return err
		}
		now := time.Now().Unix()
		updated, err := sess.ID(intent.ImportIntentId).Cols("status", "transaction_id", "completed_unix_time").Where("status=?", "processing").Update(&models.EmailBillTransactionImportIntent{
			Status: "completed", TransactionId: transaction.TransactionId, CompletedUnixTime: now,
		})
		if err != nil || updated != 1 {
			if err != nil {
				return err
			}
			return fmt.Errorf("email bill import intent was not owned")
		}
		if _, err = sess.ID(attempt.ImportAttemptId).Cols("status", "transaction_id", "completed_unix_time").Update(&models.EmailBillTransactionImportAttempt{
			Status: "completed", TransactionId: transaction.TransactionId, CompletedUnixTime: now,
		}); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{
			"transactionId": fmt.Sprintf("%d", transaction.TransactionId),
			"accountId":     fmt.Sprintf("%d", accountID), "categoryId": fmt.Sprintf("%d", categoryID),
		})
		audit := &models.EmailBillAuditEvent{
			AuditEventId: s.uuids.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), Uid: uid, CandidateId: candidateID,
			ImportRunId: runID, EventType: "transaction_created", ActorType: "system",
			PayloadJson: string(payload), CreatedUnixTime: now,
		}
		_, err = sess.Insert(audit)
		return err
	})
	if err != nil {
		_ = s.markFailed(c, database, intent.ImportIntentId, attempt.ImportAttemptId, err)
		return 0, err
	}
	return transaction.TransactionId, nil
}

func (s *EmailBillTransactionImporter) ensureIntent(c core.Context, database *datastore.Database, candidateID int64, key string) (*models.EmailBillTransactionImportIntent, error) {
	intent := &models.EmailBillTransactionImportIntent{}
	has, err := database.NewSession(c).Where("candidate_id=?", candidateID).Get(intent)
	if err != nil {
		return nil, err
	}
	if has {
		return intent, nil
	}
	intent = &models.EmailBillTransactionImportIntent{
		ImportIntentId: s.uuids.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), CandidateId: candidateID,
		IdempotencyKey: key, Status: "pending", CreatedUnixTime: time.Now().Unix(),
	}
	if _, err = database.NewSession(c).Insert(intent); err != nil {
		existing := &models.EmailBillTransactionImportIntent{}
		if found, lookupErr := database.NewSession(c).Where("candidate_id=?", candidateID).Get(existing); lookupErr == nil && found {
			return existing, nil
		}
		return nil, err
	}
	return intent, nil
}

func (s *EmailBillTransactionImporter) startAttempt(c core.Context, database *datastore.Database, intentID, runID int64) (*models.EmailBillTransactionImportAttempt, error) {
	attempt := &models.EmailBillTransactionImportAttempt{}
	err := database.DoTransaction(c, func(sess *xorm.Session) error {
		count, err := sess.Where("import_intent_id=?", intentID).Count(&models.EmailBillTransactionImportAttempt{})
		if err != nil {
			return err
		}
		attempt = &models.EmailBillTransactionImportAttempt{
			ImportAttemptId: s.uuids.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), ImportIntentId: intentID,
			ImportRunId: runID, AttemptNumber: int32(count + 1), Status: "running", StartedUnixTime: time.Now().Unix(),
		}
		_, err = sess.Insert(attempt)
		return err
	})
	return attempt, err
}

func (s *EmailBillTransactionImporter) markFailed(c core.Context, database *datastore.Database, intentID, attemptID int64, cause error) error {
	now := time.Now().Unix()
	return database.DoTransaction(c, func(sess *xorm.Session) error {
		if _, err := sess.ID(intentID).Cols("status").Update(&models.EmailBillTransactionImportIntent{Status: "failed"}); err != nil {
			return err
		}
		if attemptID > 0 {
			_, err := sess.ID(attemptID).Cols("status", "error_type", "error_message", "completed_unix_time").Update(&models.EmailBillTransactionImportAttempt{
				Status: "failed", ErrorType: "transaction_error", ErrorMessage: cause.Error(), CompletedUnixTime: now,
			})
			return err
		}
		return nil
	})
}

func buildNativeEmailBillTransaction(c core.Context, uid, accountID, categoryID int64, bill emailbill.StandardBill, marker string) *models.Transaction {
	transactionType := models.TRANSACTION_DB_TYPE_EXPENSE
	if isEmailBillIncomeFlow(bill.FlowType) {
		transactionType = models.TRANSACTION_DB_TYPE_INCOME
	}
	amount := bill.AmountMinor
	if amount < 0 {
		amount = -amount
	}
	comment := strings.TrimSpace(bill.Merchant)
	if description := strings.TrimSpace(bill.Description); description != "" && description != comment {
		comment += " | " + description
	}
	comment = trimRunes(strings.TrimSpace(comment), maxTransactionCommentLength-len(marker)-1)
	return &models.Transaction{
		Uid: uid, Type: transactionType, CategoryId: categoryID, AccountId: accountID,
		TransactionTime:   utils.GetMinTransactionTimeFromUnixTime(bill.OccurredAt.Unix()),
		TimezoneUtcOffset: utils.GetTimezoneOffsetMinutes(bill.OccurredAt.Unix(), bill.OccurredAt.Location()),
		Amount:            amount, Comment: strings.TrimSpace(comment + " " + marker), CreatedIp: c.ClientIP(),
	}
}

func isEmailBillIncomeFlow(flowType string) bool {
	switch strings.ToLower(strings.TrimSpace(flowType)) {
	case "income", "refund", "transfer_in":
		return true
	default:
		return false
	}
}

func chooseEmailBillTransactionTime(sess *xorm.Session, transaction *models.Transaction, marker string) error {
	base := utils.GetMinTransactionTimeFromUnixTime(utils.GetUnixTimeFromTransactionTime(transaction.TransactionTime))
	start := emailBillTransactionTimeOffset(marker)
	for attempt := int64(0); attempt < 999; attempt++ {
		candidateTime := base + (start+attempt)%999
		exists, err := sess.Where("uid=? AND transaction_time=?", transaction.Uid, candidateTime).Exist(&models.Transaction{})
		if err != nil {
			return err
		}
		if !exists {
			transaction.TransactionTime = candidateTime
			return nil
		}
	}
	return fmt.Errorf("no free transaction time within source second")
}

func emailBillCandidateMarker(candidateID int64) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("candidate:%d", candidateID)))
	return fmt.Sprintf("[ebk-mail:%x]", digest[:10])
}

func emailBillImportIntentCanClaim(status string) bool {
	return status == "pending" || status == "failed"
}
