package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/errs"
	"github.com/mayswind/ezbookkeeping/pkg/services"
)

// EmailBillAutomationApi exposes user-owned automation rules and dry runs.
type EmailBillAutomationApi struct {
	service *services.EmailBillAutomationService
	review  *services.EmailBillReviewService
}

// EmailBillAutomation is the authenticated email automation API singleton.
var EmailBillAutomation = &EmailBillAutomationApi{service: services.EmailBillAutomation, review: services.EmailBillReview}

type emailBillParserRuleRequest struct {
	ID         int64                   `json:"id,string"`
	Name       string                  `json:"name" binding:"required"`
	Bank       string                  `json:"bank"`
	Enabled    bool                    `json:"enabled"`
	Priority   int32                   `json:"priority"`
	Matcher    emailbill.ParserMatcher `json:"matcher"`
	SourceCode string                  `json:"sourceCode" binding:"required"`
}

type emailBillRuleIDRequest struct {
	ID int64 `json:"id,string" binding:"required"`
}

type emailBillTestMailRequest struct {
	MessageID  string            `json:"messageId"`
	Sender     string            `json:"sender" binding:"required"`
	Subject    string            `json:"subject"`
	ReceivedAt string            `json:"receivedAt" binding:"required"`
	Text       string            `json:"text" binding:"required"`
	Headers    map[string]string `json:"headers"`
}

type emailBillParserTestRequest struct {
	Matcher    emailbill.ParserMatcher  `json:"matcher"`
	SourceCode string                   `json:"sourceCode" binding:"required"`
	Mail       emailBillTestMailRequest `json:"mail" binding:"required"`
}

type emailBillParserGenerateRequest struct {
	Mail emailBillTestMailRequest `json:"mail" binding:"required"`
}

type emailBillParserRuleInfoResponse struct {
	ID              string                  `json:"id"`
	Name            string                  `json:"name"`
	Bank            string                  `json:"bank"`
	Enabled         bool                    `json:"enabled"`
	Priority        int32                   `json:"priority"`
	VersionID       string                  `json:"versionId"`
	Version         int32                   `json:"version"`
	Matcher         emailbill.ParserMatcher `json:"matcher"`
	SourceCode      string                  `json:"sourceCode"`
	RuntimeVersion  string                  `json:"runtimeVersion"`
	CreatedBy       string                  `json:"createdBy"`
	UpdatedUnixTime int64                   `json:"updatedUnixTime"`
}

type emailBillRoutingRuleRequest struct {
	ID              int64  `json:"id,string"`
	Enabled         bool   `json:"enabled"`
	Priority        int32  `json:"priority"`
	Bank            string `json:"bank"`
	Kind            string `json:"kind"`
	Last4           string `json:"last4"`
	Currency        string `json:"currency"`
	MailboxID       int64  `json:"mailboxId,string"`
	TargetAccountID int64  `json:"targetAccountId,string" binding:"required"`
}

type emailBillClassificationRuleRequest struct {
	ID              int64   `json:"id,string"`
	Origin          string  `json:"origin" binding:"required"`
	Enabled         bool    `json:"enabled"`
	Priority        int32   `json:"priority"`
	MerchantPattern string  `json:"merchantPattern" binding:"required"`
	MatchType       string  `json:"matchType" binding:"required"`
	Bank            string  `json:"bank"`
	AccountID       int64   `json:"accountId,string"`
	FlowType        string  `json:"flowType"`
	CategoryID      int64   `json:"categoryId,string" binding:"required"`
	Confidence      float64 `json:"confidence"`
}

type emailBillRoutingRuleResponse struct {
	ID              string `json:"id"`
	Enabled         bool   `json:"enabled"`
	Priority        int32  `json:"priority"`
	VersionID       string `json:"versionId"`
	Version         int32  `json:"version"`
	Bank            string `json:"bank"`
	Kind            string `json:"kind"`
	Last4           string `json:"last4"`
	Currency        string `json:"currency"`
	MailboxID       string `json:"mailboxId"`
	TargetAccountID string `json:"targetAccountId"`
	UpdatedUnixTime int64  `json:"updatedUnixTime"`
}

