package cron

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/stretchr/testify/assert"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/duplicatechecker"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
)

func TestCronJobSchedulerContainerRegisterIntervalJob(t *testing.T) {
	var err error

	container := &CronJobSchedulerContainer{
		allJobsMap:       make(map[string]*CronJob),
		allGocronJobsMap: make(map[string]gocron.Job),
	}

	container.scheduler, err = gocron.NewScheduler(
		gocron.WithLocation(time.Local),
		gocron.WithLogger(NewGocronLoggerAdapter()),
	)
	assert.Nil(t, err)

	actualValue := false
	job := &CronJob{
		Name:        "TestRegisterIntervalJob",
		Description: "The test cron job",
		Period: CronJobIntervalPeriod{
			Interval: 1 * time.Second,
		},
		Run: func(c *core.CronContext) error {
			actualValue = true
			return nil
		},
	}

	container.registerIntervalJob(core.NewNullContext(), job)
	container.scheduler.Start()

	assert.Equal(t, 1, len(container.GetAllJobs()))
	assert.Equal(t, job, container.GetAllJobs()[0])

	time.Sleep(2 * time.Second)
	assert.True(t, actualValue)

	err = container.scheduler.Shutdown()
	assert.Nil(t, err)
}

func TestCronJobSchedulerContainerSyncRunJobNow(t *testing.T) {
	var err error

	container := &CronJobSchedulerContainer{
		allJobsMap:       make(map[string]*CronJob),
		allGocronJobsMap: make(map[string]gocron.Job),
	}

	container.scheduler, err = gocron.NewScheduler(
		gocron.WithLocation(time.Local),
		gocron.WithLogger(NewGocronLoggerAdapter()),
	)
	assert.Nil(t, err)

	actualValue := false
	job := &CronJob{
		Name:        "TestSyncRunJob",
		Description: "The test cron job",
		Period: CronJobIntervalPeriod{
			Interval: 24 * time.Hour,
		},
		Run: func(c *core.CronContext) error {
			actualValue = true
			return nil
		},
	}

	container.registerIntervalJob(core.NewNullContext(), job)

	err = container.SyncRunJobNow("TestSyncRunJob")
	assert.Nil(t, err)
	assert.True(t, actualValue)
}

func TestCronJobSchedulerContainerRegistersEmailBillImporterWhenEnabled(t *testing.T) {
	container := &CronJobSchedulerContainer{
		allJobsMap:       make(map[string]*CronJob),
		allGocronJobsMap: make(map[string]gocron.Job),
	}
	scheduler, err := gocron.NewScheduler()
	assert.Nil(t, err)
	container.scheduler = scheduler

	container.registerAllJobs(core.NewNullContext(), &settings.Config{
		EmailBillConfig: &settings.EmailBillConfig{
			Enabled:          true,
			IntervalDuration: time.Hour,
		},
	})

	job, exists := container.allJobsMap["ImportEmailBills"]
	assert.True(t, exists)
	if exists {
		assert.Equal(t, time.Hour, job.Period.GetInterval())
	}
	assert.Nil(t, scheduler.Shutdown())
}

func TestCronJobSchedulerContainerRepeatRun(t *testing.T) {
	var err error

	checker, _ := duplicatechecker.NewInMemoryDuplicateChecker(&settings.Config{
		DuplicateSubmissionsIntervalDuration:            60 * time.Second,
		InMemoryDuplicateCheckerCleanupIntervalDuration: 60 * time.Second,
	})

	duplicatechecker.SetDuplicateChecker(checker)

	container := &CronJobSchedulerContainer{
		allJobsMap:       make(map[string]*CronJob),
		allGocronJobsMap: make(map[string]gocron.Job),
	}

	container.scheduler, err = gocron.NewScheduler(
		gocron.WithLocation(time.Local),
		gocron.WithLogger(NewGocronLoggerAdapter()),
	)
	assert.Nil(t, err)

	var runCount atomic.Uint32
	runTime := time.Now().Add(time.Second)
	job := &CronJob{
		Name:        "TestRepeatRunJob",
		Description: "The test cron job",
		Period: CronJobFixedTimePeriod{
			Time: runTime,
		},
		Run: func(c *core.CronContext) error {
			runCount.Add(1)
			return nil
		},
	}

	container.registerIntervalJob(core.NewNullContext(), job)
	container.registerIntervalJob(core.NewNullContext(), job)
	container.registerIntervalJob(core.NewNullContext(), job)
	container.registerIntervalJob(core.NewNullContext(), job)
	container.registerIntervalJob(core.NewNullContext(), job)
	container.scheduler.Start()

	time.Sleep(10 * time.Second)

	assert.Nil(t, err)
	assert.Equal(t, uint32(1), runCount.Load())

	err = container.scheduler.Shutdown()
	assert.Nil(t, err)
}
