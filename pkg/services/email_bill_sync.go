package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/datastore"
	"github.com/mayswind/ezbookkeeping/pkg/emailbill"
	"github.com/mayswind/ezbookkeeping/pkg/errs"
	"github.com/mayswind/ezbookkeeping/pkg/log"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
	"github.com/mayswind/ezbookkeeping/pkg/uuid"
)

type EmailBillFolderProgress struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Total   uint32 `json:"total"`
	Scanned int64  `json:"scanned"`
}

type EmailBillSyncInfo struct {
	*models.EmailBillSyncTask
	Folders []*EmailBillFolderProgress `json:"folders"`
}

// EmailBillSyncService owns background work independently of HTTP and cron locks.
// Each worker writes progress serially; handlers read detached database snapshots.
type EmailBillSyncService struct {
	ServiceUsingDB
	ServiceUsingUuid
	mu     sync.Mutex
	active map[int64]int64
	// SQLite uses shared-cache connections: a list reader can otherwise make a
	// concurrent scan write fail with SQLITE_LOCKED instead of waiting. Guard
	// all task/index access, including readers, for the SQL operation only.
	// Lock order is mu then metadataMu; never acquire mu while holding metadataMu.
	metadataMu sync.Mutex
}

var EmailBillSync = &EmailBillSyncService{
	ServiceUsingDB:   ServiceUsingDB{container: datastore.Container},
	ServiceUsingUuid: ServiceUsingUuid{container: uuid.Container},
	active:           make(map[int64]int64),
}

func syncInfo(task *models.EmailBillSyncTask) *EmailBillSyncInfo {
	info := &EmailBillSyncInfo{EmailBillSyncTask: task, Folders: make([]*EmailBillFolderProgress, 0)}
	_ = json.Unmarshal([]byte(task.FoldersJson), &info.Folders)
	return info
}

// StartConfigured is shared by scheduled and manual triggers.
func (s *EmailBillSyncService) StartConfigured(c core.Context, trigger string) (*EmailBillSyncInfo, error) {
	config := settings.Container.GetCurrentConfig()
	if config == nil || config.EmailBillConfig == nil || !config.EmailBillConfig.Enabled {
		return nil, errs.ErrCronJobNotExistsOrNotEnabled
	}
	user, err := Users.GetUserByUsername(c, config.EmailBillConfig.TargetUser)
	if err != nil {
		return nil, err
	}
	return s.Start(c, user.Uid, config.EmailBillConfig, trigger)
}

// Start persists acceptance before launching work. Repeated clicks return the active task.
func (s *EmailBillSyncService) Start(c core.Context, uid int64, config *settings.EmailBillConfig, trigger string) (*EmailBillSyncInfo, error) {
	if config == nil || !config.Enabled {
		return nil, errs.ErrCronJobNotExistsOrNotEnabled
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()
	if id := s.active[uid]; id != 0 {
		task := &models.EmailBillSyncTask{}
		has, err := s.UserDataDB(uid).NewSession(c).Where("uid=? AND task_id=?", uid, id).Get(task)
		if err != nil {
			return nil, err
		}
		if !has {
			return nil, errs.ErrOperationFailed
		}
		return syncInfo(task), nil
	}
	if err := s.interruptOrphans(c, uid); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	task := &models.EmailBillSyncTask{TaskId: s.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), Uid: uid,
		TriggerType: trigger, Status: "queued", Stage: "queued", StartedUnixTime: now, UpdatedUnixTime: now, FoldersJson: "[]"}
	if _, err := s.UserDataDB(uid).NewSession(c).Insert(task); err != nil {
		return nil, err
	}
	s.active[uid] = task.TaskId
	// No request context or mutable configuration is captured by the goroutine.
	configCopy, taskCopy := *config, *task
	configCopy.Folders = append([]string(nil), config.Folders...)
	go s.run(&taskCopy, &configCopy)
	return syncInfo(task), nil
}

