package skills

import "strings"

// Skill represents a verified, reusable coding capability for a specific financial task.
type Skill struct {
	Name        string   // unique identifier, e.g. "candlestick_chart"
	Description string   // one-line description for LLM context
	Keywords    []string // trigger words for matching (lowercase)
	Template    string   // verified, runnable Python code template
	APITips     string   // critical API notes / pitfalls
}

var registry []Skill

func Register(s Skill) {
	registry = append(registry, s)
}

// MatchSkills returns all skills whose keywords overlap with the instruction.
// Results are sorted by relevance (number of keyword hits, descending).
func MatchSkills(instruction string, maxResults int) []Skill {
	lower := strings.ToLower(instruction)

	type scored struct {
		skill Skill
		hits  int
	}
	var candidates []scored

	for _, s := range registry {
		hits := 0
		for _, kw := range s.Keywords {
			if strings.Contains(lower, kw) {
				hits++
			}
		}
		if hits > 0 {
			candidates = append(candidates, scored{s, hits})
		}
	}

	// simple descending sort by hit count
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].hits > candidates[i].hits {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	var result []Skill
	for i, c := range candidates {
		if i >= maxResults {
			break
		}
		result = append(result, c.skill)
	}
	return result
}

// FormatForPrompt renders matched skills into a string the LLM can reference.
func FormatForPrompt(matched []Skill) string {
	if len(matched) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n【可参考的 Skill 模板（经过验证可直接运行，请优先参考）】:\n")
	for i, s := range matched {
		sb.WriteString(strings.Repeat("─", 60))
		sb.WriteString("\n")
		sb.WriteString("### Skill ")
		sb.WriteString(string(rune('①' + i)))
		sb.WriteString(": ")
		sb.WriteString(s.Name)
		sb.WriteString(" — ")
		sb.WriteString(s.Description)
		sb.WriteString("\n")
		if s.APITips != "" {
			sb.WriteString("⚠️ 关键注意事项:\n")
			sb.WriteString(s.APITips)
			sb.WriteString("\n")
		}
		sb.WriteString("```python\n")
		sb.WriteString(s.Template)
		sb.WriteString("\n```\n")
	}
	sb.WriteString(strings.Repeat("─", 60))
	return sb.String()
}
