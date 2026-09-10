package services

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"xorm.io/xorm"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/models"
)

type EmailBillMailboxPage struct {
	Messages []*models.EmailBillScanMessage `json:"messages"`
	Total    int64                          `json:"total"`
	Page     int                            `json:"page"`
	PageSize int                            `json:"pageSize"`
	Folders  []string                       `json:"folders"`
}

func (s *EmailBillSyncService) ListMessages(c core.Context, uid int64, folder, status, search string, page int) (*EmailBillMailboxPage, error) {
	if page < 1 || page > 1000000 {
		page = 1
	}
	result := &EmailBillMailboxPage{Messages: make([]*models.EmailBillScanMessage, 0), Page: page, PageSize: 50, Folders: make([]string, 0)}
	db := s.UserDataDB(uid)
	query := func() *xorm.Session {
		session := db.NewSession(c).Where("uid=?", uid)
		if folder != "" {
			session = session.And("folder=?", folder)
		}
		if status != "" {
			session = session.And("status=?", status)
		}
		if search = strings.TrimSpace(search); search != "" {
			pattern := "%" + search + "%"
			session = session.And("(subject LIKE ? OR sender LIKE ?)", pattern, pattern)
		}
		return session
	}
	var err error
	result.Total, err = query().Count(&models.EmailBillScanMessage{})
	if err != nil {
		return nil, err
	}
	if err = query().OrderBy("received_unix_time desc, entry_id desc").Limit(result.PageSize, (page-1)*result.PageSize).Find(&result.Messages); err != nil {
		return nil, err
	}
	var folders []*models.EmailBillScanMessage
	if err = db.NewSession(c).Where("uid=?", uid).Distinct("folder").Find(&folders); err != nil {
		return nil, err
	}
	for _, item := range folders {
		result.Folders = append(result.Folders, item.Folder)
	}
	return result, nil
}

// MessageDetail verifies the scan row and inbound ownership before reading evidence.
func (s *EmailBillSyncService) MessageDetail(c core.Context, uid, id int64) (map[string]any, error) {
	db := s.UserDataDB(uid)
	entry := &models.EmailBillScanMessage{}
	has, err := db.NewSession(c).Where("uid=? AND entry_id=?", uid, id).Get(entry)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, fmt.Errorf("email not found")
	}
	parsers := make([]map[string]any, 0)
	candidates := make([]map[string]any, 0)
	result := map[string]any{"message": entry, "text": "", "bodySummary": "", "parsers": parsers, "candidates": candidates}
	if entry.MessageId == 0 {
		return result, nil
	}
	inbound := &models.EmailBillInboundMessage{}
	has, err = db.NewSession(c).Where("uid=? AND message_id=?", uid, entry.MessageId).Get(inbound)
	if err != nil {
		return nil, err
	}
	if !has {
		return result, nil
	}
	result["text"], result["bodySummary"] = inbound.BodyContent, inbound.BodySummary
	result["receivedAt"] = time.Unix(inbound.ReceivedUnixTime, 0).Format(time.RFC3339)
	run := &models.EmailBillImportRun{}
	has, err = db.NewSession(c).Where("message_id=?", inbound.MessageId).OrderBy("import_run_id desc").Get(run)
	if err != nil {
		return nil, err
	}
	if has {
		result["runStatus"] = run.Status
		var runs []*models.EmailBillParserRun
		if err = db.NewSession(c).Where("import_run_id=?", run.ImportRunId).OrderBy("parser_run_id asc").Limit(100).Find(&runs); err != nil {
			return nil, err
		}
		for _, parser := range runs {
			version := &models.EmailBillParserRuleVersion{}
			found, err := db.NewSession(c).ID(parser.ParserRuleVersionId).Get(version)
			if err != nil {
				return nil, err
			}
			name := ""
			if found {
				rule := &models.EmailBillParserRule{}
				if _, err = db.NewSession(c).Where("uid=? AND parser_rule_id=?", uid, version.ParserRuleId).Get(rule); err != nil {
					return nil, err
				}
				name = rule.Name
			}
			outputs, err := db.NewSession(c).Where("parser_run_id=?", parser.ParserRunId).Count(&models.EmailBillParserOutput{})
			if err != nil {
				return nil, err
			}
			parsers = append(parsers, map[string]any{"name": name, "version": version.Version, "matched": parser.Matched,
				"status": parser.Status, "errorMessage": parser.ErrorMessage, "durationMillis": parser.DurationMillis, "outputs": outputs})
		}
	}
	var bills []*models.EmailBillCandidate
	if err = db.NewSession(c).Where("uid=? AND message_id=?", uid, inbound.MessageId).OrderBy("candidate_id asc").Limit(100).Find(&bills); err != nil {
		return nil, err
	}
	for _, bill := range bills {
		variant := &models.EmailBillCandidateVariant{}
		if bill.SelectedVariantId > 0 {
			if _, err = db.NewSession(c).Where("candidate_id=? AND variant_id=?", bill.CandidateId, bill.SelectedVariantId).Get(variant); err != nil {
				return nil, err
			}
		}
		candidates = append(candidates, map[string]any{"id": strconv.FormatInt(bill.CandidateId, 10), "status": bill.Status,
			"amount": variant.Amount, "currency": variant.Currency, "merchant": variant.Merchant})
	}
	result["parsers"], result["candidates"] = parsers, candidates
	return result, nil
}