type emailBillClassificationRuleResponse struct {
	ID              string  `json:"id"`
	Origin          string  `json:"origin"`
	Enabled         bool    `json:"enabled"`
	Priority        int32   `json:"priority"`
	VersionID       string  `json:"versionId"`
	Version         int32   `json:"version"`
	MerchantPattern string  `json:"merchantPattern"`
	MatchType       string  `json:"matchType"`
	Bank            string  `json:"bank"`
	AccountID       string  `json:"accountId"`
	FlowType        string  `json:"flowType"`
	CategoryID      string  `json:"categoryId"`
	Confidence      float64 `json:"confidence"`
	UpdatedUnixTime int64   `json:"updatedUnixTime"`
}

type emailBillCandidateConfirmRequest struct {
	CandidateID int64 `json:"candidateId,string" binding:"required"`
	VariantID   int64 `json:"variantId,string" binding:"required"`
	AccountID   int64 `json:"accountId,string" binding:"required"`
	CategoryID  int64 `json:"categoryId,string" binding:"required"`
}

type emailBillCandidateRetryRequest struct {
	CandidateID int64 `json:"candidateId,string" binding:"required"`
}

type emailBillCandidateListRequest struct {
	Status string `form:"status"`
}

type emailBillAuditListRequest struct {
	CandidateID int64 `form:"candidate_id,string" binding:"required"`
}

// ParserRuleListHandler returns every parser and its current immutable version.
func (a *EmailBillAutomationApi) ParserRuleListHandler(c *core.WebContext) (any, *errs.Error) {
	infos, err := a.service.ListParserRules(c, c.GetCurrentUid())
	if err != nil {
		return nil, errs.Or(err, errs.ErrOperationFailed)
	}
	responses := make([]*emailBillParserRuleInfoResponse, 0, len(infos))
	for _, info := range infos {
		responses = append(responses, emailBillParserRuleResponse(info))
	}
	return responses, nil
}

// ParserRuleSaveHandler creates or revises one parser rule.
func (a *EmailBillAutomationApi) ParserRuleSaveHandler(c *core.WebContext) (any, *errs.Error) {
	request := emailBillParserRuleRequest{}
	if err := c.ShouldBindJSON(&request); err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	info, err := a.service.SaveParserRule(c, c.GetCurrentUid(), services.EmailBillParserRuleInput{
		RuleID: request.ID, Name: request.Name, Bank: request.Bank, Enabled: request.Enabled,
		Priority: request.Priority, Matcher: request.Matcher, Source: request.SourceCode,
	})
	if err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	return emailBillParserRuleResponse(info), nil
}

