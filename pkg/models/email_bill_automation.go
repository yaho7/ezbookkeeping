package models

// EmailBillMailbox stores one user-owned mailbox connection without credentials.
type EmailBillMailbox struct {
	MailboxId       int64  `xorm:"PK"`
	Uid             int64  `xorm:"INDEX NOT NULL"`
	Provider        string `xorm:"VARCHAR(32) NOT NULL"`
	Address         string `xorm:"VARCHAR(254) NOT NULL"`
	Status          string `xorm:"VARCHAR(24) NOT NULL"`
	CreatedUnixTime int64
	UpdatedUnixTime int64
	DeletedUnixTime int64
}

// EmailBillMailboxCredential stores encrypted mailbox authentication material.
type EmailBillMailboxCredential struct {
	MailboxId       int64  `xorm:"PK"`
	EncryptedSecret string `xorm:"TEXT NOT NULL"`
	EncryptionKeyId string `xorm:"VARCHAR(64) NOT NULL"`
	UpdatedUnixTime int64
}

// EmailBillInboundMessage is the stable identity of one fetched email.
type EmailBillInboundMessage struct {
	MessageId             int64  `xorm:"PK"`
	Uid                   int64  `xorm:"INDEX NOT NULL"`
	MailboxId             int64  `xorm:"UNIQUE(UNQ_email_bill_message_fingerprint) NOT NULL"`
	RemoteMessageId       string `xorm:"VARCHAR(998)"`
	MessageFingerprint    string `xorm:"UNIQUE(UNQ_email_bill_message_fingerprint) VARCHAR(80) NOT NULL"`
	FingerprintVersion    uint16 `xorm:"UNIQUE(UNQ_email_bill_message_fingerprint) NOT NULL"`
	Sender                string `xorm:"VARCHAR(254) NOT NULL"`
	Subject               string `xorm:"TEXT NOT NULL"`
	ReceivedUnixTime      int64  `xorm:"INDEX NOT NULL"`
	BodyHash              string `xorm:"VARCHAR(80) NOT NULL"`
	BodySummary           string `xorm:"TEXT"`
	BodyContent           string `xorm:"TEXT"`
	RawMessageRef         string `xorm:"VARCHAR(512)"`
	AuthenticationStatus  string `xorm:"VARCHAR(24) NOT NULL"`
	AuthenticationSummary string `xorm:"TEXT"`
	CreatedUnixTime       int64
}

// EmailBillParserRule is a logical, user-editable parser rule.
type EmailBillParserRule struct {
	ParserRuleId     int64  `xorm:"PK"`
	Uid              int64  `xorm:"INDEX NOT NULL"`
	Name             string `xorm:"VARCHAR(128) NOT NULL"`
	Bank             string `xorm:"VARCHAR(64)"`
	Enabled          bool   `xorm:"INDEX NOT NULL"`
	Priority         int32  `xorm:"INDEX NOT NULL"`
	CurrentVersionId int64  `xorm:"INDEX NOT NULL"`
	CreatedUnixTime  int64
	UpdatedUnixTime  int64
	DeletedUnixTime  int64
}

// EmailBillParserRuleVersion snapshots matcher, code, and runtime configuration.
type EmailBillParserRuleVersion struct {
	ParserRuleVersionId int64  `xorm:"PK"`
	ParserRuleId        int64  `xorm:"UNIQUE(UNQ_email_bill_parser_version) INDEX NOT NULL"`
	Version             int32  `xorm:"UNIQUE(UNQ_email_bill_parser_version) NOT NULL"`
	MatcherJson         string `xorm:"TEXT NOT NULL"`
	MatcherHash         string `xorm:"VARCHAR(80) NOT NULL"`
	SourceCode          string `xorm:"TEXT NOT NULL"`
	SourceHash          string `xorm:"VARCHAR(80) NOT NULL"`
	Runtime             string `xorm:"VARCHAR(32) NOT NULL"`
	RuntimeVersion      string `xorm:"VARCHAR(32) NOT NULL"`
	ParserApiVersion    uint16 `xorm:"NOT NULL"`
	VersionHash         string `xorm:"UNIQUE(UNQ_email_bill_parser_version_hash) VARCHAR(80) NOT NULL"`
	ExecutionTimeoutMs  uint32 `xorm:"NOT NULL"`
	InstructionLimit    uint64 `xorm:"NOT NULL"`
	CreatedBy           string `xorm:"VARCHAR(24) NOT NULL"`
	CreatedUnixTime     int64
}

