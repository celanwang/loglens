package parser

import (
	"strings"
	"testing"

	"github.com/celanwang/loglens/internal/model"
)

func TestParseStandardLine(t *testing.T) {
	e := ParseLine("app.log", 1, "2026-08-10 10:00:01.123 INFO  [UserService] [traceId=abc001] request start userId=10001")
	if e.ParseErr {
		t.Fatalf("标准行不应解析失败: %+v", e)
	}
	if !e.HasTime || e.Time.Hour() != 10 || e.Time.Minute() != 0 || e.Time.Second() != 1 {
		t.Errorf("时间戳解析错误: %v", e.Time)
	}
	if e.Level != model.LevelInfo {
		t.Errorf("级别错误: %s", e.Level)
	}
	if e.Module != "UserService" {
		t.Errorf("模块错误: %s", e.Module)
	}
	if e.TraceID != "abc001" {
		t.Errorf("traceId 错误: %s", e.TraceID)
	}
	if e.Fields["userId"] != "10001" {
		t.Errorf("k=v 抽取错误: %v", e.Fields)
	}
	if !strings.Contains(e.Msg, "request start") {
		t.Errorf("消息体错误: %q", e.Msg)
	}
}

func TestParseCostFields(t *testing.T) {
	e := ParseLine("app.log", 2, "2026-08-10 10:00:01.260 INFO  [UserService] [traceId=abc001] request end totalCost=137ms")
	if e.Fields["totalCost"] != "137ms" {
		t.Errorf("totalCost 抽取错误: %v", e.Fields)
	}
}

func TestParseMissingTraceID(t *testing.T) {
	e := ParseLine("app.log", 3, "2026-08-10 10:00:02.100 WARN  [Database] slow query cost=700ms")
	if e.ParseErr {
		t.Fatal("缺 traceId 应尽力解析而非标记为不可解析")
	}
	if e.TraceID != "" || e.Module != "Database" || e.Level != model.LevelWarn {
		t.Errorf("部分字段缺失解析错误: %+v", e)
	}
}

func TestParseMissingLevelAndModule(t *testing.T) {
	e := ParseLine("app.log", 4, "2026-08-10 10:00:03.500 [traceId=abc002] query order failed error=database_timeout")
	if e.ParseErr {
		t.Fatal("缺级别/模块不应标记为不可解析")
	}
	if e.Level != model.LevelUnknown || e.Module != "" || e.TraceID != "abc002" {
		t.Errorf("部分缺失解析错误: %+v", e)
	}
	if e.Fields["error"] != "database_timeout" {
		t.Errorf("error 字段抽取错误: %v", e.Fields)
	}
}

func TestParseLowercaseLevel(t *testing.T) {
	e := ParseLine("app.log", 5, "2026-08-10 10:00:04.000 warn  [Cache] cache miss cost=3ms")
	if e.Level != model.LevelWarn {
		t.Errorf("小写级别应归一化为 WARN: %s", e.Level)
	}
}

func TestParseGarbageLine(t *testing.T) {
	for _, line := range []string{
		"@@@@ corrupted line @@@@",
		"java.lang.NullPointerException",
		"\tat com.example.UserService.load(UserService.java:42)",
		"### broken ###",
	} {
		e := ParseLine("app.log", 6, line)
		if !e.ParseErr || e.Level != model.LevelParseError {
			t.Errorf("垃圾行应标记为 PARSE_ERROR: %q -> %+v", line, e)
		}
		if e.Raw != line {
			t.Errorf("不可解析行应保留原文: %q", e.Raw)
		}
	}
}

func TestParseLevelWithoutTimestamp(t *testing.T) {
	e := ParseLine("app.log", 7, "ERROR [OrderService] query order failed error=database_timeout")
	if e.ParseErr {
		t.Fatal("缺时间戳但有级别应尽力解析")
	}
	if e.Level != model.LevelError || e.Module != "OrderService" {
		t.Errorf("解析错误: %+v", e)
	}
}

func TestParseReaderSkipsBlankLines(t *testing.T) {
	input := "2026-08-10 10:00:01.123 INFO  [UserService] [traceId=abc001] request start userId=10001\n" +
		"\n" +
		"   \n" +
		"2026-08-10 10:00:01.260 INFO  [UserService] [traceId=abc001] request end totalCost=137ms\n"
	var entries []model.Entry
	err := ParseReader("app.log", strings.NewReader(input), func(e model.Entry) { entries = append(entries, e) })
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("空白行应跳过，期望 2 条，得到 %d", len(entries))
	}
	if entries[1].Line != 4 {
		t.Errorf("行号应保留原始行号 4，得到 %d", entries[1].Line)
	}
}
