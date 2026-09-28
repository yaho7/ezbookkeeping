package services

import (
	"errors"
	"fmt"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/models"
)

// ResumePending continues already parsed bills without downloading their mail.
// Conflicted and imported bills are never replayed automatically.
func (s *EmailBillFinalizer) ResumePending(c core.Context, uid int64, observer emailbill.ScanObserver) error {
	return s.resumePending(c, uid, observer, s.finalizeCandidate)
}

func (s *EmailBillFinalizer) resumePending(c core.Context, uid int64, observer emailbill.ScanObserver,
	finalize func(core.Context, int64, int64, int64, *models.EmailBillCandidate) error) error {
	database := s.db.UserDataStore.Choose(uid)
	last := &models.EmailBillCandidate{}
	has, err := database.NewSession(c).Where("uid=? AND selected_variant_id>?", uid, 0).
		In("status", "awaiting_routing", "awaiting_account", "awaiting_classification", "awaiting_confirmation", "ready", "import_failed").OrderBy("candidate_id desc").Limit(1).Get(last)
	if err != nil || !has {
		return err
	}
	if observer != nil {
		if err := observer(emailbill.ScanEvent{Kind: "resume", Status: "processing"}); err != nil {
			return err
		}
	}
	var firstFailure error
	failures, cursor := 0, int64(0)
	for cursor < last.CandidateId {
		if err := c.Err(); err != nil {
			return errors.Join(firstFailure, err)
		}
		var candidates []*models.EmailBillCandidate
		if err := database.NewSession(c).Where("uid=? AND selected_variant_id>? AND candidate_id>? AND candidate_id<=?", uid, 0, cursor, last.CandidateId).
			In("status", "awaiting_routing", "awaiting_account", "awaiting_classification", "awaiting_confirmation", "ready", "import_failed").OrderBy("candidate_id asc").Limit(100).Find(&candidates); err != nil {
			return errors.Join(firstFailure, err)
		}
		if len(candidates) == 0 {
			break
		}
		for _, candidate := range candidates {
			if err := c.Err(); err != nil {
				return errors.Join(firstFailure, err)
			}
			// Advance even when a candidate remains pending after a failure.
			cursor = candidate.CandidateId
			message := &models.EmailBillInboundMessage{}
			hasMessage, resumeErr := database.NewSession(c).Where("uid=? AND message_id=?", uid, candidate.MessageId).Get(message)
			if resumeErr == nil && !hasMessage {
				resumeErr = fmt.Errorf("inbound message not found")
			}
			run := &models.EmailBillImportRun{}
			if resumeErr == nil {
				var hasRun bool
				hasRun, resumeErr = database.NewSession(c).Where("message_id=?", message.MessageId).
					OrderBy("started_unix_time desc, import_run_id desc").Limit(1).Get(run)
				if resumeErr == nil && !hasRun {
					resumeErr = fmt.Errorf("import run not found")
				}
			}
			if resumeErr == nil {
				resumeErr = finalize(c, uid, message.MailboxId, run.ImportRunId, candidate)
			}
			event := emailbill.ScanEvent{Kind: "resume", Status: "succeeded", Processed: true,
				MessageID: candidate.MessageId, RunID: run.ImportRunId}
			if resumeErr != nil {
				failures++
				if firstFailure == nil {
					firstFailure = fmt.Errorf("candidate %d: %w", candidate.CandidateId, resumeErr)
				}
				event.Status, event.Reason = "failed", resumeErr.Error()
			}
			if observer != nil {
				if err := observer(event); err != nil {
					return errors.Join(firstFailure, err)
				}
			}
		}
	}
	if observer != nil {
		if err := observer(emailbill.ScanEvent{Kind: "resume", Status: "finished"}); err != nil {
			return errors.Join(firstFailure, err)
		}
	}
	if failures > 0 {
		return fmt.Errorf("resume pending bills: %d failed (first failure: %w)", failures, firstFailure)
	}
	return nil
}