// EmailBillImportRun represents one processing request for one message.
type EmailBillImportRun struct {
	ImportRunId       int64  `xorm:"PK"`
	MessageId         int64  `xorm:"INDEX NOT NULL"`
	TriggerType       string `xorm:"VARCHAR(24) NOT NULL"`
	ParentRunId       int64  `xorm:"INDEX"`
	Status            string `xorm:"INDEX VARCHAR(32) NOT NULL"`
	StartedUnixTime   int64
	CompletedUnixTime int64
}

// EmailBillImportRunEvent is an immutable import-run state transition.
type EmailBillImportRunEvent struct {
	EventId         int64  `xorm:"PK"`
	ImportRunId     int64  `xorm:"INDEX NOT NULL"`
	FromStatus      string `xorm:"VARCHAR(32)"`
	ToStatus        string `xorm:"VARCHAR(32) NOT NULL"`
	Reason          string `xorm:"TEXT"`
	CreatedUnixTime int64
}

// EmailBillParserRun records one parser version execution in an import run.
type EmailBillParserRun struct {
	ParserRunId         int64  `xorm:"PK"`
	ImportRunId         int64  `xorm:"UNIQUE(UNQ_email_bill_parser_run) INDEX NOT NULL"`
	ParserRuleVersionId int64  `xorm:"UNIQUE(UNQ_email_bill_parser_run) INDEX NOT NULL"`
	Matched             bool   `xorm:"NOT NULL"`
	Status              string `xorm:"VARCHAR(24) NOT NULL"`
	StartedUnixTime     int64
	FinishedUnixTime    int64
	DurationMillis      int64
	InstructionCount    uint64
	InputHash           string `xorm:"VARCHAR(80) NOT NULL"`
	ErrorType           string `xorm:"VARCHAR(64)"`
	ErrorMessage        string `xorm:"TEXT"`
	ErrorLine           int32
}

// EmailBillParserOutput is immutable parser evidence and intentionally has no
// account, category, or transaction references.
type EmailBillParserOutput struct {
	ParserOutputId      int64  `xorm:"PK"`
	ParserRunId         int64  `xorm:"INDEX NOT NULL"`
	ExternalId          string `xorm:"VARCHAR(255)"`
	BillSequence        int32
	TransactionUnixTime int64
	Amount              int64
	Currency            string `xorm:"VARCHAR(3) NOT NULL"`
	Direction           string `xorm:"VARCHAR(24) NOT NULL"`
	Merchant            string `xorm:"TEXT"`
	Description         string `xorm:"TEXT"`
	Bank                string `xorm:"VARCHAR(64)"`
	CardType            string `xorm:"VARCHAR(32)"`
	CardLast4           string `xorm:"VARCHAR(16)"`
	RawStandardBillJson string `xorm:"TEXT NOT NULL"`
	CreatedUnixTime     int64
}

// EmailBillCandidate is one logical bill identity.
type EmailBillCandidate struct {
	CandidateId                     int64  `xorm:"PK"`
	Uid                             int64  `xorm:"UNIQUE(UNQ_email_bill_candidate_identity) INDEX NOT NULL"`
	MessageId                       int64  `xorm:"INDEX NOT NULL"`
	IdentityKey                     string `xorm:"UNIQUE(UNQ_email_bill_candidate_identity) VARCHAR(80) NOT NULL"`
	IdentityVersion                 uint16 `xorm:"UNIQUE(UNQ_email_bill_candidate_identity) NOT NULL"`
	Status                          string `xorm:"INDEX VARCHAR(32) NOT NULL"`
	SelectedVariantId               int64  `xorm:"INDEX"`
	CurrentAccountDecisionId        int64  `xorm:"INDEX"`
	CurrentClassificationDecisionId int64  `xorm:"INDEX"`
	CreatedUnixTime                 int64
	UpdatedUnixTime                 int64
}

