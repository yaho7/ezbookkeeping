package services

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
)

// EmailBillFetchedMessage is the complete normalized mail input accepted by the
// automation pipeline. Authentication is checked before any user code runs.
type EmailBillFetchedMessage struct {
	RemoteMessageID      string
	Sender               string
	Subject              string
	ReceivedAt           time.Time
	Text                 string
	Headers              map[string]string
	Authenticated        bool
	RetainBody           bool
	AuthenticationDetail string
}

// EmailBillMessageInput is the immutable message evidence persisted at the
// start of a run.
type EmailBillMessageInput struct {
	UID                  int64
	MailboxID            int64
	RemoteMessageID      string
	Fingerprint          string
	FingerprintVersion   uint16
	Sender               string
	Subject              string
	ReceivedAt           time.Time
	BodyHash             string
	BodySummary          string
	BodyContent          string
	Authenticated        bool
	AuthenticationDetail string
}

// EmailBillPersistedRun is the repository-neutral run state.
type EmailBillPersistedRun struct {
	UID       int64
	RunID     int64
	MessageID int64
	Status    string
}

// EmailBillPersistedParserRun is one isolated parser outcome and its immutable
// standard output.
type EmailBillPersistedParserRun struct {
	UID             int64
	RunID           int64
	ParserVersionID int64
	Matched         bool
	Status          string
	Bills           []emailbill.StandardBill
	Stats           emailbill.ScriptStats
	InputHash       string
	ErrorMessage    string
}

// EmailBillSavedOutput binds one immutable persisted output to its bill value.
type EmailBillSavedOutput struct {
	OutputID int64
	Bill     emailbill.StandardBill
}

// EmailBillPipelineRepository is the durable boundary used by the pipeline.
// Implementations must enforce uniqueness for message and parser-run keys.
type EmailBillPipelineRepository interface {
	SaveMessageAndStartRun(core.Context, EmailBillMessageInput) (messageID int64, runID int64, duplicate bool, err error)
	SaveParserRun(core.Context, EmailBillPersistedParserRun) ([]EmailBillSavedOutput, error)
	SaveCandidates(core.Context, int64, int64, string, []emailbill.AggregatedCandidate, map[int64][]EmailBillSavedOutput) error
	FinishRun(core.Context, int64, int64, string, string) error
}

// EmailBillPipelineResult summarizes one message processing request.
type EmailBillPipelineResult struct {
	MessageID int64
	RunID     int64
	Duplicate bool
	Status    string
}

// EmailBillPipeline runs sandboxed rules and persists evidence without routing
// accounts, classifying categories, or creating transactions.
type EmailBillPipeline struct {
	repository EmailBillPipelineRepository
	parser     *emailbill.ScriptParser
}

// NewEmailBillPipeline creates an ingestion pipeline.
func NewEmailBillPipeline(repository EmailBillPipelineRepository, parser *emailbill.ScriptParser) *EmailBillPipeline {
	return &EmailBillPipeline{repository: repository, parser: parser}
}

// ProcessMessage persists identity first and then executes every matching rule.
func (p *EmailBillPipeline) ProcessMessage(c core.Context, uid, mailboxID int64, fetched EmailBillFetchedMessage, rules []emailbill.RunnableParserRule) (*EmailBillPipelineResult, error) {
	fingerprint, fingerprintVersion := emailbill.MessageFingerprint(emailbill.MessageIdentityInput{
		MailboxID: mailboxID, MessageID: fetched.RemoteMessageID, Sender: fetched.Sender,
		Subject: fetched.Subject, ReceivedAt: fetched.ReceivedAt, Body: fetched.Text,
	})
	bodyDigest := sha256.Sum256([]byte(fetched.Text))
	messageID, runID, duplicate, err := p.repository.SaveMessageAndStartRun(c, EmailBillMessageInput{
		UID: uid, MailboxID: mailboxID, RemoteMessageID: fetched.RemoteMessageID,
		Fingerprint: fingerprint, FingerprintVersion: fingerprintVersion,
		Sender: fetched.Sender, Subject: fetched.Subject, ReceivedAt: fetched.ReceivedAt,
		BodyHash: "sha256:" + hex.EncodeToString(bodyDigest[:]), BodySummary: summarizeEmailBody(fetched.Text), BodyContent: retainedEmailBillBody(fetched),
		Authenticated: fetched.Authenticated, AuthenticationDetail: fetched.AuthenticationDetail,
	})
	if err != nil {
		return nil, fmt.Errorf("persist email message: %w", err)
	}
	result := &EmailBillPipelineResult{MessageID: messageID, RunID: runID, Duplicate: duplicate}
	if duplicate {
		result.Status = "duplicate"
		return result, nil
	}
	if !fetched.Authenticated {
		result.Status = "rejected"
		if err := p.repository.FinishRun(c, uid, runID, result.Status, "email authentication failed"); err != nil {
			return nil, err
		}
		return result, nil
	}

	mail := emailbill.ScriptMail{
		MessageID: fetched.RemoteMessageID, Sender: fetched.Sender, Subject: fetched.Subject,
		ReceivedAt: fetched.ReceivedAt, Text: fetched.Text, Headers: fetched.Headers,
	}
	executions := emailbill.RunParserRules(p.parser, mail, rules)
	parserOutputs := make(map[int64][]EmailBillSavedOutput, len(executions))
	successful := make([]emailbill.ParserResult, 0, len(executions))
	matched, failures := 0, 0
	for _, execution := range executions {
		status := "not_matched"
		errorMessage := ""
		if execution.Matched {
			matched++
			if execution.Err != nil {
				status = "failed"
				failures++
				errorMessage = execution.Err.Error()
			} else {
				status = "succeeded"
				successful = append(successful, emailbill.ParserResult{ParserVersionID: execution.VersionID, Bills: execution.Bills})
			}
		}
		outputs, saveErr := p.repository.SaveParserRun(c, EmailBillPersistedParserRun{
			UID: uid, RunID: runID, ParserVersionID: execution.VersionID, Matched: execution.Matched,
			Status: status, Bills: execution.Bills, Stats: execution.Stats,
			InputHash: fingerprint, ErrorMessage: errorMessage,
		})
		if saveErr != nil {
			_ = p.repository.FinishRun(c, uid, runID, "failed", saveErr.Error())
			return nil, fmt.Errorf("persist parser run: %w", saveErr)
		}
		parserOutputs[execution.VersionID] = outputs
	}

	candidates := emailbill.AggregateBills(uid, fingerprint, successful)
	if err := p.repository.SaveCandidates(c, uid, runID, fingerprint, candidates, parserOutputs); err != nil {
		_ = p.repository.FinishRun(c, uid, runID, "failed", err.Error())
		return nil, fmt.Errorf("persist candidates: %w", err)
	}
	status := "succeeded"
	if failures > 0 && failures < matched {
		status = "partial_success"
	} else if failures > 0 {
		status = "failed"
	} else if len(candidates) == 0 {
		status = "no_output"
	}
	if err := p.repository.FinishRun(c, uid, runID, status, ""); err != nil {
		return nil, fmt.Errorf("finish import run: %w", err)
	}
	result.Status = status
	return result, nil
}

func retainedEmailBillBody(message EmailBillFetchedMessage) string {
	if message.RetainBody {
		return message.Text
	}
	return ""
}

func summarizeEmailBody(body string) string {
	const maximum = 512
	value := strings.Join(strings.Fields(strings.TrimSpace(body)), " ")
	if len([]rune(value)) > maximum {
		return string([]rune(value)[:maximum])
	}
	return value
}