// ParserRuleDisableHandler disables one user-owned parser.
func (a *EmailBillAutomationApi) ParserRuleDisableHandler(c *core.WebContext) (any, *errs.Error) {
	request := emailBillRuleIDRequest{}
	if err := c.ShouldBindJSON(&request); err != nil {
		return false, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	if err := a.service.DisableParserRule(c, c.GetCurrentUid(), request.ID); err != nil {
		return false, errs.Or(err, errs.ErrOperationFailed)
	}
	return true, nil
}

// ParserTestHandler runs supplied code against supplied mail without persistence.
func (a *EmailBillAutomationApi) ParserTestHandler(c *core.WebContext) (any, *errs.Error) {
	request := emailBillParserTestRequest{}
	if err := c.ShouldBindJSON(&request); err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	serviceRequest, err := emailBillParserTestServiceRequest(c.GetCurrentUid(), request)
	if err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	result, err := a.service.TestParser(serviceRequest)
	if err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	return result, nil
}

// ParserGenerateHandler asks the global application LLM for a draft and dry-runs it.
func (a *EmailBillAutomationApi) ParserGenerateHandler(c *core.WebContext) (any, *errs.Error) {
	request := emailBillParserGenerateRequest{}
	if err := c.ShouldBindJSON(&request); err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	mail, err := emailBillTestMailServiceRequest(request.Mail)
	if err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	draft, err := a.service.GenerateParserDraft(c, c.GetCurrentUid(), mail)
	if err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	return draft, nil
}

// RoutingRuleListHandler returns account routing rules.
func (a *EmailBillAutomationApi) RoutingRuleListHandler(c *core.WebContext) (any, *errs.Error) {
	infos, err := a.service.ListRoutingRules(c, c.GetCurrentUid())
	if err != nil {
		return nil, errs.Or(err, errs.ErrOperationFailed)
	}
	responses := make([]*emailBillRoutingRuleResponse, 0, len(infos))
	for _, info := range infos {
		responses = append(responses, emailBillRoutingRuleResponseFromInfo(info))
	}
	return responses, nil
}

// RoutingRuleSaveHandler creates or revises an account route.
func (a *EmailBillAutomationApi) RoutingRuleSaveHandler(c *core.WebContext) (any, *errs.Error) {
	request := emailBillRoutingRuleRequest{}
	if err := c.ShouldBindJSON(&request); err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	info, err := a.service.SaveRoutingRule(c, c.GetCurrentUid(), services.EmailBillRoutingRuleInput{
		RuleID: request.ID, Enabled: request.Enabled, Priority: request.Priority, Bank: request.Bank,
		Kind: request.Kind, Last4: request.Last4, Currency: request.Currency, MailboxID: request.MailboxID,
		TargetAccountID: request.TargetAccountID,
	})
	if err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	return emailBillRoutingRuleResponseFromInfo(info), nil
}

// RoutingRuleDisableHandler disables an account route.
func (a *EmailBillAutomationApi) RoutingRuleDisableHandler(c *core.WebContext) (any, *errs.Error) {
	request := emailBillRuleIDRequest{}
	if err := c.ShouldBindJSON(&request); err != nil {
		return false, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	if err := a.service.DisableRoutingRule(c, c.GetCurrentUid(), request.ID); err != nil {
		return false, errs.Or(err, errs.ErrOperationFailed)
	}
	return true, nil
}

// ClassificationRuleListHandler returns manual and learned mappings.
func (a *EmailBillAutomationApi) ClassificationRuleListHandler(c *core.WebContext) (any, *errs.Error) {
	infos, err := a.service.ListClassificationRules(c, c.GetCurrentUid())
	if err != nil {
		return nil, errs.Or(err, errs.ErrOperationFailed)
	}
	responses := make([]*emailBillClassificationRuleResponse, 0, len(infos))
	for _, info := range infos {
		responses = append(responses, emailBillClassificationRuleResponseFromInfo(info))
	}
	return responses, nil
}

// ClassificationRuleSaveHandler creates or revises a merchant mapping.
func (a *EmailBillAutomationApi) ClassificationRuleSaveHandler(c *core.WebContext) (any, *errs.Error) {
	request := emailBillClassificationRuleRequest{}
	if err := c.ShouldBindJSON(&request); err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	info, err := a.service.SaveClassificationRule(c, c.GetCurrentUid(), services.EmailBillClassificationRuleInput{
		RuleID: request.ID, Origin: request.Origin, Enabled: request.Enabled, Priority: request.Priority,
		MerchantPattern: request.MerchantPattern, MatchType: request.MatchType, Bank: request.Bank,
		AccountID: request.AccountID, FlowType: request.FlowType, CategoryID: request.CategoryID, Confidence: request.Confidence,
	})
	if err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	return emailBillClassificationRuleResponseFromInfo(info), nil
}

// ClassificationRuleDisableHandler disables a manual or learned mapping.
func (a *EmailBillAutomationApi) ClassificationRuleDisableHandler(c *core.WebContext) (any, *errs.Error) {
	request := emailBillRuleIDRequest{}
	if err := c.ShouldBindJSON(&request); err != nil {
		return false, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	if err := a.service.DisableClassificationRule(c, c.GetCurrentUid(), request.ID); err != nil {
		return false, errs.Or(err, errs.ErrOperationFailed)
	}
	return true, nil
}

// ClassificationRuleDeleteHandler soft-deletes one user-owned mapping while preserving audit history.
func (a *EmailBillAutomationApi) ClassificationRuleDeleteHandler(c *core.WebContext) (any, *errs.Error) {
	request := emailBillRuleIDRequest{}
	if err := c.ShouldBindJSON(&request); err != nil {
		return false, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	if err := a.service.DeleteClassificationRule(c, c.GetCurrentUid(), request.ID); err != nil {
		return false, errs.Or(err, errs.ErrOperationFailed)
	}
	return true, nil
}

// MessageListHandler returns recent stored messages that can be loaded into the test bench.
func (a *EmailBillAutomationApi) MessageListHandler(c *core.WebContext) (any, *errs.Error) {
	messages, err := a.review.ListMessages(c, c.GetCurrentUid())
	if err != nil {
		return nil, errs.Or(err, errs.ErrOperationFailed)
	}
	responses := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		responses = append(responses, map[string]any{
			"id": strconv.FormatInt(message.MessageId, 10), "messageId": message.RemoteMessageId,
			"sender": message.Sender, "subject": message.Subject, "receivedAt": time.Unix(message.ReceivedUnixTime, 0).Format(time.RFC3339),
			"text": message.BodyContent, "bodySummary": message.BodySummary,
		})
	}
	return responses, nil
}

// CandidateListHandler returns pending and historical candidates.
func (a *EmailBillAutomationApi) CandidateListHandler(c *core.WebContext) (any, *errs.Error) {
	request := emailBillCandidateListRequest{}
	if err := c.ShouldBindQuery(&request); err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	infos, err := a.review.ListCandidates(c, c.GetCurrentUid(), request.Status)
	if err != nil {
		return nil, errs.Or(err, errs.ErrOperationFailed)
	}
	responses := make([]map[string]any, 0, len(infos))
	for _, info := range infos {
		variants := make([]map[string]any, 0, len(info.Variants))
		for _, variant := range info.Variants {
			variants = append(variants, map[string]any{
				"id": strconv.FormatInt(variant.VariantId, 10), "amount": variant.Amount, "currency": variant.Currency,
				"direction": variant.Direction, "merchant": variant.Merchant, "description": variant.Description,
				"occurredAt": time.Unix(variant.TransactionUnixTime, 0).Format(time.RFC3339),
				"bank":       variant.Bank, "kind": variant.CardType, "last4": variant.CardLast4,
			})
		}
		accountID, categoryID := int64(0), int64(0)
		if info.AccountDecision != nil {
			accountID = info.AccountDecision.AccountId
		}
		if info.ClassificationDecision != nil {
			categoryID = info.ClassificationDecision.CategoryId
		}
		responses = append(responses, map[string]any{
			"id": strconv.FormatInt(info.Candidate.CandidateId, 10), "status": info.Candidate.Status,
			"selectedVariantId": strconv.FormatInt(info.Candidate.SelectedVariantId, 10), "variants": variants,
			"accountId": strconv.FormatInt(accountID, 10), "categoryId": strconv.FormatInt(categoryID, 10),
			"transactionId": strconv.FormatInt(info.TransactionID, 10), "updatedUnixTime": info.Candidate.UpdatedUnixTime,
		})
	}
	return responses, nil
}

// CandidateConfirmHandler applies a user correction and imports exactly once.
func (a *EmailBillAutomationApi) CandidateConfirmHandler(c *core.WebContext) (any, *errs.Error) {
	request := emailBillCandidateConfirmRequest{}
	if err := c.ShouldBindJSON(&request); err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	transactionID, err := a.review.ConfirmCandidate(c, c.GetCurrentUid(), request.CandidateID, request.VariantID, request.AccountID, request.CategoryID)
	if err != nil {
		return nil, errs.Or(err, errs.ErrOperationFailed)
	}
	return map[string]string{"transactionId": strconv.FormatInt(transactionID, 10)}, nil
}

// CandidateRetryHandler resumes a failed or unresolved candidate.
func (a *EmailBillAutomationApi) CandidateRetryHandler(c *core.WebContext) (any, *errs.Error) {
	request := emailBillCandidateRetryRequest{}
	if err := c.ShouldBindJSON(&request); err != nil {
		return false, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	if err := a.review.RetryCandidate(c, c.GetCurrentUid(), request.CandidateID); err != nil {
		return false, errs.Or(err, errs.ErrOperationFailed)
	}
	return true, nil
}

// AuditListHandler returns the append-only audit chain for one owned candidate.
func (a *EmailBillAutomationApi) AuditListHandler(c *core.WebContext) (any, *errs.Error) {
	request := emailBillAuditListRequest{}
	if err := c.ShouldBindQuery(&request); err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	events, err := a.review.ListAudit(c, c.GetCurrentUid(), request.CandidateID)
	if err != nil {
		return nil, errs.Or(err, errs.ErrOperationFailed)
	}
	responses := make([]map[string]any, 0, len(events))
	for _, event := range events {
		responses = append(responses, map[string]any{
			"id": strconv.FormatInt(event.AuditEventId, 10), "eventType": event.EventType,
			"actorType": event.ActorType, "payload": json.RawMessage(event.PayloadJson), "createdUnixTime": event.CreatedUnixTime,
		})
	}
	return responses, nil
}

func emailBillParserRuleResponse(info *services.EmailBillParserRuleInfo) *emailBillParserRuleInfoResponse {
	matcher := emailbill.ParserMatcher{}
	_ = json.Unmarshal([]byte(info.Version.MatcherJson), &matcher)
	return &emailBillParserRuleInfoResponse{
		ID: strconv.FormatInt(info.Rule.ParserRuleId, 10), Name: info.Rule.Name, Bank: info.Rule.Bank,
		Enabled: info.Rule.Enabled, Priority: info.Rule.Priority,
		VersionID: strconv.FormatInt(info.Version.ParserRuleVersionId, 10), Version: info.Version.Version,
		Matcher: matcher, SourceCode: info.Version.SourceCode, RuntimeVersion: info.Version.RuntimeVersion,
		CreatedBy:       info.Version.CreatedBy,
		UpdatedUnixTime: info.Rule.UpdatedUnixTime,
	}
}

func emailBillRoutingRuleResponseFromInfo(info *services.EmailBillRoutingRuleInfo) *emailBillRoutingRuleResponse {
	return &emailBillRoutingRuleResponse{
		ID: strconv.FormatInt(info.Rule.RoutingRuleId, 10), Enabled: info.Rule.Enabled, Priority: info.Rule.Priority,
		VersionID: strconv.FormatInt(info.Version.RoutingRuleVersionId, 10), Version: info.Version.Version,
		Bank: info.Conditions.Bank, Kind: info.Conditions.Kind, Last4: info.Conditions.Last4, Currency: info.Conditions.Currency,
		MailboxID: strconv.FormatInt(info.Conditions.MailboxID, 10), TargetAccountID: strconv.FormatInt(info.Version.TargetAccountId, 10),
		UpdatedUnixTime: info.Rule.UpdatedUnixTime,
	}
}

func emailBillClassificationRuleResponseFromInfo(info *services.EmailBillClassificationRuleInfo) *emailBillClassificationRuleResponse {
	return &emailBillClassificationRuleResponse{
		ID: strconv.FormatInt(info.Rule.ClassificationRuleId, 10), Origin: info.Rule.Origin, Enabled: info.Rule.Enabled,
		Priority: info.Rule.Priority, VersionID: strconv.FormatInt(info.Version.ClassificationRuleVersionId, 10), Version: info.Version.Version,
		MerchantPattern: info.Version.MerchantPattern, MatchType: info.Version.MatchType,
		Bank: info.Scope.Bank, AccountID: strconv.FormatInt(info.Scope.AccountID, 10), FlowType: info.Scope.FlowType,
		CategoryID: strconv.FormatInt(info.Version.CategoryId, 10), Confidence: info.Version.Confidence,
		UpdatedUnixTime: info.Rule.UpdatedUnixTime,
	}
}

func emailBillParserTestServiceRequest(uid int64, request emailBillParserTestRequest) (services.EmailBillParserTestRequest, error) {
	mail, err := emailBillTestMailServiceRequest(request.Mail)
	if err != nil {
		return services.EmailBillParserTestRequest{}, err
	}
	return services.EmailBillParserTestRequest{UID: uid, Matcher: request.Matcher, SourceCode: request.SourceCode, Mail: mail}, nil
}

func emailBillTestMailServiceRequest(request emailBillTestMailRequest) (services.EmailBillFetchedMessage, error) {
	receivedAt, err := time.Parse(time.RFC3339, request.ReceivedAt)
	if err != nil {
		return services.EmailBillFetchedMessage{}, fmt.Errorf("receivedAt must be RFC3339: %w", err)
	}
	return services.EmailBillFetchedMessage{
		RemoteMessageID: request.MessageID, Sender: request.Sender, Subject: request.Subject,
		ReceivedAt: receivedAt, Text: request.Text, Headers: request.Headers,
	}, nil
}
