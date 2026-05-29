package keywordfilter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

const maxMatchTextRunes = 1024

// MatchResult describes a keyword filter match.
type MatchResult struct {
	RuleIndex int
	Keyword   string
	Text      string
}

// CheckPayload checks a single chunk payload against all enabled keyword filter rules.
// Returns nil if no rule matches.
func CheckPayload(payload []byte, rules []config.KeywordFilterRule) *MatchResult {
	if len(payload) == 0 || len(rules) == 0 {
		return nil
	}

	preparedRules := make([]preparedRule, 0, len(rules))
	for ri := range rules {
		rule := &rules[ri]
		if !rule.Enabled || rule.Keyword == "" {
			continue
		}
		keyword := rule.Keyword
		if !rule.CaseSensitive {
			keyword = strings.ToLower(keyword)
		}
		preparedRules = append(preparedRules, preparedRule{
			index:         ri,
			keyword:       rule.Keyword,
			matchKeyword:  keyword,
			matchMode:     rule.MatchMode,
			caseSensitive: rule.CaseSensitive,
		})
	}
	if len(preparedRules) == 0 {
		return nil
	}

	texts := extractTexts(payload)
	lowerTexts := make([]string, len(texts))
	for _, rule := range preparedRules {
		for ti, text := range texts {
			candidate := text
			if !rule.caseSensitive {
				if lowerTexts[ti] == "" {
					lowerTexts[ti] = strings.ToLower(text)
				}
				candidate = lowerTexts[ti]
			}
			if matchText(candidate, rule.matchKeyword, rule.matchMode) {
				return &MatchResult{
					RuleIndex: rule.index,
					Keyword:   rule.keyword,
					Text:      excerptMatchText(text, rule.keyword, rule.caseSensitive),
				}
			}
		}
	}

	return nil
}

type preparedRule struct {
	index         int
	keyword       string
	matchKeyword  string
	matchMode     string
	caseSensitive bool
}

func matchText(text, keyword, mode string) bool {
	switch mode {
	case "start":
		return strings.HasPrefix(text, keyword)
	case "end":
		return strings.HasSuffix(text, keyword)
	case "exact":
		return text == keyword
	default: // "anywhere"
		return strings.Contains(text, keyword)
	}
}

func excerptMatchText(text, keyword string, caseSensitive bool) string {
	if utf8.RuneCountInString(text) <= maxMatchTextRunes {
		return text
	}

	runes := []rune(text)
	keywordStart := keywordRuneIndex(text, keyword, caseSensitive)
	if keywordStart < 0 {
		return string(runes[:maxMatchTextRunes]) + "..."
	}

	keywordLength := utf8.RuneCountInString(keyword)
	contextRunes := (maxMatchTextRunes - keywordLength) / 2
	if contextRunes < 0 {
		contextRunes = 0
	}

	start := keywordStart - contextRunes
	if start < 0 {
		start = 0
	}
	end := start + maxMatchTextRunes
	if end > len(runes) {
		end = len(runes)
		start = end - maxMatchTextRunes
		if start < 0 {
			start = 0
		}
	}

	excerpt := string(runes[start:end])
	if start > 0 {
		excerpt = "..." + excerpt
	}
	if end < len(runes) {
		excerpt += "..."
	}
	return excerpt
}

func keywordRuneIndex(text, keyword string, caseSensitive bool) int {
	if keyword == "" {
		return -1
	}
	if caseSensitive {
		idx := strings.Index(text, keyword)
		if idx < 0 {
			return -1
		}
		return utf8.RuneCountInString(text[:idx])
	}

	textRunes := []rune(text)
	keywordRunes := []rune(keyword)
	if len(keywordRunes) == 0 || len(keywordRunes) > len(textRunes) {
		return -1
	}
	for i := 0; i <= len(textRunes)-len(keywordRunes); i++ {
		if strings.EqualFold(string(textRunes[i:i+len(keywordRunes)]), keyword) {
			return i
		}
	}
	return -1
}

func extractTexts(payload []byte) []string {
	if !bytes.HasPrefix(bytes.TrimSpace(payload), []byte{'{'}) {
		return []string{string(payload)}
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return []string{string(payload)}
	}

	var texts []string

	if choices, ok := raw["choices"]; ok {
		var arr []json.RawMessage
		if err := json.Unmarshal(choices, &arr); err == nil {
			for _, item := range arr {
				var choice struct {
					Delta   map[string]json.RawMessage `json:"delta"`
					Message map[string]json.RawMessage `json:"message"`
					Text    string                     `json:"text"`
				}
				if err := json.Unmarshal(item, &choice); err != nil {
					continue
				}
				if d := choice.Delta; d != nil {
					if content, ok := d["content"]; ok {
						var s string
						if json.Unmarshal(content, &s) == nil && s != "" {
							texts = append(texts, s)
						}
					}
				}
				if m := choice.Message; m != nil {
					if content, ok := m["content"]; ok {
						var s string
						if json.Unmarshal(content, &s) == nil && s != "" {
							texts = append(texts, s)
						}
					}
				}
				if choice.Text != "" {
					texts = append(texts, choice.Text)
				}
			}
		}
	}

	if msgType, ok := raw["type"]; ok {
		var t string
		if json.Unmarshal(msgType, &t) == nil {
			switch t {
			case "content_block_delta":
				if delta, ok := raw["delta"]; ok {
					var d struct {
						Text string `json:"text"`
					}
					if json.Unmarshal(delta, &d) == nil && d.Text != "" {
						texts = append(texts, d.Text)
					}
				}
			case "content_block_start":
				if cb, ok := raw["content_block"]; ok {
					var d struct {
						Text string `json:"text"`
					}
					if json.Unmarshal(cb, &d) == nil && d.Text != "" {
						texts = append(texts, d.Text)
					}
				}
			}
		}
	}

	if candidates, ok := raw["candidates"]; ok {
		var arr []json.RawMessage
		if json.Unmarshal(candidates, &arr) == nil {
			for _, item := range arr {
				var c struct {
					Content *struct {
						Parts []struct {
							Text string `json:"text"`
						} `json:"parts"`
					} `json:"content"`
				}
				if json.Unmarshal(item, &c) == nil && c.Content != nil {
					for _, p := range c.Content.Parts {
						if p.Text != "" {
							texts = append(texts, p.Text)
						}
					}
				}
			}
		}
	}

	if len(texts) == 0 {
		return []string{string(payload)}
	}
	return texts
}

// ErrorMessage formats the error message returned when a keyword filter matches.
func ErrorMessage(match *MatchResult) string {
	return fmt.Sprintf("keyword filter matched: response contains %q (keyword: %q)", match.Text, match.Keyword)
}
