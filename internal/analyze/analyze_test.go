package analyze

import (
	"testing"
	"time"

	"github.com/celanwang/loglens/internal/model"
)

func entry(level, module, traceID, msg string, fields map[string]string) model.Entry {
	return model.Entry{
		Level:   level,
		Module:  module,
		TraceID: traceID,
		Msg:     msg,
		Fields:  fields,
		HasTime: true,
		Time:    time.Date(2026, 8, 10, 10, 0, 0, 0, time.Local),
	}
}

func TestPercentile(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	s := summarize(vals)
	if s.Count != 10 || s.Avg != 5.5 || s.Max != 10 {
		t.Errorf("基本统计错误: %+v", s)
	}
	if s.P50 != 5.5 {
		t.Errorf("P50 期望 5.5，得到 %v", s.P50)
	}
	// rank = 0.95*9 = 8.55 → 9 + 0.55*(10-9) = 9.55
	if s.P95 < 9.54 || s.P95 > 9.56 {
		t.Errorf("P95 期望约 9.55，得到 %v", s.P95)
	}
	if got := summarize(nil); got.Count != 0 {
		t.Errorf("空输入应返回零值: %+v", got)
	}
	if got := summarize([]float64{7}); got.P99 != 7 {
		t.Errorf("单元素 P99 应为自身: %+v", got)
	}
}

func TestParseMS(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want float64
		ok   bool
	}{
		{"108ms", 108, true},
		{"137", 137, true},
		{"1.5ms", 1.5, true},
		{"", 0, false},
		{"abc", 0, false},
		{"-3ms", 0, false},
	} {
		got, ok := parseMS(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseMS(%q) = (%v, %v)，期望 (%v, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestBuildOverviewAndLevels(t *testing.T) {
	entries := []model.Entry{
		entry(model.LevelInfo, "UserService", "t1", "request start", nil),
		entry(model.LevelWarn, "Database", "t1", "slow query", nil),
		entry(model.LevelError, "Gateway", "t1", "request failed", nil),
		{Raw: "garbage", Level: model.LevelParseError, ParseErr: true},
	}
	r := Build(entries, Options{})
	o := r.Overview
	if o.TotalLines != 4 || o.ParsedLines != 3 || o.FailedLines != 1 {
		t.Errorf("概览行数错误: %+v", o)
	}
	if o.LevelCounts[model.LevelInfo] != 1 || o.LevelCounts[model.LevelParseError] != 1 {
		t.Errorf("级别计数错误: %+v", o.LevelCounts)
	}
	if !o.HasTimeRange {
		t.Error("应识别时间范围")
	}
}

func TestBuildModuleStats(t *testing.T) {
	entries := []model.Entry{
		entry(model.LevelInfo, "A", "t1", "a", nil),
		entry(model.LevelInfo, "A", "t1", "b", nil),
		entry(model.LevelError, "A", "t1", "c", nil),
		entry(model.LevelInfo, "B", "t2", "d", nil),
		entry(model.LevelInfo, "", "t2", "no module", nil), // 缺模块 → UNKNOWN
		{Raw: "garbage", Level: model.LevelParseError, ParseErr: true},
	}
	r := Build(entries, Options{})
	if len(r.Modules) != 3 {
		t.Fatalf("期望 3 个模块（含 UNKNOWN），得到 %d: %+v", len(r.Modules), r.Modules)
	}
	a := r.Modules[0]
	if a.Module != "A" || a.Total != 3 || a.Errors != 1 {
		t.Errorf("模块 A 统计错误: %+v", a)
	}
	if a.ErrorRate < 33.3 || a.ErrorRate > 33.4 {
		t.Errorf("模块 A 错误率期望约 33.33%%，得到 %v", a.ErrorRate)
	}
	var unknown *model.ModuleStat
	for i := range r.Modules {
		if r.Modules[i].Module == "UNKNOWN" {
			unknown = &r.Modules[i]
		}
	}
	if unknown == nil || unknown.Total != 1 {
		t.Errorf("缺模块行应归入 UNKNOWN 桶: %+v", r.Modules)
	}
}

func TestBuildLatencyDualMetrics(t *testing.T) {
	entries := []model.Entry{
		entry(model.LevelInfo, "UserService", "t1", "load user", map[string]string{"cost": "100ms"}),
		entry(model.LevelInfo, "UserService", "t1", "request end", map[string]string{"totalCost": "150ms"}),
		entry(model.LevelInfo, "OrderService", "t2", "query order", map[string]string{"cost": "300ms"}),
		entry(model.LevelInfo, "Gateway", "t2", "request end", map[string]string{"totalCost": "450ms"}),
	}
	r := Build(entries, Options{})
	if r.Latency.RequestOverall.Count != 2 {
		t.Fatalf("请求级口径应含 2 个样本: %+v", r.Latency.RequestOverall)
	}
	if r.Latency.RequestOverall.Avg != 300 {
		t.Errorf("请求平均耗时期望 300，得到 %v", r.Latency.RequestOverall.Avg)
	}
	if len(r.Latency.OpByModule) != 2 {
		t.Errorf("操作级口径应按 2 个模块分组: %+v", r.Latency.OpByModule)
	}
	for _, ml := range r.Latency.RequestByModule {
		if ml.Module == "Gateway" && ml.Stat.Avg != 450 {
			t.Errorf("Gateway 请求耗时错误: %+v", ml.Stat)
		}
	}
}
