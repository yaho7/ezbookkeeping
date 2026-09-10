package emailbill

// ScanEvent exposes scan progress without exposing credentials or raw headers.
// Message bodies are handled by the existing retention-controlled pipeline.
type ScanEvent struct {
	Kind       string
	Folder     string
	Total      uint32
	Status     string
	Reason     string
	Message    *Message
	Scanned    bool
	Downloaded bool
	Processed  bool
	MessageID  int64
	RunID      int64
}

type ScanObserver func(ScanEvent) error

func (m *IMAPMailbox) SetScanObserver(observer ScanObserver) { m.observer = observer }

// SetMessageHandler processes downloaded messages before the next batch is read.
func (m *IMAPMailbox) SetMessageHandler(handler func(Message) error) { m.handler = handler }

func (m *IMAPMailbox) report(event ScanEvent) error {
	if m.observer == nil {
		return nil
	}
	if event.Folder == "" {
		event.Folder = m.folder
	}
	return m.observer(event)
}

func (m *IMAPMailbox) located(message Message, uid uint32) Message {
	message.Folder, message.UID, message.UIDValidity = m.folder, uid, m.uidValidity
	return message
}
