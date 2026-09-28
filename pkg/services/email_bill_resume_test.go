package services

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/datastore"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/models"
)

func TestEmailBillResumePagesBeyond100AndContinuesAfterIndividualFailure(t *testing.T) {
	s := emailBillDefaultsTestStore(t, new(models.EmailBillCandidate), new(models.EmailBillInboundMessage), new(models.EmailBillImportRun))
	c := core.NewNullContext()
	_, err := s.UserDataDB(7).NewSession(c).Insert(
		&models.EmailBillInboundMessage{MessageId: 20, Uid: 7, MailboxId: 70},
		&models.EmailBillImportRun{ImportRunId: 50, MessageId: 20})
	require.NoError(t, err)
	for index := int64(0); index < 205; index++ {
		status := []string{"awaiting_account", "awaiting_classification", "awaiting_routing", "import_failed", "ready", "awaiting_confirmation"}[index%6]
		_, err = s.UserDataDB(7).NewSession(c).Insert(&models.EmailBillCandidate{
			CandidateId: 1000 + index, Uid: 7, MessageId: 20, SelectedVariantId: 40,
			Status: status, IdentityKey: fmt.Sprintf("pending:%d", index), IdentityVersion: 1,
		})
		require.NoError(t, err)
	}
	for index, status := range []string{"conflict", "imported"} {
		_, err = s.UserDataDB(7).NewSession(c).Insert(&models.EmailBillCandidate{
			CandidateId: int64(2000 + index), Uid: 7, MessageId: 20, SelectedVariantId: 40,
			Status: status, IdentityKey: status, IdentityVersion: 1,
		})
		require.NoError(t, err)
	}
	_, err = s.UserDataDB(7).NewSession(c).Insert(
		&models.EmailBillCandidate{CandidateId: 3000, Uid: 8, MessageId: 20, SelectedVariantId: 40, Status: "awaiting_account"},
		&models.EmailBillCandidate{CandidateId: 4000, Uid: 7, MessageId: 20, Status: "awaiting_account", IdentityKey: "unselected"})
	require.NoError(t, err)
	finalizer := &EmailBillFinalizer{db: datastore.Container}
	seen := make(map[int64]int)
	resumed, failed := 0, 0
	err = finalizer.resumePending(c, 7, func(event emailbill.ScanEvent) error {
		assert.Equal(t, "resume", event.Kind)
		if event.Processed {
			if event.Status == "failed" {
				failed++
			} else {
				resumed++
			}
		}
		return nil
	}, func(_ core.Context, uid, mailboxID, runID int64, candidate *models.EmailBillCandidate) error {
		assert.Equal(t, int64(7), uid)
		assert.Equal(t, int64(70), mailboxID)
		assert.Equal(t, int64(50), runID)
		seen[candidate.CandidateId]++
		if candidate.CandidateId == 1000 {
			return errors.New("one bill failed")
		}
		_, err := s.UserDataDB(7).NewSession(c).ID(candidate.CandidateId).Cols("status").Update(&models.EmailBillCandidate{Status: "imported"})
		return err
	})
	require.ErrorContains(t, err, "1 failed")
	assert.Len(t, seen, 205)
	assert.Equal(t, 204, resumed)
	assert.Equal(t, 1, failed)
	for _, attempts := range seen {
		assert.Equal(t, 1, attempts, "pending failures must not loop in the same run")
	}
}
