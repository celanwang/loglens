// Package parser 将日志文本行解析为结构化 Entry，并对格式异常提供三级容错：
// 完整匹配、部分字段缺失（尽力解析并置空缺失字段）、完全不可解析（保留原文并计数）。
package parser

import (
	"bufio"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/celanwang/loglens/internal/model"
)

var (
	tsRe      = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2}:\d{2})(?:[.,](\d{1,6}))?\s*`)
	wordRe    = regexp.MustCompile(`^([A-Za-z]+)\b\s*`)
	bracketRe = regexp.MustCompile(`\[([^\[\]]+)\]`)
	kvRe      = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)=([^\s]+)`)
)

const (
	layoutWithFrac = "2006-01-02 15:04:05.999999"
	layoutPlain    = "2006-01-02 15:04:05"
)

// ParseLine 解析单行日志，绝不失败：无法识别的字段置空并在 Entry 上留标记。
func ParseLine(file string, n int, line string) model.Entry {
	e := model.Entry{Raw: line, File: file, Line: n, Level: model.LevelUnknown}
	work := strings.TrimSpace(line)

	if m := tsRe.FindStringSubmatch(work); m != nil {
		layout, ts := layoutPlain, m[1]+" "+m[2]
		if m[3] != "" {
			layout, ts = layoutWithFrac, ts+"."+m[3]
		}
		if t, err := time.ParseInLocation(layout, ts, time.Local); err == nil {
			e.Time, e.HasTime = t, true
			work = strings.TrimSpace(work[len(m[0]):])
		}
	}

	if m := wordRe.FindStringSubmatch(work); m != nil {
		if lvl, ok := normalizeLevel(m[1]); ok {
			e.Level = lvl
			work = strings.TrimSpace(work[len(m[0]):])
		}
	}

	for _, b := range bracketRe.FindAllStringSubmatch(work, -1) {
		inner := b[1]
		if tid, ok := strings.CutPrefix(inner, "traceId="); ok {
			e.TraceID = tid
		} else if e.Module == "" {
			e.Module = inner
		}
	}

	msg := bracketRe.ReplaceAllString(work, " ")
	e.Msg = strings.Join(strings.Fields(msg), " ")
	e.Fields = extractFields(e.Msg)

	if e.TraceID == "" {
		e.TraceID = e.Fields["traceId"]
	}

	if !e.HasTime && e.Level == model.LevelUnknown && e.Module == "" && e.TraceID == "" {
		e.ParseErr = true
		e.Level = model.LevelParseError
	}
	return e
}

// ParseReader 流式逐行解析，空白行跳过，每个有效行回调一次。
func ParseReader(file string, r io.Reader, fn func(model.Entry)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		fn(ParseLine(file, n, sc.Text()))
	}
	return sc.Err()
}

func normalizeLevel(s string) (string, bool) {
	switch strings.ToUpper(s) {
	case "TRACE":
		return model.LevelTrace, true
	case "DEBUG":
		return model.LevelDebug, true
	case "INFO":
		return model.LevelInfo, true
	case "WARN", "WARNING":
		return model.LevelWarn, true
	case "ERROR":
		return model.LevelError, true
	case "FATAL":
		return model.LevelFatal, true
	}
	return "", false
}

func extractFields(msg string) map[string]string {
	var fields map[string]string
	for _, m := range kvRe.FindAllStringSubmatch(msg, -1) {
		if fields == nil {
			fields = map[string]string{}
		}
		fields[m[1]] = m[2]
	}
	return fields
}
