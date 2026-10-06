// Package report 将分析中间模型渲染为单文件中文 HTML 报告（ECharts 图表走 CDN）。
package report

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"

	"github.com/celanwang/loglens/internal/model"
)

//go:embed template.html
var templateHTML string

// Render 将报告模型渲染为 HTML 写入 w。
func Render(w io.Writer, r *model.Report) error {
	t, err := template.New("report").Funcs(funcMap).Parse(templateHTML)
	if err != nil {
		return fmt.Errorf("解析报告模板失败: %w", err)
	}
	if err := t.Execute(w, newView(r)); err != nil {
		return fmt.Errorf("渲染报告失败: %w", err)
	}
	return nil
}

var funcMap = template.FuncMap{
	"ms":   func(v float64) string { return formatFloat(v, 1) },
	"pct":  func(v float64) string { return formatFloat(v, 2) },
	"join": strings.Join,
	"ftime": func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.Format("2006-01-02 15:04:05")
	},
}

func formatFloat(v float64, prec int) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.*f", prec, v), "0"), ".")
}

type view struct {
	*model.Report
	GeneratedAt   string
	TimeRange     string
	FailRate      string
	LevelSummary  string
	ErrorCount    int
	WarnCount     int
	LevelPieJSON  template.JS
	TrendJSON     template.JS
	ModuleJSON    template.JS
	HasTrend      bool
	HasLatency    bool
	HasModuleBars bool
}

func newView(r *model.Report) *view {
	v := &view{Report: r, GeneratedAt: r.GeneratedAt.Format("2006-01-02 15:04:05")}
	o := r.Overview
	if o.HasTimeRange {
		v.TimeRange = fmt.Sprintf("%s ~ %s", o.StartTime.Format("2006-01-02 15:04:05"), o.EndTime.Format("2006-01-02 15:04:05"))
	} else {
		v.TimeRange = "无法识别（日志缺少时间戳）"
	}
	v.FailRate = "0"
	if o.TotalLines > 0 {
		v.FailRate = formatFloat(float64(o.FailedLines)/float64(o.TotalLines)*100, 2)
	}
	v.ErrorCount = o.LevelCounts[model.LevelError] + o.LevelCounts[model.LevelFatal]
	v.WarnCount = o.LevelCounts[model.LevelWarn]
	var parts []string
	for _, l := range r.Levels {
		if n := o.LevelCounts[l]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s × %d", l, n))
		}
	}
	v.LevelSummary = strings.Join(parts, "，")
	v.LevelPieJSON = mustJSON(buildLevelPie(r))
	v.TrendJSON = mustJSON(buildTrendChart(r))
	v.ModuleJSON = mustJSON(buildModuleChart(r))
	v.HasTrend = len(r.Trend) > 0
	v.HasLatency = r.Latency.RequestOverall.Count > 0
	v.HasModuleBars = len(r.Modules) > 0
	return v
}

func mustJSON(v any) template.JS {
	data, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return template.JS(data)
}

type pieItem struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func buildLevelPie(r *model.Report) []pieItem {
	var items []pieItem
	for _, l := range r.Levels {
		if n := r.Overview.LevelCounts[l]; n > 0 {
			items = append(items, pieItem{Name: l, Value: n})
		}
	}
	return items
}

type trendChart struct {
	Buckets []string         `json:"buckets"`
	Levels  []string         `json:"levels"`
	Series  map[string][]int `json:"series"`
	AvgCost []float64        `json:"avgCost"`
}

func buildTrendChart(r *model.Report) trendChart {
	c := trendChart{Series: map[string][]int{}}
	levelSet := map[string]bool{}
	for _, tp := range r.Trend {
		for l := range tp.LevelCounts {
			levelSet[l] = true
		}
	}
	for _, l := range r.Levels {
		if levelSet[l] {
			c.Levels = append(c.Levels, l)
		}
	}
	for _, tp := range r.Trend {
		c.Buckets = append(c.Buckets, tp.Bucket.Format("01-02 15:04"))
		for _, l := range c.Levels {
			c.Series[l] = append(c.Series[l], tp.LevelCounts[l])
		}
		c.AvgCost = append(c.AvgCost, round1(tp.AvgTotalCost))
	}
	return c
}

type moduleChart struct {
	Modules   []string  `json:"modules"`
	Total     []int     `json:"total"`
	Errors    []int     `json:"errors"`
	ErrorRate []float64 `json:"errorRate"`
}

func buildModuleChart(r *model.Report) moduleChart {
	var c moduleChart
	for _, m := range r.Modules {
		c.Modules = append(c.Modules, m.Module)
		c.Total = append(c.Total, m.Total)
		c.Errors = append(c.Errors, m.Errors)
		c.ErrorRate = append(c.ErrorRate, round1(m.ErrorRate))
	}
	return c
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}
