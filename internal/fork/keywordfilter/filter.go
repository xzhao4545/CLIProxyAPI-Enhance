package keywordfilter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

const (
	maxMatchTextRunes  = 1024
	maxStreamTextRunes = 65536
)

// MatchResult describes a keyword filter match.
type MatchResult struct {
	RuleIndex int
	Keyword   string
	Text      string
}

// PayloadCheckResult describes keyword matching and text extraction for a
// stream chunk. HasText is false for transport/control metadata that should not
// commit a stream response before the first real response text is inspected.
type PayloadCheckResult struct {
	Match   *MatchResult
	HasText bool
}

// CheckPayload checks a single chunk payload against all enabled keyword filter rules.
// Returns nil if no rule matches.
func CheckPayload(payload []byte, rules []config.KeywordFilterRule) *MatchResult {
	if len(payload) == 0 || len(rules) == 0 {
		return nil
	}
	texts := extractTexts(payload)
	return checkTexts(texts, rules)
}

// CheckText checks already extracted response text against all enabled rules.
func CheckText(text string, rules []config.KeywordFilterRule) *MatchResult {
	if text == "" || len(rules) == 0 {
		return nil
	}
	return checkTexts([]string{text}, rules)
}

// StreamChecker preserves extracted stream text across chunks so boundary
// modes still work when upstream splits a sentence over multiple SSE frames.
type StreamChecker struct {
	rules []config.KeywordFilterRule
	text  string
}

func NewStreamChecker(rules []config.KeywordFilterRule) *StreamChecker {
	if len(rules) == 0 {
		return nil
	}
	return &StreamChecker{rules: append([]config.KeywordFilterRule(nil), rules...)}
}

func (c *StreamChecker) CheckPayload(payload []byte) *MatchResult {
	return c.CheckPayloadResult(payload).Match
}

func (c *StreamChecker) CheckPayloadResult(payload []byte) PayloadCheckResult {
	if c == nil || len(payload) == 0 || len(c.rules) == 0 {
		return PayloadCheckResult{}
	}
	texts := extractTexts(payload)
	for _, text := range texts {
		c.appendText(text)
		if match := CheckText(c.text, c.rules); match != nil {
			return PayloadCheckResult{Match: match, HasText: true}
		}
	}
	return PayloadCheckResult{HasText: len(texts) > 0}
}

func (c *StreamChecker) appendText(text string) {
	if text == "" {
		return
	}
	c.text += text
	if utf8.RuneCountInString(c.text) <= maxStreamTextRunes {
		return
	}
	runes := []rune(c.text)
	c.text = string(runes[len(runes)-maxStreamTextRunes:])
}

func checkTexts(texts []string, rules []config.KeywordFilterRule) *MatchResult {
	if len(texts) == 0 {
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
		return strings.HasPrefix(strings.TrimLeftFunc(text, unicode.IsSpace), keyword)
	case "end":
		return strings.HasSuffix(strings.TrimRightFunc(text, unicode.IsSpace), keyword)
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
	trimmed := bytes.TrimSpace(payload)
	if texts, ok := extractSSEDataTexts(trimmed); ok {
		return texts
	}
	if isSSEControlPayload(trimmed) {
		return nil
	}
	if !bytes.HasPrefix(trimmed, []byte{'{'}) {
		return []string{string(payload)}
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return []string{string(payload)}
	}

	var texts []string
	hasGeminiCandidates := false

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
			case "response.output_text.delta":
				appendJSONStringRaw(&texts, raw["delta"])
			case "response.output_text.done":
				appendJSONStringRaw(&texts, raw["text"])
			case "response.content_part.added", "response.content_part.done":
				appendPartText(&texts, raw["part"])
			case "response.output_item.added", "response.output_item.done":
				appendItemContentTexts(&texts, raw["item"])
			case "response.completed":
				appendResponseOutputTexts(&texts, raw["response"])
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
		hasGeminiCandidates = true
		appendGeminiCandidateTexts(&texts, candidates)
	}
	if responseRaw, ok := raw["response"]; ok {
		var response map[string]json.RawMessage
		if json.Unmarshal(responseRaw, &response) == nil {
			if candidates, ok := response["candidates"]; ok {
				hasGeminiCandidates = true
				appendGeminiCandidateTexts(&texts, candidates)
			}
		}
	}

	if len(texts) == 0 {
		if isOpenAIResponseEvent(raw) || isAnthropicStreamEvent(raw) || hasGeminiCandidates {
			return nil
		}
		return []string{string(payload)}
	}
	return texts
}

func appendGeminiCandidateTexts(texts *[]string, raw json.RawMessage) {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		return
	}
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
					*texts = append(*texts, p.Text)
				}
			}
		}
	}
}

func isOpenAIResponseEvent(raw map[string]json.RawMessage) bool {
	msgType, ok := raw["type"]
	if !ok {
		return false
	}
	var t string
	return json.Unmarshal(msgType, &t) == nil && strings.HasPrefix(t, "response.")
}

func isAnthropicStreamEvent(raw map[string]json.RawMessage) bool {
	msgType, ok := raw["type"]
	if !ok {
		return false
	}
	var t string
	if json.Unmarshal(msgType, &t) != nil {
		return false
	}
	switch t {
	case "message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop":
		return true
	default:
		return false
	}
}

func appendJSONStringRaw(texts *[]string, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		*texts = append(*texts, s)
	}
}

func appendPartText(texts *[]string, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var part struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &part) == nil && part.Text != "" {
		*texts = append(*texts, part.Text)
	}
}

func appendItemContentTexts(texts *[]string, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var item struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &item) != nil {
		return
	}
	for _, content := range item.Content {
		if content.Text != "" {
			*texts = append(*texts, content.Text)
		}
	}
}

func appendResponseOutputTexts(texts *[]string, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var response struct {
		Output []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return
	}
	for _, output := range response.Output {
		for _, content := range output.Content {
			if content.Text != "" {
				*texts = append(*texts, content.Text)
			}
		}
	}
}

func extractSSEDataTexts(payload []byte) ([]string, bool) {
	var texts []string
	hasDataLine := false
	for _, line := range bytes.Split(payload, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || bytes.HasPrefix(line, []byte{':'}) {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		hasDataLine = true
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		texts = append(texts, extractTexts(data)...)
	}
	if !hasDataLine {
		return nil, false
	}
	return texts, true
}

func isSSEControlPayload(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	hasControlLine := false
	for _, line := range bytes.Split(payload, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || bytes.HasPrefix(line, []byte{':'}) {
			continue
		}
		if bytes.HasPrefix(line, []byte("event:")) ||
			bytes.HasPrefix(line, []byte("id:")) ||
			bytes.HasPrefix(line, []byte("retry:")) {
			hasControlLine = true
			continue
		}
		return false
	}
	return hasControlLine
}

// ErrorMessage formats the error message returned when a keyword filter matches.
func ErrorMessage(match *MatchResult) string {
	return fmt.Sprintf("keyword filter matched: response contains %q (keyword: %q)", match.Text, match.Keyword)
}
