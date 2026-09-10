package api

import (
	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/errs"
	"github.com/mayswind/ezbookkeeping/pkg/services"
)

func (a *EmailBillAutomationApi) MailboxListHandler(c *core.WebContext) (any, *errs.Error) {
	request := struct {
		Folder string `form:"folder" binding:"max=1000"`
		Status string `form:"status" binding:"max=32"`
		Search string `form:"search" binding:"max=200"`
		Page   int    `form:"page" binding:"min=0,max=1000000"`
	}{}
	if err := c.ShouldBindQuery(&request); err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	page, err := services.EmailBillSync.ListMessages(c, c.GetCurrentUid(), request.Folder, request.Status, request.Search, request.Page)
	if err != nil {
		return nil, errs.Or(err, errs.ErrOperationFailed)
	}
	return page, nil
}

func (a *EmailBillAutomationApi) MailboxDetailHandler(c *core.WebContext) (any, *errs.Error) {
	request := struct {
		ID int64 `form:"id" binding:"required,min=1"`
	}{}
	if err := c.ShouldBindQuery(&request); err != nil {
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}
	message, err := services.EmailBillSync.MessageDetail(c, c.GetCurrentUid(), request.ID)
	if err != nil {
		return nil, errs.Or(err, errs.ErrOperationFailed)
	}
	return message, nil
}
