package models

// EmailBillSyncTask persists the lifetime and progress of a background mailbox scan.
type EmailBillSyncTask struct {
	TaskId            int64  `xorm:"PK" json:"id,string"`
	Uid               int64  `xorm:"INDEX NOT NULL" json:"-"`
	TriggerType       string `xorm:"VARCHAR(24)" json:"trigger"`
	Status            string `xorm:"VARCHAR(24) INDEX NOT NULL" json:"status"`
	Stage             string `xorm:"VARCHAR(32)" json:"stage"`
	CurrentFolder     string `xorm:"TEXT" json:"currentFolder"`
	Scanned           int64  `json:"scanned"`
	Downloaded        int64  `json:"downloaded"`
	Processed         int64  `json:"processed"`
	Skipped           int64  `json:"skipped"`
	Failed            int64  `json:"failed"`
	FoldersJson       string `xorm:"TEXT" json:"-"`
	ErrorMessage      string `xorm:"TEXT" json:"errorMessage"`
	StartedUnixTime   int64  `json:"startedUnixTime"`
	UpdatedUnixTime   int64  `json:"updatedUnixTime"`
	CompletedUnixTime int64  `json:"completedUnixTime"`
}

// EmailBillScanMessage is a metadata-only index. It does not claim an import
// identity, so skipped and failed downloads remain safe to retry.
type EmailBillScanMessage struct {
	EntryId          int64  `xorm:"PK" json:"id,string"`
	Uid              int64  `xorm:"UNIQUE(UNQ_email_bill_scan_identity) INDEX NOT NULL" json:"-"`
	RemoteKey        string `xorm:"UNIQUE(UNQ_email_bill_scan_identity) VARCHAR(80) NOT NULL" json:"-"`
	Folder           string `xorm:"TEXT NOT NULL" json:"folder"`
	RemoteUid        uint32 `json:"remoteUid"`
	UidValidity      uint32 `json:"-"`
	RemoteMessageId  string `xorm:"VARCHAR(998)" json:"remoteMessageId"`
	Sender           string `xorm:"VARCHAR(254)" json:"sender"`
	Subject          string `xorm:"TEXT" json:"subject"`
	ReceivedUnixTime int64  `xorm:"INDEX" json:"receivedUnixTime"`
	Authenticated    bool   `json:"authenticated"`
	Status           string `xorm:"VARCHAR(32) INDEX" json:"status"`
	Reason           string `xorm:"TEXT" json:"reason"`
	MessageId        int64  `xorm:"INDEX" json:"messageId,string"`
	ImportRunId      int64  `json:"importRunId,string"`
	TaskId           int64  `xorm:"INDEX" json:"taskId,string"`
	UpdatedUnixTime  int64  `json:"updatedUnixTime"`
}