// EmailBillCandidateVariant is one exact parsed representation of a candidate.
type EmailBillCandidateVariant struct {
	VariantId           int64  `xorm:"PK"`
	CandidateId         int64  `xorm:"UNIQUE(UNQ_email_bill_candidate_variant) INDEX NOT NULL"`
	BillFingerprint     string `xorm:"UNIQUE(UNQ_email_bill_candidate_variant) VARCHAR(80) NOT NULL"`
	FingerprintVersion  uint16 `xorm:"UNIQUE(UNQ_email_bill_candidate_variant) NOT NULL"`
	TransactionUnixTime int64
	Amount              int64
	Currency            string `xorm:"VARCHAR(3) NOT NULL"`
	Direction           string `xorm:"VARCHAR(24) NOT NULL"`
	Merchant            string `xorm:"TEXT"`
	Description         string `xorm:"TEXT"`
	Bank                string `xorm:"VARCHAR(64)"`
	CardType            string `xorm:"VARCHAR(32)"`
	CardLast4           string `xorm:"VARCHAR(16)"`
	ExternalId          string `xorm:"VARCHAR(255)"`
	BillSequence        int32
	CreatedUnixTime     int64
}

// EmailBillCandidateEvidence links every parser output supporting a variant.
type EmailBillCandidateEvidence struct {
	CandidateEvidenceId int64 `xorm:"PK"`
	VariantId           int64 `xorm:"UNIQUE(UNQ_email_bill_candidate_evidence) INDEX NOT NULL"`
	ParserOutputId      int64 `xorm:"UNIQUE(UNQ_email_bill_candidate_evidence) INDEX NOT NULL"`
	CreatedUnixTime     int64
}

// EmailBillCandidateConflict records unresolved multiple variants.
type EmailBillCandidateConflict struct {
	ConflictId       int64  `xorm:"PK"`
	CandidateId      int64  `xorm:"INDEX NOT NULL"`
	Status           string `xorm:"VARCHAR(24) NOT NULL"`
	CreatedUnixTime  int64
	ResolvedUnixTime int64
}

// EmailBillCandidateConflictItem links conflicting variants.
type EmailBillCandidateConflictItem struct {
	ConflictItemId int64 `xorm:"PK"`
	ConflictId     int64 `xorm:"UNIQUE(UNQ_email_bill_conflict_item) INDEX NOT NULL"`
	VariantId      int64 `xorm:"UNIQUE(UNQ_email_bill_conflict_item) INDEX NOT NULL"`
}

// EmailBillAccountRoutingRule is a logical routing rule.
type EmailBillAccountRoutingRule struct {
	RoutingRuleId    int64 `xorm:"PK"`
	Uid              int64 `xorm:"INDEX NOT NULL"`
	Enabled          bool  `xorm:"INDEX NOT NULL"`
	Priority         int32 `xorm:"INDEX NOT NULL"`
	CurrentVersionId int64 `xorm:"INDEX NOT NULL"`
	CreatedUnixTime  int64
	UpdatedUnixTime  int64
	DeletedUnixTime  int64
}

// EmailBillAccountRoutingRuleVersion snapshots routing conditions and target.
type EmailBillAccountRoutingRuleVersion struct {
	RoutingRuleVersionId int64  `xorm:"PK"`
	RoutingRuleId        int64  `xorm:"UNIQUE(UNQ_email_bill_routing_version) INDEX NOT NULL"`
	Version              int32  `xorm:"UNIQUE(UNQ_email_bill_routing_version) NOT NULL"`
	ConditionsJson       string `xorm:"TEXT NOT NULL"`
	RuleSignature        string `xorm:"INDEX VARCHAR(80) NOT NULL"`
	TargetAccountId      int64  `xorm:"INDEX NOT NULL"`
	CreatedUnixTime      int64
}

// EmailBillAccountRoutingDecision is an immutable routing decision.
type EmailBillAccountRoutingDecision struct {
	AccountDecisionId    int64  `xorm:"PK"`
	CandidateId          int64  `xorm:"INDEX NOT NULL"`
	ImportRunId          int64  `xorm:"INDEX NOT NULL"`
	DecisionType         string `xorm:"VARCHAR(24) NOT NULL"`
	RoutingRuleVersionId int64  `xorm:"INDEX"`
	AccountId            int64  `xorm:"INDEX"`
	Confidence           float64
	Reason               string `xorm:"TEXT"`
	CreatedUnixTime      int64
}

