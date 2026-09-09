package models

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmailBillParserOutputHasNoForbiddenReferences(t *testing.T) {
	outputType := reflect.TypeOf(EmailBillParserOutput{})

	for _, forbidden := range []string{"AccountId", "CategoryId", "TransactionId"} {
		_, exists := outputType.FieldByName(forbidden)
		assert.False(t, exists, "parser output must not contain %s", forbidden)
	}
}

func TestEmailBillCandidateStoresSelectedVariantAndCurrentDecisions(t *testing.T) {
	candidateType := reflect.TypeOf(EmailBillCandidate{})

	for _, field := range []string{
		"SelectedVariantId",
		"CurrentAccountDecisionId",
		"CurrentClassificationDecisionId",
	} {
		_, exists := candidateType.FieldByName(field)
		assert.True(t, exists, "candidate must contain %s", field)
	}
}

func TestEmailBillParserVersionSnapshotsMatcherAndRuntime(t *testing.T) {
	versionType := reflect.TypeOf(EmailBillParserRuleVersion{})

	for _, field := range []string{
		"MatcherJson",
		"MatcherHash",
		"SourceCode",
		"SourceHash",
		"Runtime",
		"RuntimeVersion",
		"ParserApiVersion",
		"VersionHash",
	} {
		_, exists := versionType.FieldByName(field)
		assert.True(t, exists, "parser version must contain %s", field)
	}
}
