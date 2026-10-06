package analyze

import (
	"sort"

	"github.com/celanwang/loglens/internal/model"
)

const maxRootCauses = 5

// buildRootCauses 候选根因推断：在每条含错误的链路中按时间序定位首个异常事件
// （WARN/ERROR/FATAL），跨链路聚合其出现次数，频次最高者即最可能的故障源头。
func buildRootCauses(traces []*traceInfo, entries []model.Entry, r *model.Report) {
	type agg struct {
		rc     *model.RootCause
		traces map[string]bool
	}
	m := map[string]*agg{}
	for _, t := range traces {
		if !t.hasError {
			continue
		}
		for _, i := range t.idx {
			e := entries[i]
			if !isErrorLevel(e.Level) && e.Level != model.LevelWarn {
				continue
			}
			key := e.Module + "|" + groupKey(e)
			a := m[key]
			if a == nil {
				a = &agg{
					rc:     &model.RootCause{Candidate: groupKey(e), Module: e.Module},
					traces: map[string]bool{},
				}
				m[key] = a
			}
			if !a.traces[t.id] {
				a.traces[t.id] = true
				a.rc.Hits++
				if len(a.rc.SampleTraces) < 3 {
					a.rc.SampleTraces = append(a.rc.SampleTraces, t.id)
				}
			}
			break
		}
	}
	list := make([]model.RootCause, 0, len(m))
	for _, a := range m {
		list = append(list, *a.rc)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Hits != list[j].Hits {
			return list[i].Hits > list[j].Hits
		}
		return list[i].Candidate < list[j].Candidate
	})
	if len(list) > maxRootCauses {
		list = list[:maxRootCauses]
	}
	r.RootCauses = list
}
