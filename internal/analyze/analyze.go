// Package analyze 在解析后的日志条目上执行统计分析，产出统一的报告中间模型。
package analyze

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/celanwang/loglens/internal/model"
)

// Options 分析选项。
type Options struct {
	SlowMs float64 // 慢请求阈值（totalCost 口径）
	TopN   int     // 慢请求/错误归组展示上限
}

func (o Options) withDefaults() Options {
	if o.SlowMs <= 0 {
		o.SlowMs = 500
	}
	if o.TopN <= 0 {
		o.TopN = 10
	}
	return o
}

// Build 对全部日志条目执行全量分析。
func Build(entries []model.Entry, opts Options) *model.Report {
	opts = opts.withDefaults()
	r := &model.Report{
		GeneratedAt:     time.Now(),
		SlowThresholdMs: opts.SlowMs,
		Levels:          model.LevelOrder,
	}
	buildOverview(entries, r)
	buildModuleStats(entries, r)
	buildLatency(entries, r)
	return r
}

func buildOverview(entries []model.Entry, r *model.Report) {
	fileSet := map[string]bool{}
	counts := map[string]int{}
	var start, end time.Time
	parsed, failed := 0, 0
	for _, e := range entries {
		fileSet[e.File] = true
		counts[e.Level]++
		if e.ParseErr {
			failed++
		} else {
			parsed++
		}
		if e.HasTime {
			if start.IsZero() || e.Time.Before(start) {
				start = e.Time
			}
			if e.Time.After(end) {
				end = e.Time
			}
		}
	}
	files := make([]string, 0, len(fileSet))
	for f := range fileSet {
		files = append(files, f)
	}
	sort.Strings(files)
	r.Overview = model.Overview{
		Files:        files,
		TotalLines:   len(entries),
		ParsedLines:  parsed,
		FailedLines:  failed,
		StartTime:    start,
		EndTime:      end,
		HasTimeRange: !start.IsZero(),
		LevelCounts:  counts,
	}
}

func buildModuleStats(entries []model.Entry, r *model.Report) {
	type agg struct{ total, errs int }
	m := map[string]*agg{}
	for _, e := range entries {
		if e.ParseErr {
			continue
		}
		mod := e.Module
		if mod == "" {
			mod = "UNKNOWN"
		}
		a := m[mod]
		if a == nil {
			a = &agg{}
			m[mod] = a
		}
		a.total++
		if isErrorLevel(e.Level) {
			a.errs++
		}
	}
	list := make([]model.ModuleStat, 0, len(m))
	for mod, a := range m {
		rate := 0.0
		if a.total > 0 {
			rate = float64(a.errs) / float64(a.total) * 100
		}
		list = append(list, model.ModuleStat{Module: mod, Total: a.total, Errors: a.errs, ErrorRate: rate})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Errors != list[j].Errors {
			return list[i].Errors > list[j].Errors
		}
		return list[i].Total > list[j].Total
	})
	r.Modules = list
}

func buildLatency(entries []model.Entry, r *model.Report) {
	var reqAll []float64
	reqByMod := map[string][]float64{}
	opByMod := map[string][]float64{}
	for _, e := range entries {
		mod := e.Module
		if mod == "" {
			mod = "UNKNOWN"
		}
		if v, ok := parseMS(e.Fields["totalCost"]); ok {
			reqAll = append(reqAll, v)
			reqByMod[mod] = append(reqByMod[mod], v)
		}
		if v, ok := parseMS(e.Fields["cost"]); ok {
			opByMod[mod] = append(opByMod[mod], v)
		}
	}
	r.Latency.RequestOverall = summarize(reqAll)
	r.Latency.RequestByModule = summarizeByModule(reqByMod)
	r.Latency.OpByModule = summarizeByModule(opByMod)
}

func summarizeByModule(m map[string][]float64) []model.ModuleLatency {
	out := make([]model.ModuleLatency, 0, len(m))
	for mod, vals := range m {
		out = append(out, model.ModuleLatency{Module: mod, Stat: summarize(vals)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Stat.P95 != out[j].Stat.P95 {
			return out[i].Stat.P95 > out[j].Stat.P95
		}
		return out[i].Module < out[j].Module
	})
	return out
}

func summarize(vals []float64) model.Percentile {
	if len(vals) == 0 {
		return model.Percentile{}
	}
	sort.Float64s(vals)
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return model.Percentile{
		Count: len(vals),
		Avg:   sum / float64(len(vals)),
		P50:   percentile(vals, 50),
		P95:   percentile(vals, 95),
		P99:   percentile(vals, 99),
		Max:   vals[len(vals)-1],
	}
}

// percentile 线性插值百分位，输入必须已升序排序。
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	rank := p / 100 * float64(n-1)
	lo := int(rank)
	hi := lo + 1
	if hi >= n {
		return sorted[n-1]
	}
	return sorted[lo] + (sorted[hi]-sorted[lo])*(rank-float64(lo))
}

func parseMS(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "ms")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

func isErrorLevel(level string) bool {
	return level == model.LevelError || level == model.LevelFatal
}
