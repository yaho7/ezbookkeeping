package cron

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/duplicatechecker"
	"github.com/mayswind/ezbookkeeping/pkg/errs"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
)

func TestEmailBillJobReleasesRunningInfoAfterSuccessOrFailure(t *testing.T) {
	previous := duplicatechecker.Container
	duplicatechecker.Container = &duplicatechecker.DuplicateCheckerContainer{}
	t.Cleanup(func() { duplicatechecker.Container = previous })
	checker, err := duplicatechecker.NewInMemoryDuplicateChecker(&settings.Config{
		DuplicateSubmissionsIntervalDuration: time.Minute,
	})
	require.NoError(t, err)
	duplicatechecker.SetDuplicateChecker(checker)

	job := NewEmailBillImportJob("0 3 * * *", "Asia/Shanghai")
	want := errors.New("IMAP fetch failed")
	runs := 0
	job.Run = func(c *core.CronContext) error {
		runs++
		// A second request must not enter the importer while this run is active.
		require.ErrorIs(t, job.run(), errs.ErrSystemIsBusy)
		if runs == 1 {
			return want
		}
		return nil
	}

	require.ErrorIs(t, job.run(), want)
	require.NoError(t, job.run())
	require.NoError(t, job.run())
	require.Equal(t, 3, runs)
}
