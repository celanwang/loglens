package analyze

import (
	"regexp"
	"sort"
	"strings"

	"github.com/celanwang/loglens/internal/model"
)

// 消息归一化：长十六进制串与数字替换为占位符，使同类错误收敛为同一模式。
var normalizeRe = regexp.MustCompile(`[0-9a-fA-F]{8,}|\d+`)

const maxErrorGroups = 20

// groupKey 错误归组键：优先 error= 字段，否则使用归一化后的消息模板。
func groupKey(e model.Entry) string {
	if v := e.Fields["error"]; v != "" {
		return v
	}
	s := strings.TrimSpace(normalizeRe.ReplaceAllString(e.Msg, "*"))
	if s == "" {
		s = strings.TrimSpace(normalizeRe.ReplaceAllString(e.Raw, "*"))
	}
	if s == "" {
		return "(空消息)"
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

func buildErrorGroups(entries []model.Entry, r *model.Report) {
	type agg struct {
		g      *model.ErrorGroup
		mods   map[string]bool
		traces map[string]bool
	}
	m := map[string]*agg{}
	for _, e := range entries {
		if e.ParseErr || !isErrorLevel(e.Level) {
			continue
		}
		k := groupKey(e)
		a := m[k]
		if a == nil {
			a = &agg{
				g:      &model.ErrorGroup{Key: k, Sample: e.Raw, First: e.Time, Last: e.Time},
				mods:   map[string]bool{},
				traces: map[string]bool{},
			}
			m[k] = a
		}
		a.g.Count++
		if e.Module != "" {
			a.mods[e.Module] = true
		}
		if e.TraceID != "" {
			a.traces[e.TraceID] = true
			if len(a.g.SampleTraces) < 3 && !containsStr(a.g.SampleTraces, e.TraceID) {
				a.g.SampleTraces = append(a.g.SampleTraces, e.TraceID)
			}
		}
		if e.HasTime {
			if a.g.First.IsZero() || e.Time.Before(a.g.First) {
				a.g.First = e.Time
			}
			if e.Time.After(a.g.Last) {
				a.g.Last = e.Time
			}
		}
	}
	groups := make([]model.ErrorGroup, 0, len(m))
	for _, a := range m {
		a.g.TraceCount = len(a.traces)
		for mod := range a.mods {
			a.g.Modules = append(a.g.Modules, mod)
		}
		sort.Strings(a.g.Modules)
		groups = append(groups, *a.g)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].Key < groups[j].Key
	})
	if len(groups) > maxErrorGroups {
		groups = groups[:maxErrorGroups]
	}
	r.Errors = groups
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
