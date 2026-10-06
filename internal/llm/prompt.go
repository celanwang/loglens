package llm

import (
	"fmt"
	"strings"

	"github.com/celanwang/loglens/internal/model"
)

const promptLineCap = 10

// BuildPrompt 将报告中的统计结论压缩为 LLM 诊断提示词。
func BuildPrompt(r *model.Report) string {
	var b strings.Builder
	b.WriteString("以下是一次应用运行日志的统计分析结果，请据此诊断问题。\n\n")

	o := r.Overview
	fmt.Fprintf(&b, "【数据概览】总行数 %d，解析失败 %d 行；时间范围 %s ~ %s。\n",
		o.TotalLines, o.FailedLines,
		o.StartTime.Format("01-02 15:04:05"), o.EndTime.Format("01-02 15:04:05"))
	var levels []string
	for _, l := range model.LevelOrder {
		if n := o.LevelCounts[l]; n > 0 {
			levels = append(levels, fmt.Sprintf("%s=%d", l, n))
		}
	}
	fmt.Fprintf(&b, "级别分布: %s。\n", strings.Join(levels, ", "))

	if lat := r.Latency.RequestOverall; lat.Count > 0 {
		fmt.Fprintf(&b, "【请求耗时】样本 %d，平均 %.0fms，P95 %.0fms，P99 %.0fms，最大 %.0fms（慢请求阈值 %.0fms）。\n",
			lat.Count, lat.Avg, lat.P95, lat.P99, lat.Max, r.SlowThresholdMs)
	}

	if len(r.Errors) > 0 {
		b.WriteString("【Top 错误模式】\n")
		for i, g := range r.Errors {
			if i >= promptLineCap {
				break
			}
			fmt.Fprintf(&b, "- %s：%d 次，涉及模块 [%s]，影响 %d 条请求链路。\n",
				g.Key, g.Count, strings.Join(g.Modules, ","), g.TraceCount)
		}
	}

	if len(r.SlowTraces) > 0 {
		b.WriteString("【Top 慢请求】\n")
		for i, t := range r.SlowTraces {
			if i >= promptLineCap {
				break
			}
			fmt.Fprintf(&b, "- traceId=%s 总耗时 %.0fms，链路 [%s]，含错误=%v。\n",
				t.TraceID, t.TotalCost, strings.Join(t.Modules, "->"), t.HasError)
		}
	}

	if r.UnfinishedTotal > 0 {
		fmt.Fprintf(&b, "【未完成请求】共 %d 条链路有 request start 但无 request end（疑似中断/超时）。\n", r.UnfinishedTotal)
	}

	if len(r.RootCauses) > 0 {
		b.WriteString("【候选根因（规则推断：故障链路中首个异常点）】\n")
		for _, rc := range r.RootCauses {
			fmt.Fprintf(&b, "- 模块 %s 的「%s」，作为首异常点出现于 %d 条故障链路。\n", rc.Module, rc.Candidate, rc.Hits)
		}
	}

	b.WriteString("\n请输出：\n")
	b.WriteString("1. 3~6 条问题诊断，按严重程度从高到低排列，每条包含【现象】【可能原因】【排查建议】；\n")
	b.WriteString("2. 一段总体结论（系统当前最主要的风险是什么、优先处理什么）。\n")
	b.WriteString("使用中文 Markdown 列表，语言精炼，不要复述原始统计数据。\n")
	return b.String()
}
