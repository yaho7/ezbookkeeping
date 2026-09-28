package services

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"xorm.io/xorm"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/datastore"
	"github.com/mayswind/ezbookkeeping/pkg/errs"
	"github.com/mayswind/ezbookkeeping/pkg/locales"
	"github.com/mayswind/ezbookkeeping/pkg/log"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/uuid"
	"github.com/mayswind/ezbookkeeping/pkg/validators"
)

type emailBillDefaultAccountResolver interface {
	Resolve(core.Context, int64, string) (int64, error)
}

// EmailBillDefaultAccountService prepares accounts without requiring routing setup.
// Account creation and its unique per-user/currency mapping commit together.
type EmailBillDefaultAccountService struct {
	ServiceUsingDB
	ServiceUsingUuid
	mu sync.Mutex
}

var EmailBillDefaultAccounts = &EmailBillDefaultAccountService{
	ServiceUsingDB:   ServiceUsingDB{container: datastore.Container},
	ServiceUsingUuid: ServiceUsingUuid{container: uuid.Container},
}

func (s *EmailBillDefaultAccountService) Resolve(c core.Context, uid int64, currency string) (int64, error) {
	if uid <= 0 {
		return 0, errs.ErrUserIdInvalid
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if !validators.AllCurrencyNames[currency] {
		return 0, errs.ErrAccountCurrencyInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	user := &models.User{}
	has, err := s.UserDB().NewSession(c).ID(uid).Where("deleted=?", false).Get(user)
	if err != nil {
		return 0, err
	}
	if !has {
		return 0, errs.ErrUserNotFound
	}

	accountID, defaultUsable := int64(0), false
	err = s.UserDataDB(uid).DoTransaction(c, func(sess *xorm.Session) error {
		var accounts []*models.Account
		if err := sess.Where("uid=? AND deleted=?", uid, false).Find(&accounts); err != nil {
			return err
		}
		defaultAccount := usableEmailBillAccount(accounts, user.DefaultAccountId, uid, "")
		defaultUsable = defaultAccount != nil
		if defaultAccount != nil && defaultAccount.Currency == currency {
			accountID = defaultAccount.AccountId
			return nil
		}
		mapping := &models.EmailBillDefaultAccount{}
		hasMapping, err := sess.Where("uid=? AND currency=?", uid, currency).Get(mapping)
		if err != nil {
			return err
		}
		if hasMapping && usableEmailBillAccount(accounts, mapping.AccountId, uid, currency) != nil {
			accountID = mapping.AccountId
			return nil
		}
		name := locales.GetLocaleTextItems(user.Language).GlobalTextItems.DefaultEmailBillAccountName
		if name == "" {
			name = "Default bookkeeping account"
		}
		account := &models.Account{
			Uid: uid, Name: fmt.Sprintf("%s (%s)", name, currency), Currency: currency,
			Category: models.ACCOUNT_CATEGORY_VIRTUAL, Type: models.ACCOUNT_TYPE_SINGLE_ACCOUNT,
			Icon: 1, IconType: core.ICON_TYPE_SYSTEM, Color: "607D8B",
		}
		for _, existing := range accounts {
			if existing.ParentAccountId == 0 && existing.Category == account.Category && existing.DisplayOrder >= account.DisplayOrder {
				account.DisplayOrder = existing.DisplayOrder + 1
			}
		}
		if account.DisplayOrder == 0 {
			account.DisplayOrder = 1
		}
		account.AccountId = s.GenerateUuid(uuid.UUID_TYPE_ACCOUNT)
		if account.AccountId <= 0 {
			return errs.ErrSystemIsBusy
		}
		account.CreatedUnixTime, account.UpdatedUnixTime = time.Now().Unix(), time.Now().Unix()
		// A zero-balance native account needs no opening-balance transaction.
		if _, err := sess.Insert(account); err != nil {
			return err
		}
		accountID = account.AccountId
		if hasMapping {
			_, err = sess.ID(mapping.DefaultAccountId).Cols("account_id").Update(&models.EmailBillDefaultAccount{AccountId: accountID})
			return err
		}
		mapping = &models.EmailBillDefaultAccount{
			DefaultAccountId: s.GenerateUuid(uuid.UUID_TYPE_EMAIL_BILL), Uid: uid,
			Currency: currency, AccountId: accountID, CreatedUnixTime: time.Now().Unix(),
		}
		if mapping.DefaultAccountId <= 0 {
			return errs.ErrSystemIsBusy
		}
		_, err = sess.Insert(mapping)
		return err
	})
	if err != nil {
		return 0, err
	}
	if !defaultUsable {
		// Never overwrite a concurrent profile change, or other profile fields.
		if err := Users.SetDefaultAccountIfUnchanged(c, uid, user.DefaultAccountId, accountID); err != nil {
			log.Warnf(c, "[email_bill_defaults.Resolve] cannot update default account uid=%d: %s", uid, err.Error())
		}
	}
	return accountID, nil
}

// An empty currency checks validity of the profile default, without replacing
// a valid default just because an email uses a different currency.
func usableEmailBillAccount(accounts []*models.Account, accountID, uid int64, currency string) *models.Account {
	var selected *models.Account
	for _, account := range accounts {
		if account.AccountId == accountID && account.Uid == uid && !account.Deleted && !account.Hidden &&
			account.Type == models.ACCOUNT_TYPE_SINGLE_ACCOUNT && (currency == "" || account.Currency == currency) {
			selected = account
			break
		}
	}
	if selected == nil || selected.ParentAccountId == 0 {
		return selected
	}
	for _, parent := range accounts {
		if parent.AccountId == selected.ParentAccountId && parent.Uid == uid && !parent.Deleted && !parent.Hidden && parent.Type == models.ACCOUNT_TYPE_MULTI_SUB_ACCOUNTS {
			return selected
		}
	}
	return nil
}
