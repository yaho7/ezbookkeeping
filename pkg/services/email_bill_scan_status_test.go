package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmailBillScanOutcomePreservesDurableBookkeepingResult(t *testing.T) {
	assert.Equal(t, "imported", emailBillScanOutcome("succeeded", []string{"imported"}))
	assert.Equal(t, "partial_success", emailBillScanOutcome("succeeded", []string{"imported", "awaiting_account"}))
	assert.Equal(t, "failed", emailBillScanOutcome("succeeded", []string{"import_failed"}))
	assert.Equal(t, "no_output", emailBillScanOutcome("no_output", nil))
	assert.Equal(t, "succeeded", emailBillScanOutcome("succeeded", []string{"awaiting_account"}))
}
