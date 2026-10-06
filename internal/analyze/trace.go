package analyze

import (
	"sort"
	"strings"

	"github.com/celanwang/loglens/internal/model"
)

// traceInfo 是一条 traceId 下全部日志条目的聚合视图。
type traceInfo struct {
	id       string
	idx      []int // 指向 entries 的下标，按时间升序
	started  bool
	finished bool
	hasError bool
	total    float64
	hasTotal bool
	modules  []string
}

// buildTraces 按 traceId 聚合日志条目；条目在 trace 内按时间稳定排序。
func buildTraces(entries []model.Entry) []*traceInfo {
	byID := map[string][]int{}
	var order []string
	for i, e := range entries {
		if e.TraceID == "" {
			continue
		}
		if _, ok := byID[e.TraceID]; !ok {
			order = append(order, e.TraceID)
		}
		byID[e.TraceID] = append(byID[e.TraceID], i)
	}
	traces := make([]*traceInfo, 0, len(byID))
	for _, id := range order {
		idx := byID[id]
		sort.SliceStable(idx, func(a, b int) bool {
			ea, eb := entries[idx[a]], entries[idx[b]]
			if ea.HasTime && eb.HasTime && !ea.Time.Equal(eb.Time) {
				return ea.Time.Before(eb.Time)
			}
			if ea.HasTime != eb.HasTime {
				return ea.HasTime
			}
			return idx[a] < idx[b]
		})
		t := &traceInfo{id: id, idx: idx}
		seen := map[string]bool{}
		for _, i := range idx {
			e := entries[i]
			if e.Module != "" && !seen[e.Module] {
				seen[e.Module] = true
				t.modules = append(t.modules, e.Module)
			}
			if strings.Contains(e.Msg, "request start") {
				t.started = true
			}
			if strings.Contains(e.Msg, "request end") {
				t.finished = true
			}
			if isErrorLevel(e.Level) {
				t.hasError = true
			}
			if v, ok := parseMS(e.Fields["totalCost"]); ok {
				if !t.hasTotal || v > t.total {
					t.total = v
				}
				t.hasTotal = true
				t.finished = true
			}
		}
		traces = append(traces, t)
	}
	return traces
}

func (t *traceInfo) summary(entries []model.Entry) model.TraceSummary {
	s := model.TraceSummary{
		TraceID:   t.id,
		TotalCost: t.total,
		Modules:   t.modules,
		HasError:  t.hasError,
		Finished:  t.finished,
	}
	for _, i := range t.idx {
		if e := entries[i]; e.HasTime {
			if s.Start.IsZero() || e.Time.Before(s.Start) {
				s.Start = e.Time
			}
			if e.Time.After(s.End) {
				s.End = e.Time
			}
		}
	}
	return s
}

// buildSlowAndUnfinished 计算 Top N 慢请求与"有开始无结束"的未完成请求。
func buildSlowAndUnfinished(traces []*traceInfo, entries []model.Entry, r *model.Report, opts Options) {
	var slow, unfinished []model.TraceSummary
	for _, t := range traces {
		switch {
		case t.finished && t.hasTotal && t.total > opts.SlowMs:
			slow = append(slow, t.summary(entries))
		case t.started && !t.finished:
			unfinished = append(unfinished, t.summary(entries))
		}
	}
	sort.Slice(slow, func(i, j int) bool { return slow[i].TotalCost > slow[j].TotalCost })
	sort.Slice(unfinished, func(i, j int) bool { return unfinished[i].Start.After(unfinished[j].Start) })
	r.UnfinishedTotal = len(unfinished)
	if len(slow) > opts.TopN {
		slow = slow[:opts.TopN]
	}
	if len(unfinished) > opts.TopN {
		unfinished = unfinished[:opts.TopN]
	}
	r.SlowTraces = slow
	r.Unfinished = unfinished
}

// TraceChain 还原指定 traceId 的完整日志链路，按时间升序。
func TraceChain(entries []model.Entry, traceID string) []model.Entry {
	var out []model.Entry
	for _, e := range entries {
		if e.TraceID == traceID {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.HasTime && b.HasTime && !a.Time.Equal(b.Time) {
			return a.Time.Before(b.Time)
		}
		if a.HasTime != b.HasTime {
			return a.HasTime
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return out
}

// SuggestTraces 返回按前缀近似的 traceId 候选，用于查询未命中时提示。
func SuggestTraces(entries []model.Entry, prefix string, limit int) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		if e.TraceID == "" || seen[e.TraceID] {
			continue
		}
		if strings.Contains(e.TraceID, prefix) {
			seen[e.TraceID] = true
			out = append(out, e.TraceID)
			if len(out) >= limit {
				break
			}
		}
	}
	sort.Strings(out)
	return out
}
