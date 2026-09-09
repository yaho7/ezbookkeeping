package services

import (
	"encoding/json"
	"fmt"
	"time"

	"xorm.io/xorm"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/datastore"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/uuid"
)

// EmailBillAutomationRepository persists the append-only automation evidence
// in the same user data database as native transactions.
type EmailBillAutomationRepository struct {
	ServiceUsingDB
	ServiceUsingUuid
}

// EmailBillAutomationStore is the shared automation repository.
var EmailBillAutomationStore = &EmailBillAutomationRepository{
	ServiceUsingDB:   ServiceUsingDB{container: datastore.Container},
	ServiceUsingUuid: ServiceUsingUuid{container: uuid.Container},
}

// PurgeRawBodies clears retained message bodies without removing identities or audit evidence.
func (r *EmailBillAutomationRepository) PurgeRawBodies(c core.Context, uid int64, olderThanUnix int64) error {
	session := r.UserDataDB(uid).NewSession(c).Where("uid=? AND body_content<>?", uid, "")
	if olderThanUnix > 0 {
		session = session.And("received_unix_time<?", olderThanUnix)
	}
	_, err := session.Cols("body_content").Update(&models.EmailBillInboundMessage{BodyContent: ""})
	return err
}

// SaveMessageAndStartRun atomically claims a message identity and creates its
// first import run. An existing identity is returned as a duplicate.
func (r *EmailBillAutomationRepository) SaveMessageAndStartRun(c core.Context, input EmailBillMessageInput) (messageID int64, runID int64, duplicate bool, err error) {
	existing := &models.EmailBillInboundMessage{}
	has, err := r.UserDataDB(input.UID).NewSession(c).
		Where("uid=? AND mailbox_id=? AND message_fingerprint=? AND fingerprint_version=?", input.UID, input.MailboxID, input.Fingerprint, input.FingerprintVersion).
		Get(existing)
	if err != nil {
		return 0, 0, false, err
	}
	if has {
		return existing.MessageId, 0, true, nil
	}

	now := time.Now().Unix()
	message := &models.EmailBillInboundMessage{
		MessageId: r.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), Uid: input.UID, MailboxId: input.MailboxID,
		RemoteMessageId: input.RemoteMessageID, MessageFingerprint: input.Fingerprint,
		FingerprintVersion: input.FingerprintVersion, Sender: input.Sender, Subject: input.Subject,
		ReceivedUnixTime: input.ReceivedAt.Unix(), BodyHash: input.BodyHash, BodySummary: input.BodySummary, BodyContent: input.BodyContent,
		AuthenticationStatus: authenticationStatus(input.Authenticated), AuthenticationSummary: input.AuthenticationDetail,
		CreatedUnixTime: now,
	}
	run := &models.EmailBillImportRun{
		ImportRunId: r.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), MessageId: message.MessageId,
		TriggerType: "mailbox", Status: "running", StartedUnixTime: now,
	}
	err = r.UserDataDB(input.UID).DoTransaction(c, func(sess *xorm.Session) error {
		if _, insertErr := sess.Insert(message); insertErr != nil {
			return insertErr
		}
		if _, insertErr := sess.Insert(run); insertErr != nil {
			return insertErr
		}
		event := &models.EmailBillImportRunEvent{
			EventId: r.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), ImportRunId: run.ImportRunId,
			ToStatus: "running", Reason: "message accepted", CreatedUnixTime: now,
		}
		if _, insertErr := sess.Insert(event); insertErr != nil {
			return insertErr
		}
		return r.insertAuditEvent(sess, input.UID, message.MessageId, 0, run.ImportRunId, "message_received", "system", map[string]any{
			"authenticated": input.Authenticated, "fingerprintVersion": input.FingerprintVersion,
		})
	})
	if err != nil {
		// A concurrent worker may have claimed the same unique identity.
		existing = &models.EmailBillInboundMessage{}
		has, lookupErr := r.UserDataDB(input.UID).NewSession(c).
			Where("uid=? AND mailbox_id=? AND message_fingerprint=? AND fingerprint_version=?", input.UID, input.MailboxID, input.Fingerprint, input.FingerprintVersion).
			Get(existing)
		if lookupErr == nil && has {
			return existing.MessageId, 0, true, nil
		}
		return 0, 0, false, err
	}
	return message.MessageId, run.ImportRunId, false, nil
}

