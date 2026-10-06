package report_test

import (
	"os"
	"strings"
	"testing"

	"github.com/celanwang/loglens/internal/analyze"
	"github.com/celanwang/loglens/internal/model"
	"github.com/celanwang/loglens/internal/parser"
	"github.com/celanwang/loglens/internal/report"
)

// 端到端：解析真实样本日志 → 分析 → 渲染 HTML 报告。
func TestRenderFromSampleLog(t *testing.T) {
	fh, err := os.Open("../../testdata/sample.log")
	if err != nil {
		t.Skipf("测试日志不存在（可运行 go run ./scripts/genlogs 生成）: %v", err)
	}
	defer fh.Close()
	var entries []model.Entry
	if err := parser.ParseReader("sample.log", fh, func(e model.Entry) { entries = append(entries, e) }); err != nil {
		t.Fatal(err)
	}
	rep := analyze.Build(entries, analyze.Options{SlowMs: 500, TopN: 10})

	var sb strings.Builder
	if err := report.Render(&sb, rep); err != nil {
		t.Fatal(err)
	}
	html := sb.String()
	for _, kw := range []string{"智能日志分析报告", "database_timeout", "network_error", "候选根因", "Top 慢请求", "未完成请求"} {
		if !strings.Contains(html, kw) {
			t.Errorf("报告缺少关键内容 %q", kw)
		}
	}
}
