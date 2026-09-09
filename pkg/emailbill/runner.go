package emailbill

import "sync"

// RunnableParserRule is an immutable rule version selected for a run.
type RunnableParserRule struct {
	VersionID int64
	Matcher   ParserMatcher
	Source    string
}

// ParserExecutionResult is the isolated outcome of one rule.
type ParserExecutionResult struct {
	VersionID int64
	Matched   bool
	Bills     []StandardBill
	Stats     ScriptStats
	Err       error
}

// RunParserRules evaluates all matchers and runs every match independently.
func RunParserRules(parser *ScriptParser, mail ScriptMail, rules []RunnableParserRule) []ParserExecutionResult {
	results := make([]ParserExecutionResult, len(rules))
	var wait sync.WaitGroup
	for index, rule := range rules {
		results[index] = ParserExecutionResult{VersionID: rule.VersionID}
		if !rule.Matcher.Matches(mail) {
			continue
		}
		results[index].Matched = true
		wait.Add(1)
		go func(index int, rule RunnableParserRule) {
			defer wait.Done()
			results[index].Bills, results[index].Stats, results[index].Err = parser.Parse(rule.Source, mail)
		}(index, rule)
	}
	wait.Wait()
	return results
}
