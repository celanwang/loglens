// Package model 定义日志条目与分析报告的核心数据结构。
package model

import "time"

// 日志级别常量。PARSE_ERROR 表示完全不可解析的行，UNKNOWN 表示可解析但级别缺失或无法识别。
const (
	LevelTrace      = "TRACE"
	LevelDebug      = "DEBUG"
	LevelInfo       = "INFO"
	LevelWarn       = "WARN"
	LevelError      = "ERROR"
	LevelFatal      = "FATAL"
	LevelUnknown    = "UNKNOWN"
	LevelParseError = "PARSE_ERROR"
)

// LevelOrder 是报告中级别展示的固定顺序。
var LevelOrder = []string{
	LevelTrace, LevelDebug, LevelInfo, LevelWarn, LevelError, LevelFatal, LevelUnknown, LevelParseError,
}

// Entry 是单条日志的结构化表示。
type Entry struct {
	Raw      string            // 原文
	File     string            // 来源文件
	Line     int               // 文件内行号
	Time     time.Time         // 日志时间戳
	HasTime  bool              // 时间戳是否解析成功
	Level    string            // 日志级别（见级别常量）
	Module   string            // 模块名，缺失为空串
	TraceID  string            // 请求链路 ID，缺失为空串
	Msg      string            // 消息体（已剥离时间戳/级别/方括号段）
	Fields   map[string]string // 消息体内的 k=v 键值对
	ParseErr bool              // 是否完全不可解析
}

// Overview 报告概览。
type Overview struct {
	Files        []string
	TotalLines   int
	ParsedLines  int
	FailedLines  int
	TraceCount   int
	StartTime    time.Time
	EndTime      time.Time
	HasTimeRange bool
	LevelCounts  map[string]int
}

// ModuleStat 模块维度统计。
type ModuleStat struct {
	Module    string
	Total     int
	Errors    int
	ErrorRate float64 // 百分比 0-100
}

// Percentile 一组耗时分值的统计摘要（单位 ms）。
type Percentile struct {
	Count int
	Avg   float64
	P50   float64
	P95   float64
	P99   float64
	Max   float64
}

// ModuleLatency 模块维度耗时统计。
type ModuleLatency struct {
	Module string
	Stat   Percentile
}

// LatencyStat 耗时统计：totalCost 为请求级口径，cost 为操作级口径。
type LatencyStat struct {
	RequestOverall  Percentile
	RequestByModule []ModuleLatency
	OpByModule      []ModuleLatency
}

// ErrorGroup 归组后的错误模式。
type ErrorGroup struct {
	Key          string   // error= 字段值或归一化消息模板
	Count        int
	Modules      []string
	TraceCount   int
	SampleTraces []string
	Sample       string // 首条命中日志原文
	First        time.Time
	Last         time.Time
}

// TraceSummary 单条请求链路的摘要。
type TraceSummary struct {
	TraceID   string
	TotalCost float64
	Modules   []string // 按时间序去重后的模块链路
	HasError  bool
	Start     time.Time
	End       time.Time
	Finished  bool
}

// TrendPoint 一个时间桶内的统计。
type TrendPoint struct {
	Bucket       time.Time
	LevelCounts  map[string]int
	RequestCount int
	AvgTotalCost float64
}

// RootCause 候选根因：异常请求链路中首个异常事件的归组。
type RootCause struct {
	Candidate    string
	Module       string
	Hits         int // 作为首个异常点出现的 trace 数
	SampleTraces []string
}

// Report 是分析与渲染之间的统一中间模型。
type Report struct {
	GeneratedAt     time.Time
	Overview        Overview
	Levels          []string
	Modules         []ModuleStat
	Latency         LatencyStat
	Errors          []ErrorGroup
	SlowTraces      []TraceSummary
	Unfinished      []TraceSummary
	UnfinishedTotal int
	Trend           []TrendPoint
	RootCauses      []RootCause
	SlowThresholdMs float64
	LLMSummary      string // LLM 生成的诊断；为空表示未启用或已降级
	LLMNote         string // LLM 未启用/降级原因说明
}
