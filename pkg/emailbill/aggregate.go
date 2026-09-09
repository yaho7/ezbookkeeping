package emailbill

// ParserResult is the successful standard output of one parser version.
type ParserResult struct {
	ParserVersionID int64
	Bills           []StandardBill
}

// AggregatedVariant is an exact parsed representation plus all supporting rules.
type AggregatedVariant struct {
	Bill               StandardBill
	Fingerprint        string
	FingerprintVersion uint16
	ParserVersionIDs   []int64
}

// AggregatedCandidate is one logical bill and all parser interpretations of it.
type AggregatedCandidate struct {
	IdentityKey     string
	IdentityVersion uint16
	Variants        []AggregatedVariant
	Conflicted      bool
}

// AggregateBills merges equivalent parser evidence and exposes conflicts.
func AggregateBills(userID int64, messageFingerprint string, results []ParserResult) []AggregatedCandidate {
	candidates := make([]AggregatedCandidate, 0)
	candidateIndexes := make(map[string]int)
	variantIndexes := make(map[string]map[string]int)

	for _, result := range results {
		for sequence, bill := range result.Bills {
			identityKey, identityVersion := CandidateIdentity(CandidateIdentityInput{
				UserID:             userID,
				Bank:               bill.AccountHint.Bank,
				AccountLast4:       bill.AccountHint.Last4,
				ExternalID:         bill.ExternalID,
				MessageFingerprint: messageFingerprint,
				BillSequence:       sequence,
			})
			candidateIndex, exists := candidateIndexes[identityKey]
			if !exists {
				candidateIndex = len(candidates)
				candidateIndexes[identityKey] = candidateIndex
				variantIndexes[identityKey] = make(map[string]int)
				candidates = append(candidates, AggregatedCandidate{
					IdentityKey: identityKey, IdentityVersion: identityVersion,
				})
			}

			fingerprint, fingerprintVersion := BillFingerprint(BillFingerprintInput{
				IdentityKey: identityKey, TransactionTime: bill.OccurredAt,
				AmountMinor: bill.AmountMinor, Currency: bill.Currency,
				Direction: bill.FlowType, Merchant: bill.Merchant, Description: bill.Description,
			})
			variantIndex, exists := variantIndexes[identityKey][fingerprint]
			if !exists {
				variantIndex = len(candidates[candidateIndex].Variants)
				variantIndexes[identityKey][fingerprint] = variantIndex
				candidates[candidateIndex].Variants = append(candidates[candidateIndex].Variants, AggregatedVariant{
					Bill: bill, Fingerprint: fingerprint, FingerprintVersion: fingerprintVersion,
				})
			}
			variant := &candidates[candidateIndex].Variants[variantIndex]
			if !containsParserVersion(variant.ParserVersionIDs, result.ParserVersionID) {
				variant.ParserVersionIDs = append(variant.ParserVersionIDs, result.ParserVersionID)
			}
		}
	}

	for index := range candidates {
		candidates[index].Conflicted = len(candidates[index].Variants) > 1
	}
	return candidates
}

func containsParserVersion(ids []int64, expected int64) bool {
	for _, id := range ids {
		if id == expected {
			return true
		}
	}
	return false
}
