package services

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/datastore"
	"github.com/mayswind/ezbookkeeping/pkg/mail"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
	"github.com/mayswind/ezbookkeeping/pkg/templates"
)

type emailBillTaskContextKey struct{}

func SanitizeEmailBillNotificationError(reason string, config *settings.EmailBillConfig) string {
	return sanitizeEmailTaskError(reason, config)
}

func emailBillTaskID(c core.Context) int64 {
	id, _ := c.Value(emailBillTaskContextKey{}).(int64)
	return id
}

type EmailBillNotificationPreview struct {
	Subject string `json:"subject"`
	HTML    string `json:"html"`
}

type emailBillNotificationEntry struct {
	Merchant string
	Category string
	Amount   string
}

type emailBillNotificationCurrency struct {
	Currency    string
	Expense     int64
	Income      int64
	ExpenseText string
	IncomeText  string
}

type emailBillNotificationData struct {
	Language                                                       string
	Title, Description, Status, Accent, Time, Duration, URL, Error string
	Scanned, Imported, Uncategorized, Failed                       int64
	Entries                                                        []emailBillNotificationEntry
	Currencies                                                     []emailBillNotificationCurrency
	Folders                                                        []string
	More                                                           bool
	Test                                                           bool
	Copy                                                           map[string]string
}

// EmailBillNotificationService sends one detached result notification per task.
type EmailBillNotificationService struct {
	ServiceUsingDB
	newMailer func(*settings.SMTPConfig) (mail.Mailer, error)
}

var EmailBillNotifications = &EmailBillNotificationService{
	ServiceUsingDB: ServiceUsingDB{container: datastore.Container},
	newMailer:      func(config *settings.SMTPConfig) (mail.Mailer, error) { return mail.NewSecureMailer(config) },
}

func (s *EmailBillNotificationService) ImportedCount(c core.Context, task *models.EmailBillSyncTask) (int64, error) {
	EmailBillSync.metadataMu.Lock()
	defer EmailBillSync.metadataMu.Unlock()
	var row struct{ Total int64 }
	_, err := s.UserDataDB(task.Uid).NewSession(c).Table(new(models.Transaction)).Alias("t").
		Join("INNER", []string{"email_bill_transaction_import_attempt", "a"}, "t.transaction_id=a.transaction_id").
		Where("a.sync_task_id=? AND a.status=? AND t.uid=? AND t.deleted=?", task.TaskId, "completed", task.Uid, false).
		Select("count(DISTINCT a.transaction_id) AS total").Get(&row)
	return row.Total, err
}

func emailBillShouldNotify(task *models.EmailBillSyncTask, n *settings.EmailBillNotificationConfig) bool {
	if n == nil || !n.Enabled {
		return false
	}
	failed := task.Status == "failed" || task.Failed > 0
	switch n.Mode {
	case "changes_only":
		return task.Imported > 0
	case "errors_only":
		return failed
	case "changes_or_errors":
		return task.Imported > 0 || failed
	default:
		return true
	}
}

// Notify atomically claims pending delivery; notification failure never changes the import outcome.
func (s *EmailBillNotificationService) Notify(c core.Context, task *models.EmailBillSyncTask, config *settings.EmailBillConfig, rootURL, locale string) error {
	EmailBillSync.metadataMu.Lock()
	claimed, err := s.UserDataDB(task.Uid).NewSession(c).Where("uid=? AND task_id=? AND notification_status=?", task.Uid, task.TaskId, "pending").
		Cols("notification_status").Update(&models.EmailBillSyncTask{NotificationStatus: "sending"})
	EmailBillSync.metadataMu.Unlock()
	if err != nil || claimed != 1 {
		return err
	}
	preview, err := s.renderTask(c, task, config, rootURL, locale)
	if err == nil {
		err = s.send(config, preview)
	}
	status, reason, sentAt := "sent", "", time.Now().Unix()
	if err != nil {
		status, reason, sentAt = "failed", sanitizeEmailTaskError(err.Error(), config), 0
	}
	EmailBillSync.metadataMu.Lock()
	_, saveErr := s.UserDataDB(task.Uid).NewSession(c).Where("uid=? AND task_id=?", task.Uid, task.TaskId).
		Cols("notification_status", "notification_error", "notification_sent_unix_time").Update(&models.EmailBillSyncTask{
		NotificationStatus: status, NotificationError: reason, NotificationSentUnixTime: sentAt,
	})
	EmailBillSync.metadataMu.Unlock()
	if saveErr != nil {
		return saveErr
	}
	return err
}

