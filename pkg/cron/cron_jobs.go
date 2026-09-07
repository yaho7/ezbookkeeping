package cron

import (
	"fmt"
	"time"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/services"
)

// RemoveExpiredTokensJob represents the cron job which periodically remove expired user tokens from the database
var RemoveExpiredTokensJob = &CronJob{
	Name:        "RemoveExpiredTokens",
	Description: "Periodically remove expired user tokens from the database.",
	Period: CronJobFixedHourPeriod{
		Hour: 0,
	},
	Run: func(c *core.CronContext) error {
		return services.Tokens.DeleteAllExpiredTokens(c)
	},
}

// CreateScheduledTransactionJob represents the cron job which periodically create transaction by scheduled transaction template
var CreateScheduledTransactionJob = &CronJob{
	Name:        "CreateScheduledTransaction",
	Description: "Periodically create transaction by scheduled transaction template.",
	Period: CronJobEvery15MinutesPeriod{
		Second: 0,
	},
	Run: func(c *core.CronContext) error {
		return services.Transactions.CreateScheduledTransactions(c, time.Now().Unix(), c.GetInterval())
	},
}

// NewEmailBillImportJob returns the built-in email bill importer cron job.
func NewEmailBillImportJob(cronExpression string, timezone string) *CronJob {
	return &CronJob{
		Name:        "ImportEmailBills",
		Description: "Periodically import supported bank emails into native transactions.",
		Period: CronJobExpressionPeriod{
			Expression: fmt.Sprintf("CRON_TZ=%s %s", timezone, cronExpression),
		},
		Run: func(c *core.CronContext) error {
			return services.EmailBillImporter.Import(c)
		},
	}
}