// EmailBillClassificationRule is a logical manual or learned rule.
type EmailBillClassificationRule struct {
	ClassificationRuleId int64  `xorm:"PK"`
	Uid                  int64  `xorm:"INDEX NOT NULL"`
	Origin               string `xorm:"INDEX VARCHAR(24) NOT NULL"`
	Enabled              bool   `xorm:"INDEX NOT NULL"`
	Priority             int32  `xorm:"INDEX NOT NULL"`
	CurrentVersionId     int64  `xorm:"INDEX NOT NULL"`
	CreatedUnixTime      int64
	UpdatedUnixTime      int64
	DisabledUnixTime     int64
}

// EmailBillClassificationRuleVersion snapshots match and category behavior.
type EmailBillClassificationRuleVersion struct {
	ClassificationRuleVersionId int64  `xorm:"PK"`
	ClassificationRuleId        int64  `xorm:"UNIQUE(UNQ_email_bill_classification_version) INDEX NOT NULL"`
	Version                     int32  `xorm:"UNIQUE(UNQ_email_bill_classification_version) NOT NULL"`
	MerchantPattern             string `xorm:"TEXT NOT NULL"`
	MatchType                   string `xorm:"VARCHAR(16) NOT NULL"`
	ScopeJson                   string `xorm:"TEXT NOT NULL"`
	RuleSignature               string `xorm:"INDEX VARCHAR(80) NOT NULL"`
	CategoryId                  int64  `xorm:"INDEX NOT NULL"`
	Confidence                  float64
	CreatedUnixTime             int64
}

// EmailBillLLMClassificationRun is one immutable LLM request and response.
type EmailBillLLMClassificationRun struct {
	LLMRunId                 int64  `xorm:"PK"`
	CandidateId              int64  `xorm:"INDEX NOT NULL"`
	ImportRunId              int64  `xorm:"INDEX NOT NULL"`
	Provider                 string `xorm:"VARCHAR(64) NOT NULL"`
	Model                    string `xorm:"VARCHAR(128) NOT NULL"`
	PromptVersion            uint16
	InputHash                string `xorm:"VARCHAR(80) NOT NULL"`
	ExistingCategorySnapshot string `xorm:"TEXT NOT NULL"`
	ResultType               string `xorm:"VARCHAR(32) NOT NULL"`
	SelectedCategoryId       int64  `xorm:"INDEX"`
	ProposedCategoryName     string `xorm:"VARCHAR(64)"`
	ProposedParentCategoryId int64  `xorm:"INDEX"`
	Confidence               float64
	Reason                   string `xorm:"TEXT"`
	TokenUsage               int64
	DurationMillis           int64
	CreatedUnixTime          int64
}

// EmailBillCategoryCreationProposal separates LLM suggestions from mutations.
type EmailBillCategoryCreationProposal struct {
	ProposalId        int64                   `xorm:"PK"`
	CandidateId       int64                   `xorm:"INDEX NOT NULL"`
	ImportRunId       int64                   `xorm:"INDEX NOT NULL"`
	LLMRunId          int64                   `xorm:"INDEX NOT NULL"`
	ProposedName      string                  `xorm:"VARCHAR(64) NOT NULL"`
	ParentCategoryId  int64                   `xorm:"INDEX"`
	CategoryType      TransactionCategoryType `xorm:"NOT NULL"`
	Confidence        float64
	Status            string `xorm:"INDEX VARCHAR(24) NOT NULL"`
	CreatedCategoryId int64  `xorm:"INDEX"`
	CreatedUnixTime   int64
}

