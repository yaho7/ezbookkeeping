package emailbill

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/client"
	"github.com/stretchr/testify/require"
)

func TestIMAPSlowClassificationDoesNotCloseConnectionBeforeNextBatch(t *testing.T) {
	connection, server := net.Pipe()
	defer connection.Close()
	defer server.Close()
	go func() {
		_, _ = io.WriteString(server, "* PREAUTH [CAPABILITY IMAP4rev1] Ready\r\n")
		scanner := bufio.NewScanner(server)
		for scanner.Scan() {
			tag := strings.SplitN(scanner.Text(), " ", 2)[0]
			if _, err := io.WriteString(server, tag+" OK Completed\r\n"); err != nil {
				return
			}
		}
	}()
	imapClient, err := client.New(connection)
	require.NoError(t, err)
	defer imapClient.Terminate()
	imapClient.Timeout = 100 * time.Millisecond
	require.NoError(t, imapClient.Noop())
	mailbox := &IMAPMailbox{connection: connection, handler: func(Message) error {
		time.Sleep(2 * imapClient.Timeout)
		return nil
	}}
	messages := []Message{{Text: "downloaded bill", Headers: map[string]string{"subject": "Bill"}}}
	require.NoError(t, mailbox.processDownloadedMessages(context.Background(), messages))
	require.NoError(t, imapClient.Noop(), "the next batch must still be able to use the connection")
	require.Empty(t, messages[0].Text)
	require.Nil(t, messages[0].Headers)
}
