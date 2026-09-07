package emailbill

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLocation(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	return location
}

func TestCMBCreditParserMatchesSupportedMail(t *testing.T) {
	parser := NewCMBCreditParser()

	assert.True(t, parser.Matches("招商银行 <ccsvc@message.cmbchina.com>", "招商银行每日信用管家"))
	assert.False(t, parser.Matches("attacker@example.com", "招商银行每日信用管家"))
	assert.False(t, parser.Matches("ccsvc@message.cmbchina.com", "无关邮件"))
}

func TestCMBCreditParserParsesExpenseAndRefund(t *testing.T) {
	parser := NewCMBCreditParser()
	receivedAt := time.Date(2026, 9, 7, 9, 0, 0, 0, testLocation(t))
	body := "2026/09/06 11:03:15 CNY 1.53 尾号5460 消费 麦当劳 (每日邮件) " +
		"12:04:05 CNY -3.27 尾号5460 退货 麦当劳 (每日邮件)"

	transactions, err := parser.Parse(body, receivedAt)

	require.NoError(t, err)
	require.Len(t, transactions, 2)
	assert.Equal(t, int64(-153), transactions[0].AmountMinor)
	assert.Equal(t, "麦当劳", transactions[0].Merchant)
	assert.Equal(t, 11, transactions[0].OccurredAt.Hour())
	assert.Equal(t, int64(327), transactions[1].AmountMinor)
}

func TestCMBCreditParserUsesNextTimestampAndEndOfTextAsBoundaries(t *testing.T) {
	parser := NewCMBCreditParser()
	receivedAt := time.Date(2026, 9, 7, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	body := "2026/09/07\n08:30:00 CNY 12.34 尾号1234消费 早餐店\n09:45:10 CNY -5.67 尾号1234退货 商场"

	transactions, err := parser.Parse(body, receivedAt)

	require.NoError(t, err)
	require.Len(t, transactions, 2)
	assert.Equal(t, "早餐店", transactions[0].Merchant)
	assert.Equal(t, int64(-1234), transactions[0].AmountMinor)
	assert.Equal(t, "商场", transactions[1].Merchant)
	assert.Equal(t, int64(567), transactions[1].AmountMinor)
}

func TestCMBDebitParserParsesExpenseIncomeAndFundsAggregation(t *testing.T) {
	parser := NewCMBDebitParser()
	receivedAt := time.Date(2026, 9, 7, 20, 0, 0, 0, testLocation(t))
	body := "您尾号1234的账户于09月07日08:30在早餐店消费人民币12.34元。" +
		"您的账户于09月07日09:45工资入账人民币1234.56元。" +
		"资金归集执行成功，已从其他账户向您尾号1234账户转账人民币500.00元，截至09月07日18:35。"

	transactions, err := parser.Parse(body, receivedAt)

	require.NoError(t, err)
	require.Len(t, transactions, 3)
	assert.Equal(t, int64(-1234), transactions[0].AmountMinor)
	assert.Equal(t, "早餐店", transactions[0].Merchant)
	assert.Equal(t, int64(123456), transactions[1].AmountMinor)
	assert.Equal(t, "工资", transactions[1].Merchant)
	assert.Equal(t, int64(50000), transactions[2].AmountMinor)
	assert.Contains(t, transactions[2].Merchant, "资金归集")
	assert.Equal(t, 18, transactions[2].OccurredAt.Hour())
}

func TestCMBDebitParserParsesMultilineIncomePrefix(t *testing.T) {
	parser := NewCMBDebitParser()
	receivedAt := time.Date(2026, 9, 7, 20, 0, 0, 0, time.FixedZone("CST", 8*60*60))

	transactions, err := parser.Parse("您的账户于09月07日09:45\n工资入账100.00元。", receivedAt)

	require.NoError(t, err)
	require.Len(t, transactions, 1)
	assert.Equal(t, int64(10000), transactions[0].AmountMinor)
	assert.Equal(t, "工资", transactions[0].Merchant)
}

func TestCMBDebitParserUsesPreviousYearForDecemberMailReceivedInJanuary(t *testing.T) {
	parser := NewCMBDebitParser()
	receivedAt := time.Date(2027, 1, 1, 1, 0, 0, 0, testLocation(t))

	transactions, err := parser.Parse(
		"您尾号1234的账户于12月31日23:59在便利店消费人民币1.00元。",
		receivedAt,
	)

	require.NoError(t, err)
	require.Len(t, transactions, 1)
	assert.Equal(t, 2026, transactions[0].OccurredAt.Year())
}

func TestMoneyToMinorRoundsHalfUp(t *testing.T) {
	minor, err := moneyToMinor("1.235")

	require.NoError(t, err)
	assert.Equal(t, int64(124), minor)
}