// SaveParserRun records one outcome and all validated parser outputs.
func (r *EmailBillAutomationRepository) SaveParserRun(c core.Context, input EmailBillPersistedParserRun) ([]EmailBillSavedOutput, error) {
	now := time.Now().Unix()
	parserRun := &models.EmailBillParserRun{
		ParserRunId: r.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), ImportRunId: input.RunID,
		ParserRuleVersionId: input.ParserVersionID, Matched: input.Matched, Status: input.Status,
		StartedUnixTime: now, FinishedUnixTime: now, DurationMillis: input.Stats.Duration.Milliseconds(),
		InstructionCount: input.Stats.ExecutionSteps, InputHash: input.InputHash,
		ErrorType: parserErrorType(input.ErrorMessage), ErrorMessage: input.ErrorMessage,
	}
	outputs := make([]EmailBillSavedOutput, len(input.Bills))
	err := r.UserDataDB(input.UID).DoTransaction(c, func(sess *xorm.Session) error {
		if _, err := sess.Insert(parserRun); err != nil {
			return err
		}
		for index, bill := range input.Bills {
			raw, err := json.Marshal(bill)
			if err != nil {
				return err
			}
			output := parserOutputModel(r.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), parserRun.ParserRunId, int32(index), bill, string(raw), now)
			if _, err = sess.Insert(output); err != nil {
				return err
			}
			outputs[index] = EmailBillSavedOutput{OutputID: output.ParserOutputId, Bill: bill}
		}
		return r.insertAuditEvent(sess, input.UID, 0, 0, input.RunID, "parser_finished", "parser", map[string]any{
			"parserVersionId": input.ParserVersionID, "status": input.Status, "outputCount": len(input.Bills),
		})
	})
	return outputs, err
}

