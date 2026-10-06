package analyze

import (
	"fmt"
	"testing"
	"time"

	"github.com/celanwang/loglens/internal/model"
)

// traceEntries 构造一条完整请求链路与若干异常链路，供 trace 相关测试共用。
func traceEntries() []model.Entry {
	base := time.Date(2026, 8, 10, 10, 0, 0, 0, time.Local)
	at := func(sec int) time.Time { return base.Add(time.Duration(sec) * time.Second) }
	mk := func(sec int, level, module, tid, msg string, fields map[string]string) model.Entry {
		return model.Entry{Level: level, Module: module, TraceID: tid, Msg: msg, Fields: fields, HasTime: true, Time: at(sec)}
	}
	return []model.Entry{
		// t-ok：正常请求，总耗时 200ms
		mk(0, "INFO", "Gateway", "t-ok", "request start userId=1", nil),
		mk(1, "INFO", "UserService", "t-ok", "load user success", map[string]string{"cost": "100ms"}),
		mk(2, "INFO", "Gateway", "t-ok", "request end", map[string]string{"totalCost": "200ms"}),
		// t-slow：慢请求，总耗时 900ms
		mk(3, "INFO", "Gateway", "t-slow", "request start userId=2", nil),
		mk(4, "WARN", "Database", "t-slow", "slow query", map[string]string{"cost": "700ms"}),
		mk(5, "INFO", "Gateway", "t-slow", "request end", map[string]string{"totalCost": "900ms"}),
		// t-err：数据库超时故障链
		mk(6, "INFO", "Gateway", "t-err", "request start userId=3", nil),
		mk(7, "WARN", "Database", "t-err", "slow query", map[string]string{"cost": "700ms"}),
		mk(8, "ERROR", "OrderService", "t-err", "query order failed", map[string]string{"error": "database_timeout"}),
		mk(9, "ERROR", "Gateway", "t-err", "request failed status=500", map[string]string{"totalCost": "1410ms"}),
		// t-lost：未完成请求
		mk(10, "INFO", "Gateway", "t-lost", "request start userId=4", nil),
		mk(11, "INFO", "AuthService", "t-lost", "verify token", map[string]string{"cost": "10ms"}),
	}
}

func TestBuildSlowAndUnfinished(t *testing.T) {
	r := Build(traceEntries(), Options{SlowMs: 500, TopN: 10})
	if len(r.SlowTraces) != 2 {
		t.Fatalf("期望 2 个慢请求（t-slow/t-err），得到 %+v", r.SlowTraces)
	}
	if r.SlowTraces[0].TraceID != "t-err" || r.SlowTraces[0].TotalCost != 1410 {
		t.Errorf("慢请求应按耗时降序: %+v", r.SlowTraces)
	}
	if r.SlowTraces[1].TraceID != "t-slow" || r.SlowTraces[1].HasError {
		t.Errorf("t-slow 仅 WARN 不应标记为含错误: %+v", r.SlowTraces[1])
	}
	if r.UnfinishedTotal != 1 || len(r.Unfinished) != 1 || r.Unfinished[0].TraceID != "t-lost" {
		t.Errorf("未完成请求识别错误: total=%d %+v", r.UnfinishedTotal, r.Unfinished)
	}
}

func TestTraceChainOrdering(t *testing.T) {
	entries := traceEntries()
	// 打乱输入顺序，验证链路还原按时间重排
	shuffled := []model.Entry{entries[9], entries[6], entries[8], entries[7]}
	chain := TraceChain(shuffled, "t-err")
	if len(chain) != 4 {
		t.Fatalf("链路长度错误: %d", len(chain))
	}
	for i := 1; i < len(chain); i++ {
		if chain[i].Time.Before(chain[i-1].Time) {
			t.Errorf("链路未按时间升序: %v < %v", chain[i].Time, chain[i-1].Time)
		}
	}
	if chain[3].Level != model.LevelError || chain[3].Module != "Gateway" {
		t.Errorf("链路末尾应为 Gateway 500: %+v", chain[3])
	}
}

func TestSuggestTraces(t *testing.T) {
	sug := SuggestTraces(traceEntries(), "t-s", 5)
	if len(sug) != 1 || sug[0] != "t-slow" {
		t.Errorf("前缀建议错误: %v", sug)
	}
}

func TestBuildErrorGroups(t *testing.T) {
	entries := traceEntries()
	// 追加两条同类错误（仅 ID 不同），验证归一化收敛
	base := time.Date(2026, 8, 10, 10, 1, 0, 0, time.Local)
	for i, tid := range []string{"t-e2", "t-e3"} {
		entries = append(entries, model.Entry{
			Level: model.LevelError, Module: "PaymentService", TraceID: tid, HasTime: true,
			Time: base.Add(time.Duration(i) * time.Second),
			Msg:  fmt.Sprintf("charge failed order %d", 20001+i),
		})
	}
	r := Build(entries, Options{})
	var dbTimeout, charge *model.ErrorGroup
	for i := range r.Errors {
		switch r.Errors[i].Key {
		case "database_timeout":
			dbTimeout = &r.Errors[i]
		case "charge failed order *":
			charge = &r.Errors[i]
		}
	}
	if dbTimeout == nil || dbTimeout.Count != 1 || dbTimeout.TraceCount != 1 {
		t.Errorf("error= 字段应作为归组键: %+v", r.Errors)
	}
	if charge == nil || charge.Count != 2 || charge.TraceCount != 2 {
		t.Fatalf("相似错误应归一化聚类: %+v", charge)
	}
	if len(charge.Modules) != 1 || charge.Modules[0] != "PaymentService" {
		t.Errorf("错误组模块统计错误: %+v", charge)
	}
}