func (s *EmailBillNotificationService) send(config *settings.EmailBillConfig, preview *EmailBillNotificationPreview) error {
	resolved := *config
	if config.Notification != nil {
		n := *config.Notification
		resolved.Notification = &n
	}
	if err := settings.NormalizeEmailBillNotification(&resolved, true); err != nil {
		return err
	}
	m, err := s.newMailer(settings.EmailBillNotificationSMTP(&resolved))
	if err != nil {
		return err
	}
	subject := preview.Subject
	if resolved.Notification.Subject != "" {
		subject = resolved.Notification.Subject
	}
	return m.SendMail(&mail.MailMessage{To: resolved.Notification.Recipient, Subject: subject, Body: preview.HTML})
}

func (s *EmailBillNotificationService) Test(config *settings.EmailBillConfig, rootURL, locale string) error {
	preview, err := s.Preview(rootURL, locale, "test")
	if err != nil {
		return err
	}
	return s.send(config, preview)
}

func (s *EmailBillNotificationService) renderTask(c core.Context, task *models.EmailBillSyncTask, config *settings.EmailBillConfig, rootURL, locale string) (*EmailBillNotificationPreview, error) {
	data := emailBillNotificationBase(task, rootURL, locale)
	data.Folders = append([]string(nil), config.Folders...)
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return nil, err
	}
	data.Time = time.Unix(task.CompletedUnixTime, 0).In(location).Format("2006-01-02 15:04 MST")
	data.Duration = fmt.Sprintf("%dm %ds", (task.CompletedUnixTime-task.StartedUnixTime)/60, (task.CompletedUnixTime-task.StartedUnixTime)%60)
	data.Error = sanitizeEmailTaskError(task.ErrorMessage, config)
	db := s.UserDataDB(task.Uid)
	var rows []struct {
		Merchant, Category, Currency, DecisionType string
		Amount                                     int64
		Type                                       int
	}
	err = func() error {
		EmailBillSync.metadataMu.Lock()
		defer EmailBillSync.metadataMu.Unlock()
		return db.NewSession(c).Table(new(models.Transaction)).Alias("t").
			Join("INNER", []string{"email_bill_transaction_import_attempt", "a"}, "t.transaction_id=a.transaction_id").
			Join("INNER", []string{"email_bill_transaction_import_intent", "i"}, "i.import_intent_id=a.import_intent_id").
			Join("INNER", []string{"email_bill_candidate", "c"}, "c.candidate_id=i.candidate_id").
			Join("INNER", []string{"email_bill_candidate_variant", "v"}, "v.variant_id=c.selected_variant_id").
			Join("INNER", []string{"account", "ac"}, "ac.account_id=t.account_id").
			Join("INNER", []string{"transaction_category", "cat"}, "cat.category_id=t.category_id").
			Join("LEFT", []string{"email_bill_classification_decision", "d"}, "d.classification_decision_id=c.current_classification_decision_id").
			Where("a.sync_task_id=? AND a.status=? AND t.uid=? AND t.deleted=?", task.TaskId, "completed", task.Uid, false).
			Select("v.merchant AS merchant, cat.name AS category, ac.currency AS currency, t.amount AS amount, t.type AS type, d.decision_type AS decision_type").
			OrderBy("t.created_unix_time DESC, t.transaction_id DESC").Find(&rows)
	}()
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.DecisionType == "fallback" {
			data.Uncategorized++
		}
		if len(data.Entries) < 6 {
			sign := "−"
			if row.Type == int(models.TRANSACTION_DB_TYPE_INCOME) {
				sign = "+"
			}
			data.Entries = append(data.Entries, emailBillNotificationEntry{Merchant: row.Merchant, Category: row.Category, Amount: sign + emailBillNotificationAmount(row.Amount) + " " + row.Currency})
		}
		index := -1
		for i := range data.Currencies {
			if data.Currencies[i].Currency == row.Currency {
				index = i
				break
			}
		}
		if index < 0 {
			data.Currencies = append(data.Currencies, emailBillNotificationCurrency{Currency: row.Currency})
			index = len(data.Currencies) - 1
		}
		if row.Type == int(models.TRANSACTION_DB_TYPE_INCOME) {
			data.Currencies[index].Income += row.Amount
		} else {
			data.Currencies[index].Expense += row.Amount
		}
	}
	for i := range data.Currencies {
		data.Currencies[i].ExpenseText = emailBillNotificationAmount(data.Currencies[i].Expense)
		data.Currencies[i].IncomeText = emailBillNotificationAmount(data.Currencies[i].Income)
	}
	data.More = len(rows) > len(data.Entries)
	return renderEmailBillNotification(data)
}