// interruptOrphans requires both mu and metadataMu to be held by the caller.
func (s *EmailBillSyncService) interruptOrphans(c core.Context, uid int64) error {
	_, err := s.UserDataDB(uid).NewSession(c).Where("uid=?", uid).In("status", "queued", "running").
		Cols("status", "stage", "error_message", "updated_unix_time", "completed_unix_time").Update(&models.EmailBillSyncTask{
		Status: "interrupted", Stage: "finished", ErrorMessage: "Server restarted before the task completed",
		UpdatedUnixTime: time.Now().Unix(), CompletedUnixTime: time.Now().Unix(),
	})
	if err != nil {
		return err
	}
	_, err = s.UserDataDB(uid).NewSession(c).Where("uid=?", uid).In("status", "ready", "downloaded", "processing").
		Cols("status", "reason", "updated_unix_time").Update(&models.EmailBillScanMessage{
		Status: "not_processed", Reason: "Server restarted before the task completed", UpdatedUnixTime: time.Now().Unix(),
	})
	return err
}

func (s *EmailBillSyncService) Latest(c core.Context, uid int64) (*EmailBillSyncInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()
	if s.active[uid] == 0 {
		if err := s.interruptOrphans(c, uid); err != nil {
			return nil, err
		}
	}
	task := &models.EmailBillSyncTask{}
	has, err := s.UserDataDB(uid).NewSession(c).Where("uid=?", uid).OrderBy("task_id desc").Get(task)
	if err != nil || !has {
		return nil, err
	}
	return syncInfo(task), nil
}

func (s *EmailBillSyncService) save(c core.Context, task *models.EmailBillSyncTask, folders []*EmailBillFolderProgress) error {
	data, err := json.Marshal(folders)
	if err != nil {
		return err
	}
	task.FoldersJson, task.UpdatedUnixTime = string(data), time.Now().Unix()
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()
	_, err = s.UserDataDB(task.Uid).NewSession(c).ID(task.TaskId).AllCols().Update(task)
	return err
}

func (s *EmailBillSyncService) finishPendingMessages(c core.Context, uid, taskID int64) error {
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()
	_, err := s.UserDataDB(uid).NewSession(c).Where("uid=? AND task_id=?", uid, taskID).
		In("status", "ready", "downloaded", "processing").Cols("status", "reason", "updated_unix_time").Update(&models.EmailBillScanMessage{
		Status: "not_processed", Reason: "Task ended before this message was processed; check the task outcome and run again", UpdatedUnixTime: time.Now().Unix(),
	})
	return err
}

func (s *EmailBillSyncService) run(task *models.EmailBillSyncTask, config *settings.EmailBillConfig) {
	c := core.NewCronJobContext("EmailBillBackground", 0)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	c.Context = ctx
	defer cancel()
	folders := make([]*EmailBillFolderProgress, 0)
	var runErr error
	defer func() {
		if recovered := recover(); recovered != nil {
			runErr = fmt.Errorf("email task panic: %v", recovered)
		}
		task.Status, task.Stage, task.CompletedUnixTime = "succeeded", "finished", time.Now().Unix()
		if task.Failed > 0 {
			task.Status = "partial_success"
		}
		if runErr != nil {
			task.Status, task.ErrorMessage = "failed", sanitizeEmailTaskError(runErr.Error(), config)
		}
		for _, folder := range folders {
			if folder.Status == "scanning" || folder.Status == "opening" {
				folder.Status = "interrupted"
			}
			if folder.Status == "waiting" {
				folder.Status = "not_scanned"
			}
		}
		// Persist terminal state even after the worker deadline has expired.
		finish := core.NewCronJobContext("EmailBillBackgroundFinish", 0)
		if err := s.finishPendingMessages(finish, task.Uid, task.TaskId); err != nil && runErr == nil {
			task.Status, task.ErrorMessage = "failed", sanitizeEmailTaskError(err.Error(), config)
		}
		if err := s.save(finish, task, folders); err != nil {
			log.Errorf(finish, "[email_bill_sync] cannot save task %d outcome: %s", task.TaskId, sanitizeEmailTaskError(err.Error(), config))
		}
		log.Infof(finish, "[email_bill_sync] task=%d status=%s scanned=%d processed=%d failed=%d error=%s", task.TaskId, task.Status, task.Scanned, task.Processed, task.Failed, task.ErrorMessage)
		s.mu.Lock()
		delete(s.active, task.Uid)
		s.mu.Unlock()
	}()
	task.Status, task.Stage = "running", "connecting"
	if runErr = s.save(c, task, folders); runErr != nil {
		return
	}
	log.Infof(c, "[email_bill_sync] task=%d started trigger=%s", task.TaskId, task.TriggerType)
	lastSave := time.Now()
	runErr = EmailBillImporter.importWithObserver(c, task.Uid, config, func(event emailbill.ScanEvent) error {
		folder := findEmailBillFolder(&folders, event.Folder)
		if event.Kind == "folder" {
			folder.Status = event.Status
			if event.Status == "scanning" {
				folder.Total = event.Total
			}
			if event.Status != "waiting" {
				task.CurrentFolder, task.Stage = event.Folder, "scanning"
			}
		} else {
			if event.Scanned {
				task.Scanned++
				folder.Scanned++
			}
			if event.Downloaded {
				task.Downloaded++
			}
			if event.Processed {
				task.Processed++
			}
			switch event.Status {
			case "duplicate", "not_matched", "rejected", "oversized":
				task.Skipped++
			case "failed", "partial_success":
				task.Failed++
			}
			if event.Status == "processing" {
				task.Stage = "processing"
			} else {
				task.Stage = "scanning"
			}
			if err := s.record(c, task, config, event); err != nil {
				return err
			}
		}
		if event.Kind == "folder" || event.Status == "processing" || time.Since(lastSave) >= time.Second {
			lastSave = time.Now()
			return s.save(c, task, folders)
		}
		return nil
	})
}

