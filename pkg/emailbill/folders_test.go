package emailbill

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectedMailboxFoldersExcludeInboxAndUnselectedChildren(t *testing.T) {
	actual, err := selectedMailboxFolders([]string{"其他文件夹/账单", "其他文件夹/账单/历史", "其他文件夹/邮件归档", "INBOX"}, []string{"其他文件夹/账单", "其他文件夹/账单"})
	require.NoError(t, err)
	require.Equal(t, []string{"其他文件夹/账单"}, actual)
}

func TestSelectedMailboxFoldersNeverFallBackToAll(t *testing.T) {
	for _, selected := range [][]string{nil, {"deleted-folder"}, {"INBOX", "deleted-folder"}} {
		folders, err := selectedMailboxFolders([]string{"INBOX", "Archive"}, selected)
		require.Error(t, err)
		require.Nil(t, folders)
	}
	folders, err := selectedMailboxFolders([]string{"INBOX", "Archive"}, []string{"inbox"})
	require.NoError(t, err)
	require.Equal(t, []string{"INBOX"}, folders)
}
