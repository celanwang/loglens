package report

import (
	"strings"
	"testing"
	"time"

	"github.com/celanwang/loglens/internal/model"
)

func sampleReport() *model.Report {
	return &model.Report{
		GeneratedAt: time.Date(2026, 8, 10, 12, 0, 0, 0, time.Local),
		Levels:      model.LevelOrder,
		Overview: model.Overview{
			Files: []string{"app.log"}, TotalLines: 100, ParsedLines: 98, FailedLines: 2,
			TraceCount: 10, HasTimeRange: true,
			StartTime:   time.Date(2026, 8, 10, 10, 0, 0, 0, time.Local),
			EndTime:     time.Date(2026, 8, 10, 10, 30, 0, 0, time.Local),
			LevelCounts: map[string]int{"INFO": 90, "WARN": 5, "ERROR": 3, "PARSE_ERROR": 2},
		},
		Modules: []model.ModuleStat{
			{Module: "Gateway", Total: 50, Errors: 3, ErrorRate: 6},
			{Module: "Database", Total: 30, Errors: 0},
		},
		Latency: model.LatencyStat{
			RequestOverall:  model.Percentile{Count: 10, Avg: 120.5, P95: 500, P99: 900, Max: 1200},
			RequestByModule: []model.ModuleLatency{{Module: "Gateway", Stat: model.Percentile{Count: 10, Avg: 120.5, P95: 500, P99: 900}}},
			OpByModule:      []model.ModuleLatency{{Module: "Database", Stat: model.Percentile{Count: 20, Avg: 40, P95: 700, P99: 750}}},
		},
		Errors: []model.ErrorGroup{
			{Key: "database_timeout", Count: 3, Modules: []string{"OrderService"}, TraceCount: 2, SampleTraces: []string{"t1"}, Sample: "ERROR sample"},
		},
		SlowTraces: []model.TraceSummary{
			{TraceID: "t1", TotalCost: 1410, Modules: []string{"Gateway", "Database"}, HasError: true, Start: time.Date(2026, 8, 10, 10, 1, 0, 0, time.Local)},
		},
		Unfinished:      []model.TraceSummary{{TraceID: "t2", Modules: []string{"Gateway"}}},
		UnfinishedTotal: 1,
		Trend: []model.TrendPoint{
			{Bucket: time.Date(2026, 8, 10, 10, 0, 0, 0, time.Local), LevelCounts: map[string]int{"INFO": 50, "ERROR": 1}, RequestCount: 5, AvgTotalCost: 100},
			{Bucket: time.Date(2026, 8, 10, 10, 1, 0, 0, time.Local), LevelCounts: map[string]int{"INFO": 40, "ERROR": 2}, RequestCount: 5, AvgTotalCost: 300},
		},
		RootCauses:      []model.RootCause{{Module: "Database", Candidate: "slow query", Hits: 2, SampleTraces: []string{"t1"}}},
		SlowThresholdMs: 500,
		LLMSummary:      "1. 数据库超时是主因\n2. 建议增加连接池容量",
	}
}

func TestRenderContainsKeySections(t *testing.T) {
	var sb strings.Builder
	if err := Render(&sb, sampleReport()); err != nil {
		t.Fatal(err)
	}
	html := sb.String()
	for _, kw := range []string{
		"智能日志分析报告", "数据概览", "LLM 智能诊断", "数据库超时是主因",
		"模块统计", "Gateway", "请求耗时分析", "P95",
		"错误归纳", "database_timeout", "Top 慢请求", "t1", "1410",
		"未完成请求", "候选根因", "slow query",
		"levelPie", "moduleBar", "trendLine",
	} {
		if !strings.Contains(html, kw) {
			t.Errorf("报告缺少关键内容 %q", kw)
		}
	}
}

func TestRenderFallbackNoteWhenLLMOff(t *testing.T) {
	r := sampleReport()
	r.LLMSummary = ""
	r.LLMNote = "未配置 DASHSCOPE_API_KEY，LLM 诊断未启用。"
	var sb strings.Builder
	if err := Render(&sb, r); err != nil {
		t.Fatal(err)
	}
	html := sb.String()
	if !strings.Contains(html, "规则归纳") || !strings.Contains(html, "LLM 诊断未启用") {
		t.Error("LLM 降级时应展示规则归纳与降级说明")
	}
	if strings.Contains(html, "LLM 智能诊断") {
		t.Error("LLM 未启用时不应渲染 LLM 诊断区块")
	}
}

func TestRenderEmptyReport(t *testing.T) {
	var sb strings.Builder
	r := &model.Report{Levels: model.LevelOrder, Overview: model.Overview{LevelCounts: map[string]int{}}}
	if err := Render(&sb, r); err != nil {
		t.Fatalf("空报告也应正常渲染: %v", err)
	}
	if !strings.Contains(sb.String(), "智能日志分析报告") {
		t.Error("空报告缺少标题")
	}
}