// SaveCandidates upserts candidate identities, exact variants and all parser evidence.
func (r *EmailBillAutomationRepository) SaveCandidates(c core.Context, uid, runID int64, messageFingerprint string, candidates []emailbill.AggregatedCandidate, outputs map[int64][]EmailBillSavedOutput) error {
	now := time.Now().Unix()
	return r.UserDataDB(uid).DoTransaction(c, func(sess *xorm.Session) error {
		run := &models.EmailBillImportRun{}
		has, err := sess.ID(runID).Get(run)
		if err != nil || !has {
			if err != nil {
				return err
			}
			return fmt.Errorf("email bill import run %d not found", runID)
		}
		for _, aggregated := range candidates {
			candidate, err := r.getOrCreateCandidate(sess, uid, run.MessageId, aggregated, now)
			if err != nil {
				return err
			}
			variantIDs := make([]int64, 0, len(aggregated.Variants))
			for _, aggregatedVariant := range aggregated.Variants {
				variant, err := r.getOrCreateVariant(sess, candidate.CandidateId, aggregatedVariant, now)
				if err != nil {
					return err
				}
				variantIDs = append(variantIDs, variant.VariantId)
				for _, parserVersionID := range aggregatedVariant.ParserVersionIDs {
					for _, outputID := range matchingEmailBillOutputIDs(aggregated.IdentityKey, aggregatedVariant.Fingerprint, outputs[parserVersionID]) {
						evidence := &models.EmailBillCandidateEvidence{CandidateEvidenceId: r.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), VariantId: variant.VariantId, ParserOutputId: outputID, CreatedUnixTime: now}
						exists, err := sess.Where("variant_id=? AND parser_output_id=?", variant.VariantId, outputID).Exist(&models.EmailBillCandidateEvidence{})
						if err != nil {
							return err
						}
						if !exists {
							if _, err = sess.Insert(evidence); err != nil {
								return err
							}
						}
					}
				}
			}
			status := "awaiting_routing"
			selectedVariantID := int64(0)
			if aggregated.Conflicted {
				status = "awaiting_confirmation"
				if err := r.ensureConflict(sess, candidate.CandidateId, variantIDs, now); err != nil {
					return err
				}
			} else if len(variantIDs) == 1 {
				selectedVariantID = variantIDs[0]
			}
			candidate.Status = status
			candidate.SelectedVariantId = selectedVariantID
			candidate.UpdatedUnixTime = now
			if _, err = sess.ID(candidate.CandidateId).Cols("status", "selected_variant_id", "updated_unix_time").Update(candidate); err != nil {
				return err
			}
			if err := r.insertAuditEvent(sess, uid, run.MessageId, candidate.CandidateId, runID, "candidate_aggregated", "system", map[string]any{
				"messageFingerprint": messageFingerprint, "variantCount": len(variantIDs), "status": status,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// FinishRun appends the terminal run event and updates the current status.
func (r *EmailBillAutomationRepository) FinishRun(c core.Context, uid, runID int64, status, reason string) error {
	now := time.Now().Unix()
	return r.UserDataDB(uid).DoTransaction(c, func(sess *xorm.Session) error {
		run := &models.EmailBillImportRun{}
		has, err := sess.ID(runID).Get(run)
		if err != nil {
			return err
		}
		if !has {
			return fmt.Errorf("email bill import run %d not found", runID)
		}
		previous := run.Status
		run.Status = status
		run.CompletedUnixTime = now
		if _, err = sess.ID(runID).Cols("status", "completed_unix_time").Update(run); err != nil {
			return err
		}
		event := &models.EmailBillImportRunEvent{
			EventId: r.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), ImportRunId: runID,
			FromStatus: previous, ToStatus: status, Reason: reason, CreatedUnixTime: now,
		}
		if _, err = sess.Insert(event); err != nil {
			return err
		}
		return r.insertAuditEvent(sess, uid, run.MessageId, 0, runID, "run_finished", "system", map[string]any{"status": status, "reason": reason})
	})
}

func (r *EmailBillAutomationRepository) getOrCreateCandidate(sess *xorm.Session, uid, messageID int64, aggregated emailbill.AggregatedCandidate, now int64) (*models.EmailBillCandidate, error) {
	candidate := &models.EmailBillCandidate{}
	has, err := sess.Where("uid=? AND identity_key=? AND identity_version=?", uid, aggregated.IdentityKey, aggregated.IdentityVersion).Get(candidate)
	if err != nil {
		return nil, err
	}
	if has {
		return candidate, nil
	}
	candidate = &models.EmailBillCandidate{
		CandidateId: r.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), Uid: uid, MessageId: messageID,
		IdentityKey: aggregated.IdentityKey, IdentityVersion: aggregated.IdentityVersion,
		Status: "aggregating", CreatedUnixTime: now, UpdatedUnixTime: now,
	}
	_, err = sess.Insert(candidate)
	return candidate, err
}

func (r *EmailBillAutomationRepository) getOrCreateVariant(sess *xorm.Session, candidateID int64, aggregated emailbill.AggregatedVariant, now int64) (*models.EmailBillCandidateVariant, error) {
	variant := &models.EmailBillCandidateVariant{}
	has, err := sess.Where("candidate_id=? AND bill_fingerprint=? AND fingerprint_version=?", candidateID, aggregated.Fingerprint, aggregated.FingerprintVersion).Get(variant)
	if err != nil {
		return nil, err
	}
	if has {
		return variant, nil
	}
	bill := aggregated.Bill
	variant = &models.EmailBillCandidateVariant{
		VariantId: r.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), CandidateId: candidateID,
		BillFingerprint: aggregated.Fingerprint, FingerprintVersion: aggregated.FingerprintVersion,
		TransactionUnixTime: bill.OccurredAt.Unix(), Amount: bill.AmountMinor, Currency: bill.Currency,
		Direction: bill.FlowType, Merchant: bill.Merchant, Description: bill.Description,
		Bank: bill.AccountHint.Bank, CardType: bill.AccountHint.Kind, CardLast4: bill.AccountHint.Last4,
		ExternalId: bill.ExternalID, CreatedUnixTime: now,
	}
	_, err = sess.Insert(variant)
	return variant, err
}

func (r *EmailBillAutomationRepository) ensureConflict(sess *xorm.Session, candidateID int64, variantIDs []int64, now int64) error {
	conflict := &models.EmailBillCandidateConflict{}
	has, err := sess.Where("candidate_id=? AND status=?", candidateID, "open").Get(conflict)
	if err != nil {
		return err
	}
	if !has {
		conflict = &models.EmailBillCandidateConflict{ConflictId: r.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), CandidateId: candidateID, Status: "open", CreatedUnixTime: now}
		if _, err = sess.Insert(conflict); err != nil {
			return err
		}
	}
	for _, variantID := range variantIDs {
		exists, err := sess.Where("conflict_id=? AND variant_id=?", conflict.ConflictId, variantID).Exist(&models.EmailBillCandidateConflictItem{})
		if err != nil {
			return err
		}
		if !exists {
			item := &models.EmailBillCandidateConflictItem{ConflictItemId: r.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), ConflictId: conflict.ConflictId, VariantId: variantID}
			if _, err = sess.Insert(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *EmailBillAutomationRepository) insertAuditEvent(sess *xorm.Session, uid, messageID, candidateID, runID int64, eventType, actorType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	event := &models.EmailBillAuditEvent{
		AuditEventId: r.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), Uid: uid, MessageId: messageID,
		CandidateId: candidateID, ImportRunId: runID, EventType: eventType, ActorType: actorType,
		PayloadJson: string(raw), CreatedUnixTime: time.Now().Unix(),
	}
	_, err = sess.Insert(event)
	return err
}

func parserOutputModel(id, parserRunID int64, sequence int32, bill emailbill.StandardBill, raw string, now int64) *models.EmailBillParserOutput {
	return &models.EmailBillParserOutput{
		ParserOutputId: id, ParserRunId: parserRunID, ExternalId: bill.ExternalID, BillSequence: sequence,
		TransactionUnixTime: bill.OccurredAt.Unix(), Amount: bill.AmountMinor, Currency: bill.Currency,
		Direction: bill.FlowType, Merchant: bill.Merchant, Description: bill.Description,
		Bank: bill.AccountHint.Bank, CardType: bill.AccountHint.Kind, CardLast4: bill.AccountHint.Last4,
		RawStandardBillJson: raw, CreatedUnixTime: now,
	}
}

func matchingEmailBillOutputIDs(identityKey, expectedFingerprint string, outputs []EmailBillSavedOutput) []int64 {
	ids := make([]int64, 0, len(outputs))
	for _, output := range outputs {
		fingerprint, _ := emailbill.BillFingerprint(emailbill.BillFingerprintInput{
			IdentityKey: identityKey, TransactionTime: output.Bill.OccurredAt,
			AmountMinor: output.Bill.AmountMinor, Currency: output.Bill.Currency,
			Direction: output.Bill.FlowType, Merchant: output.Bill.Merchant, Description: output.Bill.Description,
		})
		if fingerprint == expectedFingerprint {
			ids = append(ids, output.OutputID)
		}
	}
	return ids
}

func authenticationStatus(authenticated bool) string {
	if authenticated {
		return "passed"
	}
	return "failed"
}

func parserErrorType(message string) string {
	if message == "" {
		return ""
	}
	return "parser_error"
}
