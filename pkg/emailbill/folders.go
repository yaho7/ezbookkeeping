package emailbill

import (
	"context"
	"fmt"

	"github.com/emersion/go-imap"
)

// ListFolders performs only login and LIST. It never issues SELECT, SEARCH or FETCH.
func (m *IMAPMailbox) ListFolders(ctx context.Context) ([]string, error) {
	imapClient, err := m.openConnection(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = imapClient.Logout() }()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			_ = imapClient.Terminate()
		case <-finished:
		}
	}()
	return listMailboxFolders(imapClient)
}

func selectedMailboxFolders(available, selected []string) ([]string, error) {
	if len(selected) == 0 {
		return nil, fmt.Errorf("Select at least one email folder")
	}
	wanted := make(map[string]bool, len(selected))
	for _, folder := range selected {
		wanted[imap.CanonicalMailboxName(folder)] = false
	}
	result := make([]string, 0, len(selected))
	for _, folder := range available {
		canonical := imap.CanonicalMailboxName(folder)
		if _, ok := wanted[canonical]; ok && !wanted[canonical] {
			result = append(result, folder)
			wanted[canonical] = true
		}
	}
	for folder, found := range wanted {
		if !found {
			return nil, fmt.Errorf("selected IMAP folder %q is missing or cannot be opened; refresh the folder list", folder)
		}
	}
	return result, nil
}