func emailBillNotificationAmount(amount int64) string {
	return fmt.Sprintf("%d.%02d", amount/100, amount%100)
}

func emailBillNotificationBase(task *models.EmailBillSyncTask, rootURL, locale string) emailBillNotificationData {
	copy := emailBillNotificationCopy(locale)
	d := emailBillNotificationData{Copy: copy, Scanned: task.Scanned, Imported: task.Imported, Failed: task.Failed,
		URL: strings.TrimRight(rootURL, "/") + "/desktop#/settings/email_bill", Accent: "#26734d", Status: copy["success"], Title: copy["completed"], Description: copy["description"]}
	d.Language = strings.ReplaceAll(locale, "_", "-")
	if d.Language == "" {
		d.Language = "en"
	}
	if task.Status == "failed" || task.Failed > 0 {
		d.Title, d.Status, d.Accent, d.Description = copy["failedTitle"], copy["attention"], "#b42318", copy["failedDescription"]
	}
	if task.Imported == 0 && task.Status != "failed" && task.Failed == 0 {
		d.Description = copy["empty"]
	}
	return d
}

func (s *EmailBillNotificationService) Preview(rootURL, locale, outcome string) (*EmailBillNotificationPreview, error) {
	task := &models.EmailBillSyncTask{Status: "succeeded", Scanned: 32, Imported: 12}
	if outcome == "failed" {
		task.Status, task.Failed, task.Imported = "failed", 2, 4
	}
	if outcome == "empty" || outcome == "test" {
		task.Imported = 0
	}
	d := emailBillNotificationBase(task, rootURL, locale)
	d.Time, d.Duration, d.Folders = time.Now().Format("2006-01-02 15:04 MST"), "1m 12s", []string{d.Copy["sampleFolder"]}
	if task.Imported > 0 {
		d.Uncategorized = 1
		d.Currencies = []emailBillNotificationCurrency{{Currency: "CNY", ExpenseText: "186.50", IncomeText: "0.00"}}
		d.Entries = []emailBillNotificationEntry{{Merchant: d.Copy["sampleMerchant"], Category: d.Copy["sampleCategory"], Amount: "−28.50 CNY"}, {Merchant: d.Copy["sampleMerchant2"], Category: d.Copy["uncategorized"], Amount: "−10.00 CNY"}}
		d.More = true
	}
	if outcome == "failed" {
		d.Error = d.Copy["sampleError"]
	}
	if outcome == "test" {
		d.Title, d.Description, d.Test = d.Copy["testTitle"], d.Copy["testDescription"], true
	}
	return renderEmailBillNotification(d)
}

func renderEmailBillNotification(data emailBillNotificationData) (*EmailBillNotificationPreview, error) {
	tmpl, err := templates.GetTemplate(templates.TEMPLATE_EMAIL_BILL_RESULT)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	if err = tmpl.Execute(&body, data); err != nil {
		return nil, err
	}
	return &EmailBillNotificationPreview{Subject: "[ezBookkeeping] " + data.Title, HTML: body.String()}, nil
}

