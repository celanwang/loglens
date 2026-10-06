package analyze

import (
	"sort"
	"time"

	"github.com/celanwang/loglens/internal/model"
)

// buildTrend 按分钟级时间桶统计各级别日志量与请求平均耗时，刻画时间趋势。
func buildTrend(entries []model.Entry, r *model.Report) {
	type agg struct {
		counts map[string]int
		reqN   int
		reqSum float64
	}
	buckets := map[time.Time]*agg{}
	for _, e := range entries {
		if !e.HasTime {
			continue
		}
		b := e.Time.Truncate(time.Minute)
		a := buckets[b]
		if a == nil {
			a = &agg{counts: map[string]int{}}
			buckets[b] = a
		}
		a.counts[e.Level]++
		if v, ok := parseMS(e.Fields["totalCost"]); ok {
			a.reqN++
			a.reqSum += v
		}
	}
	keys := make([]time.Time, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })
	for _, k := range keys {
		a := buckets[k]
		tp := model.TrendPoint{Bucket: k, LevelCounts: a.counts, RequestCount: a.reqN}
		if a.reqN > 0 {
			tp.AvgTotalCost = a.reqSum / float64(a.reqN)
		}
		r.Trend = append(r.Trend, tp)
	}
}
