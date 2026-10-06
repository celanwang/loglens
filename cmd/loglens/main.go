// loglens —— 智能日志分析工具命令行入口。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/celanwang/loglens/internal/analyze"
	"github.com/celanwang/loglens/internal/llm"
	"github.com/celanwang/loglens/internal/model"
	"github.com/celanwang/loglens/internal/parser"
	"github.com/celanwang/loglens/internal/report"
)

const usageText = `loglens —— 智能日志分析工具

用法:
  loglens analyze [选项] <日志文件...>              分析日志并生成 HTML 报告
  loglens trace   [选项] <traceId> <日志文件...>    还原指定请求的完整日志链路

analyze 选项:
  -o string      报告输出路径 (默认 "loglens-report.html")
  -slow-ms float 慢请求阈值，单位毫秒 (默认 500)
  -top int       Top N 慢请求/错误展示条数 (默认 10)
  -llm           启用 LLM 智能诊断 (默认 true，需在 .env 配置 DASHSCOPE_API_KEY)
  -env string    .env 文件路径 (默认 ".env")

示例:
  loglens analyze -o report.html logs/app1.log logs/app2.log
  loglens trace abc001 logs/app1.log
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "analyze":
		runAnalyze(os.Args[2:])
	case "trace":
		runTrace(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usageText)
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n\n%s", os.Args[1], usageText)
		os.Exit(2)
	}
}

func runAnalyze(args []string) {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)
	out := fs.String("o", "loglens-report.html", "报告输出路径")
	slowMs := fs.Float64("slow-ms", 500, "慢请求阈值（毫秒）")
	top := fs.Int("top", 10, "Top N 展示条数")
	useLLM := fs.Bool("llm", true, "是否启用 LLM 智能诊断")
	envPath := fs.String("env", ".env", ".env 文件路径")
	_ = fs.Parse(args)

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "错误: 请指定至少一个日志文件\n\n"+usageText)
		os.Exit(2)
	}
	entries, err := loadEntries(files)
	if err != nil {
		fatal(err)
	}

	rep := analyze.Build(entries, analyze.Options{SlowMs: *slowMs, TopN: *top})
	applyLLM(rep, *envPath, *useLLM)

	fh, err := os.Create(*out)
	if err != nil {
		fatal(fmt.Errorf("创建报告文件失败: %w", err))
	}
	defer fh.Close()
	if err := report.Render(fh, rep); err != nil {
		fatal(err)
	}

	o := rep.Overview
	fmt.Printf("分析完成：%d 行日志（%d 解析失败），%d 条链路，ERROR/FATAL %d 条，未完成请求 %d 条\n",
		o.TotalLines, o.FailedLines, o.TraceCount,
		o.LevelCounts[model.LevelError]+o.LevelCounts[model.LevelFatal], rep.UnfinishedTotal)
	if lat := rep.Latency.RequestOverall; lat.Count > 0 {
		fmt.Printf("请求耗时：平均 %.0fms，P95 %.0fms，P99 %.0fms\n", lat.Avg, lat.P95, lat.P99)
	}
	if rep.LLMSummary != "" {
		fmt.Println("LLM 诊断已生成。")
	} else if rep.LLMNote != "" {
		fmt.Println("提示：" + rep.LLMNote)
	}
	fmt.Printf("报告已生成: %s\n", *out)
}

func applyLLM(rep *model.Report, envPath string, enabled bool) {
	if !enabled {
		rep.LLMNote = "未启用 LLM（-llm=false）。"
		return
	}
	cfg := llm.LoadConfig(envPath)
	switch {
	case !cfg.Enabled:
		rep.LLMNote = "LLM 已禁用（LLM_ENABLED=false）。"
	case !cfg.Available():
		rep.LLMNote = "未配置有效的 DASHSCOPE_API_KEY，LLM 诊断未启用。"
	default:
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout+5*time.Second)
		defer cancel()
		summary, err := llm.NewClient(cfg).Summarize(ctx, llm.BuildPrompt(rep))
		if err != nil {
			rep.LLMNote = "LLM 调用失败，已降级为规则归纳（" + err.Error() + "）。"
		} else {
			rep.LLMSummary = summary
		}
	}
}

func runTrace(args []string) {
	fs := flag.NewFlagSet("trace", flag.ExitOnError)
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) < 2 {
		fmt.Fprintln(os.Stderr, "错误: 用法 loglens trace <traceId> <日志文件...>")
		os.Exit(2)
	}
	traceID, files := rest[0], rest[1:]
	entries, err := loadEntries(files)
	if err != nil {
		fatal(err)
	}
	chain := analyze.TraceChain(entries, traceID)
	if len(chain) == 0 {
		fmt.Fprintf(os.Stderr, "未找到 traceId=%s 的日志\n", traceID)
		if sug := analyze.SuggestTraces(entries, traceID, 5); len(sug) > 0 {
			fmt.Fprintf(os.Stderr, "相似 traceId: %s\n", strings.Join(sug, ", "))
		}
		os.Exit(1)
	}
	printChain(traceID, chain)
}

func printChain(traceID string, chain []model.Entry) {
	var base time.Time
	var modules []string
	seen := map[string]bool{}
	var total float64
	hasTotal, hasError, finished := false, false, false
	for _, e := range chain {
		if e.HasTime && base.IsZero() {
			base = e.Time
		}
		if e.Module != "" && !seen[e.Module] {
			seen[e.Module] = true
			modules = append(modules, e.Module)
		}
		if v, ok := parseTotalCost(e); ok && (!hasTotal || v > total) {
			total, hasTotal = v, true
		}
		if e.Level == model.LevelError || e.Level == model.LevelFatal {
			hasError = true
		}
		if strings.Contains(e.Msg, "request end") || hasTotal {
			finished = true
		}
	}

	status := "未完成（有开始无结束，疑似中断或超时）"
	switch {
	case hasError:
		status = "失败（含 ERROR）"
	case finished:
		status = "成功"
	}
	fmt.Printf("链路还原: traceId=%s  共 %d 条日志\n", traceID, len(chain))
	fmt.Printf("模块链路: %s\n", strings.Join(modules, " → "))
	if hasTotal {
		fmt.Printf("总耗时: %.0fms\n", total)
	}
	fmt.Printf("状态: %s\n\n", status)

	for _, e := range chain {
		ts := "(--:--:--.---)"
		offset := "(+       -)"
		if e.HasTime {
			ts = e.Time.Format("15:04:05.000")
			offset = fmt.Sprintf("(+%8.3fs)", e.Time.Sub(base).Seconds())
		}
		fmt.Printf("%s %s %-5s [%s] %s\n", ts, offset, e.Level, moduleOrDash(e), e.Msg)
	}
}

func parseTotalCost(e model.Entry) (float64, bool) {
	s := strings.TrimSuffix(e.Fields["totalCost"], "ms")
	if s == "" {
		return 0, false
	}
	var v float64
	if _, err := fmt.Sscanf(s, "%g", &v); err != nil {
		return 0, false
	}
	return v, true
}

func moduleOrDash(e model.Entry) string {
	if e.Module == "" {
		return "-"
	}
	return e.Module
}

func loadEntries(files []string) ([]model.Entry, error) {
	var entries []model.Entry
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			return nil, fmt.Errorf("打开日志文件 %s 失败: %w", f, err)
		}
		err = parser.ParseReader(f, fh, func(e model.Entry) { entries = append(entries, e) })
		_ = fh.Close()
		if err != nil {
			return nil, fmt.Errorf("读取日志文件 %s 失败: %w", f, err)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("未读取到任何有效日志行")
	}
	return entries, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(1)
}