// EmailBillCategoryCreationClaim prevents concurrent duplicate AI categories.
type EmailBillCategoryCreationClaim struct {
	CategoryClaimId  int64                   `xorm:"PK"`
	Uid              int64                   `xorm:"UNIQUE(UNQ_email_bill_category_claim) NOT NULL"`
	CategoryType     TransactionCategoryType `xorm:"UNIQUE(UNQ_email_bill_category_claim) NOT NULL"`
	ParentCategoryId int64                   `xorm:"UNIQUE(UNQ_email_bill_category_claim) NOT NULL"`
	NormalizedName   string                  `xorm:"UNIQUE(UNQ_email_bill_category_claim) VARCHAR(64) NOT NULL"`
	CategoryId       int64                   `xorm:"INDEX"`
	CreatedUnixTime  int64
}

// EmailBillClassificationDecision is an immutable classification decision.
type EmailBillClassificationDecision struct {
	ClassificationDecisionId    int64  `xorm:"PK"`
	CandidateId                 int64  `xorm:"INDEX NOT NULL"`
	ImportRunId                 int64  `xorm:"INDEX NOT NULL"`
	DecisionType                string `xorm:"VARCHAR(24) NOT NULL"`
	ClassificationRuleVersionId int64  `xorm:"INDEX"`
	LLMRunId                    int64  `xorm:"INDEX"`
	CategoryId                  int64  `xorm:"INDEX"`
	Confidence                  float64
	Reason                      string `xorm:"TEXT"`
	CreatedUnixTime             int64
}

// EmailBillConfirmationAction records a user's append-only correction.
type EmailBillConfirmationAction struct {
	ConfirmationActionId int64  `xorm:"PK"`
	CandidateId          int64  `xorm:"INDEX NOT NULL"`
	ImportRunId          int64  `xorm:"INDEX NOT NULL"`
	Uid                  int64  `xorm:"INDEX NOT NULL"`
	Action               string `xorm:"VARCHAR(24) NOT NULL"`
	PreviousVariantId    int64  `xorm:"INDEX"`
	SelectedVariantId    int64  `xorm:"INDEX"`
	PreviousAccountId    int64  `xorm:"INDEX"`
	SelectedAccountId    int64  `xorm:"INDEX"`
	PreviousCategoryId   int64  `xorm:"INDEX"`
	SelectedCategoryId   int64  `xorm:"INDEX"`
	CreatedUnixTime      int64
}

// EmailBillTransactionImportIntent is the stable exactly-once boundary.
type EmailBillTransactionImportIntent struct {
	ImportIntentId    int64  `xorm:"PK"`
	CandidateId       int64  `xorm:"UNIQUE NOT NULL"`
	IdempotencyKey    string `xorm:"UNIQUE VARCHAR(80) NOT NULL"`
	Status            string `xorm:"INDEX VARCHAR(24) NOT NULL"`
	TransactionId     int64  `xorm:"INDEX"`
	CreatedUnixTime   int64
	CompletedUnixTime int64
}

// EmailBillTransactionImportAttempt is one immutable execution attempt.
type EmailBillTransactionImportAttempt struct {
	ImportAttemptId   int64  `xorm:"PK"`
	ImportIntentId    int64  `xorm:"UNIQUE(UNQ_email_bill_import_attempt) INDEX NOT NULL"`
	ImportRunId       int64  `xorm:"INDEX NOT NULL"`
	AttemptNumber     int32  `xorm:"UNIQUE(UNQ_email_bill_import_attempt) NOT NULL"`
	Status            string `xorm:"VARCHAR(24) NOT NULL"`
	ErrorType         string `xorm:"VARCHAR(64)"`
	ErrorMessage      string `xorm:"TEXT"`
	TransactionId     int64  `xorm:"INDEX"`
	StartedUnixTime   int64
	CompletedUnixTime int64
}

// EmailBillAuditEvent is the append-only cross-stage event stream.
type EmailBillAuditEvent struct {
	AuditEventId    int64  `xorm:"PK"`
	Uid             int64  `xorm:"INDEX NOT NULL"`
	MessageId       int64  `xorm:"INDEX"`
	CandidateId     int64  `xorm:"INDEX"`
	ImportRunId     int64  `xorm:"INDEX"`
	EventType       string `xorm:"INDEX VARCHAR(64) NOT NULL"`
	ActorType       string `xorm:"VARCHAR(24) NOT NULL"`
	ActorId         int64
	PayloadJson     string `xorm:"TEXT NOT NULL"`
	CreatedUnixTime int64  `xorm:"INDEX NOT NULL"`
}