func emailBillNotificationCopy(locale string) map[string]string {
	c := map[string]string{
		"success": "Completed", "attention": "Needs attention", "completed": "Your email bills are recorded", "failedTitle": "Email bookkeeping needs attention",
		"description": "New transactions from this run have been saved to your accounts.", "failedDescription": "Successful entries are saved. Open the email workspace to inspect the remaining errors.",
		"empty": "No new transactions. Previously processed emails are skipped automatically.", "imported": "New transactions", "scanned": "Emails scanned", "failed": "Failed emails", "uncategorized": "Uncategorized",
		"expense": "Expenses", "income": "Income and refunds", "entries": "Recent entries from this run", "merchant": "Merchant and category", "amount": "Amount", "more": "Open the workspace for all entries.", "runTime": "Run time",
		"fallback": "Low-confidence classifications were recorded as Uncategorized. You can edit their categories in your accounts.", "error": "What needs attention", "open": "Open email workspace", "folder": "Folders", "duration": "Duration",
		"footer": "This email summarizes one run. Transaction dates come from the original bills.", "testTitle": "Email notifications are connected", "testDescription": "This is a test email. No scan or bookkeeping changes were made.",
		"sampleFolder": "Bills", "sampleMerchant": "Sample restaurant", "sampleMerchant2": "Sample transport", "sampleCategory": "Dining", "sampleError": "The mailbox connection timed out. Completed entries are safe; run again to continue.",
	}
	if strings.HasPrefix(locale, "zh") {
		for k, v := range map[string]string{"success": "已完成", "attention": "需要处理", "completed": "本次邮件账单已入账", "failedTitle": "邮件记账尚未全部完成", "description": "本次新增账单已保存，可在账目中查看和修改。", "failedDescription": "已成功入账的记录已保存。请打开邮件工作台查看未完成项。", "empty": "本次没有新增账单，已处理的邮件会自动跳过。", "imported": "新增账目", "scanned": "扫描邮件", "failed": "处理失败", "uncategorized": "未分类", "expense": "支出", "income": "收入与退款", "entries": "本次新增账目摘要", "merchant": "商户与分类", "amount": "金额", "more": "完整账目请到邮件工作台查看。", "fallback": "分类置信度不足的账单已计入未分类，可在账目中修改分类。", "error": "需要处理的问题", "open": "打开邮件工作台", "folder": "扫描目录", "duration": "用时", "runTime": "运行时间", "footer": "本通知仅汇总本次运行，账目日期以原始账单为准。", "testTitle": "邮件通知连接成功", "testDescription": "这是一封测试邮件，没有执行扫描，也没有修改账目。", "sampleFolder": "账单", "sampleMerchant": "示例餐厅", "sampleMerchant2": "示例公交", "sampleCategory": "餐饮", "sampleError": "邮箱连接超时。已入账的记录已保存，再次运行即可继续。"} {
			c[k] = v
		}
	}
	if strings.Contains(locale, "Hant") || strings.Contains(locale, "TW") {
		for k, v := range map[string]string{"success": "已完成", "attention": "需要處理", "completed": "本次郵件帳單已入帳", "failedTitle": "郵件記帳尚未全部完成", "description": "本次新增帳單已儲存，可在帳目中查看和修改。", "failedDescription": "已成功入帳的記錄已儲存。請開啟郵件工作台查看未完成項。", "empty": "本次沒有新增帳單，已處理的郵件會自動略過。", "imported": "新增帳目", "scanned": "掃描郵件", "failed": "處理失敗", "uncategorized": "未分類", "expense": "支出", "income": "收入與退款", "entries": "本次新增帳目摘要", "merchant": "商戶與分類", "amount": "金額", "more": "完整帳目請到郵件工作台查看。", "fallback": "分類信心不足的帳單已計入未分類，可在帳目中修改分類。", "error": "需要處理的問題", "open": "開啟郵件工作台", "folder": "掃描目錄", "duration": "用時", "runTime": "執行時間", "footer": "本通知僅彙總本次執行，帳目日期以原始帳單為準。", "testTitle": "郵件通知連線成功", "testDescription": "這是一封測試郵件，沒有執行掃描，也沒有修改帳目。", "sampleFolder": "帳單", "sampleMerchant": "示例餐廳", "sampleMerchant2": "示例公車", "sampleCategory": "餐飲", "sampleError": "信箱連線逾時。已入帳的記錄已儲存，再次執行即可繼續。"} {
			c[k] = v
		}
	}
	return c
}
