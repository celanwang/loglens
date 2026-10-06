package analyze_test

import (
	"os"
	"testing"

	"github.com/celanwang/loglens/internal/analyze"
	"github.com/celanwang/loglens/internal/model"
	"github.com/celanwang/loglens/internal/parser"
)

// 对仓库内生成的测试日志（scripts/genlogs 产出）做端到端分析校验。
func loadSample(t *testing.T) []model.Entry {
	t.Helper()
	fh, err := os.Open("../../testdata/sample.log")
	if err != nil {
		t.Skipf("测试日志不存在（可运行 go run ./scripts/genlogs 生成）: %v", err)
	}
	defer fh.Close()
	var entries []model.Entry
	if err := parser.ParseReader("sample.log", fh, func(e model.Entry) { entries = append(entries, e) }); err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestEndToEndOnSampleLog(t *testing.T) {
	entries := loadSample(t)
	r := analyze.Build(entries, analyze.Options{SlowMs: 500, TopN: 10})
	o := r.Overview

	if o.TotalLines < 5000 {
		t.Errorf("样本日志行数应符合 PRD 规模要求: %d", o.TotalLines)
	}
	if o.TraceCount < 100 {
		t.Errorf("traceId 数量应 >= 100: %d", o.TraceCount)
	}
	if o.FailedLines == 0 {
		t.Error("样本包含乱格式行，解析失败数应大于 0")
	}
	if float64(o.FailedLines)/float64(o.TotalLines) > 0.05 {
		t.Errorf("解析失败率应低于 5%%: %d/%d", o.FailedLines, o.TotalLines)
	}

	errKeys := map[string]bool{}
	for _, g := range r.Errors {
		errKeys[g.Key] = true
	}
	for _, want := range []string{"database_timeout", "network_error"} {
		if !errKeys[want] {
			t.Errorf("错误归纳应包含预埋故障 %q，实际: %v", want, errKeys)
		}
	}

	lat := r.Latency.RequestOverall
	if lat.Count == 0 || !(lat.Avg < lat.P95 && lat.P95 <= lat.P99 && lat.P99 <= lat.Max) {
		t.Errorf("百分位关系应满足 Avg<P95<=P99<=Max: %+v", lat)
	}

	if len(r.SlowTraces) == 0 {
		t.Error("应识别出慢请求")
	}
	if r.UnfinishedTotal == 0 {
		t.Error("应识别出未完成请求")
	}
	if len(r.Trend) == 0 {
		t.Error("应产出时间趋势")
	}
	if len(r.RootCauses) == 0 || r.RootCauses[0].Module != "Database" {
		t.Errorf("数据库故障窗口的头号候选根因应为 Database: %+v", r.RootCauses)
	}

	// 多文件合并场景：同一文件传入两次，trace 数不变但日志数翻倍
	r2 := analyze.Build(append(entries, entries...), analyze.Options{})
	if r2.Overview.TotalLines != o.TotalLines*2 {
		t.Errorf("合并后总行数应翻倍: %d", r2.Overview.TotalLines)
	}
}

func BenchmarkAnalyzeSample(b *testing.B) {
	fh, err := os.Open("../../testdata/sample.log")
	if err != nil {
		b.Skip(err)
	}
	defer fh.Close()
	var entries []model.Entry
	_ = parser.ParseReader("sample.log", fh, func(e model.Entry) { entries = append(entries, e) })
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		analyze.Build(entries, analyze.Options{SlowMs: 500, TopN: 10})
	}
}
