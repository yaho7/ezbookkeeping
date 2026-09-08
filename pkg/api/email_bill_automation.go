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
}

// EmailBillAutomation is the authenticated email automation API singleton.
var EmailBillAutomation = &EmailBillAutomationApi{service: services.EmailBillAutomation}

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

// RoutingRuleListHandler returns account routing rules.
func (a *EmailBillAutomationApi) RoutingRuleListHandler(c *core.WebContext) (any, *errs.Error) {
	infos, err := a.service.ListRoutingRules(c, c.GetCurrentUid())
	if err != nil {
		return nil, errs.Or(err, errs.ErrOperationFailed)
	}
	return infos, nil
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
	return info, nil
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
	return infos, nil
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
	return info, nil
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

func emailBillParserRuleResponse(info *services.EmailBillParserRuleInfo) *emailBillParserRuleInfoResponse {
	matcher := emailbill.ParserMatcher{}
	_ = json.Unmarshal([]byte(info.Version.MatcherJson), &matcher)
	return &emailBillParserRuleInfoResponse{
		ID: strconv.FormatInt(info.Rule.ParserRuleId, 10), Name: info.Rule.Name, Bank: info.Rule.Bank,
		Enabled: info.Rule.Enabled, Priority: info.Rule.Priority,
		VersionID: strconv.FormatInt(info.Version.ParserRuleVersionId, 10), Version: info.Version.Version,
		Matcher: matcher, SourceCode: info.Version.SourceCode, RuntimeVersion: info.Version.RuntimeVersion,
		UpdatedUnixTime: info.Rule.UpdatedUnixTime,
	}
}

func emailBillParserTestServiceRequest(uid int64, request emailBillParserTestRequest) (services.EmailBillParserTestRequest, error) {
	receivedAt, err := time.Parse(time.RFC3339, request.Mail.ReceivedAt)
	if err != nil {
		return services.EmailBillParserTestRequest{}, fmt.Errorf("receivedAt must be RFC3339: %w", err)
	}
	return services.EmailBillParserTestRequest{
		UID: uid, Matcher: request.Matcher, SourceCode: request.SourceCode,
		Mail: services.EmailBillFetchedMessage{
			RemoteMessageID: request.Mail.MessageID, Sender: request.Mail.Sender, Subject: request.Mail.Subject,
			ReceivedAt: receivedAt, Text: request.Mail.Text, Headers: request.Mail.Headers, Authenticated: true,
		},
	}, nil
}
