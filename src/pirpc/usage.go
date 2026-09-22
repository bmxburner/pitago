package pirpc

import "sort"

// CostBreak is one per-model cost line (port of pi's getUsageCostBreakdown).
type CostBreak struct {
	Key    string
	Cost   float64
	Tokens int
}

// UsageBreakdown groups entry usage by provider/model (assistant messages
// by responseModel, tool summaries apart), dropping zero rows, cost desc.
func UsageBreakdown(entries []SessionEntry) []CostBreak {
	sum := map[string]*CostBreak{}
	add := func(key string, u *EntryUsage) {
		if key == "" || u == nil {
			return
		}
		b, ok := sum[key]
		if !ok {
			b = &CostBreak{Key: key}
			sum[key] = b
		}
		b.Cost += u.Cost.Total
		b.Tokens += u.Input + u.Output + u.CacheRead + u.CacheWrite
	}
	for _, e := range entries {
		switch {
		case e.Type == "message" && e.Message != nil && e.Message.Role == "assistant":
			model := e.Message.Model
			if e.Message.ResponseModel != "" {
				model = e.Message.ResponseModel
			}
			add(e.Message.Provider+"/"+model, e.Message.Usage)
		case e.Type == "usage":
			add(e.Provider+"/"+e.Model, e.Usage)
		case e.Type == "message" && e.Message != nil && e.Message.Role == "toolResult" && e.Message.Usage != nil:
			add("Tools/summaries", e.Message.Usage)
		case (e.Type == "branch_summary" || e.Type == "compaction") && e.Usage != nil:
			add("Tools/summaries", e.Usage)
		}
	}
	out := make([]CostBreak, 0, len(sum))
	for _, b := range sum {
		if b.Cost > 0 || b.Tokens > 0 {
			out = append(out, *b)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Cost > out[j].Cost })
	return out
}