func findEmailBillFolder(folders *[]*EmailBillFolderProgress, name string) *EmailBillFolderProgress {
	for _, folder := range *folders {
		if folder.Name == name {
			return folder
		}
	}
	folder := &EmailBillFolderProgress{Name: name, Status: "waiting"}
	*folders = append(*folders, folder)
	return folder
}

func sanitizeEmailTaskError(value string, config *settings.EmailBillConfig) string {
	if config.MailPassword != "" {
		value = strings.ReplaceAll(value, config.MailPassword, "[redacted]")
	}
	return trimRunes(value, 2000)
}

func (s *EmailBillSyncService) record(c core.Context, task *models.EmailBillSyncTask, config *settings.EmailBillConfig, event emailbill.ScanEvent) error {
	message := event.Message
	if message == nil || message.UID == 0 {
		return nil
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d", config.IMAPServer, config.MailUser, event.Folder, message.UIDValidity, message.UID)))
	key := fmt.Sprintf("%x", digest)
	entry := &models.EmailBillScanMessage{}
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()
	db := s.UserDataDB(task.Uid)
	has, err := db.NewSession(c).Where("uid=? AND remote_key=?", task.Uid, key).Get(entry)
	if err != nil {
		return err
	}
	if !has {
		entry.EntryId, entry.Uid, entry.RemoteKey = s.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), task.Uid, key
	}
	entry.Folder, entry.RemoteUid, entry.UidValidity = event.Folder, message.UID, message.UIDValidity
	if message.Sender != "" {
		entry.Sender = message.Sender
	}
	if message.Subject != "" {
		entry.Subject = message.Subject
	}
	if message.MessageID != "" {
		entry.RemoteMessageId = message.MessageID
	}
	if !message.ReceivedAt.IsZero() {
		entry.ReceivedUnixTime = message.ReceivedAt.Unix()
	}
	entry.Authenticated, entry.Status, entry.Reason = message.Authenticated, event.Status, sanitizeEmailTaskError(event.Reason, config)
	entry.TaskId, entry.UpdatedUnixTime = task.TaskId, time.Now().Unix()
	if event.MessageID > 0 {
		entry.MessageId, entry.ImportRunId = event.MessageID, event.RunID
	}
	if entry.MessageId == 0 && event.Status == "duplicate" && message.MessageID != "" {
		fingerprint, version := emailbill.MessageFingerprint(emailbill.MessageIdentityInput{MailboxID: task.Uid, MessageID: message.MessageID, Sender: message.Sender, Subject: message.Subject, ReceivedAt: message.ReceivedAt})
		inbound := &models.EmailBillInboundMessage{}
		found, err := db.NewSession(c).Where("uid=? AND mailbox_id=? AND message_fingerprint=? AND fingerprint_version=?", task.Uid, task.Uid, fingerprint, version).Get(inbound)
		if err != nil {
			return err
		}
		if found {
			entry.MessageId = inbound.MessageId
		}
	}
	if has {
		_, err = db.NewSession(c).ID(entry.EntryId).AllCols().Update(entry)
	} else {
		_, err = db.NewSession(c).Insert(entry)
	}
	return err
}
