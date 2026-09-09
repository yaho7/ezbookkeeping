package emailbill

import "time"

// AccountHint contains parser-provided routing hints, never a database account ID.
type AccountHint struct {
	Bank  string `json:"bank"`
	Kind  string `json:"kind"`
	Last4 string `json:"last4"`
}

// StandardBill is the validated output of a parser rule.
type StandardBill struct {
	ExternalID  string      `json:"external_id"`
	OccurredAt  time.Time   `json:"occurred_at"`
	AmountMinor int64       `json:"amount_minor"`
	Currency    string      `json:"currency"`
	FlowType    string      `json:"flow_type"`
	Merchant    string      `json:"merchant"`
	Description string      `json:"description"`
	AccountHint AccountHint `json:"account_hint"`
}
