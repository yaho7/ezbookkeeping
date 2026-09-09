package cron

import (
	"fmt"
	"time"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/duplicatechecker"
	"github.com/mayswind/ezbookkeeping/pkg/errs"
	"github.com/mayswind/ezbookkeeping/pkg/log"
	"github.com/mayswind/ezbookkeeping/pkg/utils"
)

// CronJob represents the cron job instance
type CronJob struct {
	Name        string
	Description string
	Period      CronJobPeriod
	Run         func(*core.CronContext) error
	// ReleaseRunningInfo makes the duplicate marker an execution lock rather
	// than suppressing further runs until the next scheduled occurrence.
	ReleaseRunningInfo bool
}

func (j *CronJob) doRun() {
	_ = j.run()
}

// run preserves the job outcome for synchronous callers while doRun remains the
// scheduled task callback.
func (j *CronJob) run() error {
	start := time.Now()
	c := core.NewCronJobContext(j.Name, j.Period.GetInterval())

	if duplicatechecker.Container.IsEnabled() {
		localAddr, err := utils.GetLocalIPAddressesString()

		if err != nil {
			log.Warnf(c, "[cron_job.doRun] job \"%s\" cannot get local ipv4 address, because %s", j.Name, err.Error())
			return err
		}

		currentInfo := fmt.Sprintf("ip: %s, startTime: %d", localAddr, time.Now().Unix())
		interval := j.Period.GetInterval()
		if j.ReleaseRunningInfo {
			// The in-memory lock must not expire while an import is still running.
			interval = -1
		}
		found, runningInfo := duplicatechecker.Container.GetOrSetCronJobRunningInfo(j.Name, currentInfo, interval)

		if found {
			log.Warnf(c, "[cron_job.doRun] job \"%s\" is already running (%s)", j.Name, runningInfo)
			return errs.ErrSystemIsBusy
		}
		if j.ReleaseRunningInfo {
			defer duplicatechecker.Container.RemoveCronJobRunningInfo(j.Name)
		}
	}

	err := j.Run(c)

	now := time.Now()

	if err != nil {
		log.Errorf(c, "[cron_job.doRun] failed to run job \"%s\", because %s", j.Name, err.Error())
		return err
	}

	cost := now.Sub(start).Nanoseconds() / 1e6
	log.Infof(c, "[cron_job.doRun] run job \"%s\" successfully, cost %dms", j.Name, cost)
	return nil
}
