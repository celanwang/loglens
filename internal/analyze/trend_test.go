package analyze

import (
	"testing"
)

func TestBuildRootCauses(t *testing.T) {
	r := Build(traceEntries(), Options{})
	if len(r.RootCauses) == 0 {
		t.Fatal("含错误链路应产出候选根因")
	}
	top := r.RootCauses[0]
	// t-err 链路中首个异常事件是 Database 的 slow query
	if top.Module != "Database" || top.Candidate != "slow query" {
		t.Errorf("候选根因应为 Database slow query: %+v", top)
	}
	if top.Hits != 1 || len(top.SampleTraces) != 1 || top.SampleTraces[0] != "t-err" {
		t.Errorf("根因命中统计错误: %+v", top)
	}
}

func TestBuildRootCausesMultipleTraces(t *testing.T) {
	entries := traceEntries()
	// 再造两条同样以 Database slow query 开头的故障链路，验证跨链路聚合
	for _, tid := range []string{"t-err2", "t-err3"} {
		for _, src := range traceEntries()[6:10] {
			e := src
			e.TraceID = tid
			entries = append(entries, e)
		}
	}
	r := Build(entries, Options{})
	if len(r.RootCauses) == 0 || r.RootCauses[0].Hits != 3 {
		t.Fatalf("相同首异常应聚合为 3 次命中: %+v", r.RootCauses)
	}
}

func TestBuildTrend(t *testing.T) {
	r := Build(traceEntries(), Options{})
	if len(r.Trend) == 0 {
		t.Fatal("应产出趋势数据")
	}
	total := 0
	var reqCount int
	for _, tp := range r.Trend {
		if tp.Bucket.Minute() != 0 && tp.Bucket.Hour() != 10 {
			t.Errorf("时间桶应截断到分钟: %v", tp.Bucket)
		}
		for _, c := range tp.LevelCounts {
			total += c
		}
		reqCount += tp.RequestCount
	}
	if total != 12 {
		t.Errorf("趋势覆盖的日志条数应为 12，得到 %d", total)
	}
	if reqCount != 3 {
		t.Errorf("趋势请求耗时应覆盖 3 个 totalCost 样本，得到 %d", reqCount)
	}
}
