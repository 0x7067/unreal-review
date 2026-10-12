package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/0x7067/unreal-review/internal/eval"
)

func TestFinishPR38PrintsReportAndFailsTheCommand(t *testing.T) {
	prec := 1.0
	report := eval.AggregatePR38([]eval.PR38Score{
		{Run: 1, Status: "failed", Err: "boom", HitIDs: []string{}},
		{Run: 2, Status: "complete", Hits: 1, Recall: 0.2, Precision: &prec, Produced: 1, CostUSD: 0.5, HitIDs: []string{"F1"}},
	})
	var out, errOut bytes.Buffer
	err := finishPR38(&out, &errOut, report, true, "/tmp/pr38")
	if err == nil || !bytes.Contains(errOut.Bytes(), []byte("pr38 run 1 failed: boom")) || bytes.Contains(errOut.Bytes(), []byte("pr38 run 2 failed")) {
		t.Fatalf("err=%v stderr=%s", err, errOut.String())
	}
	var got eval.PR38Report
	if decErr := json.Unmarshal(out.Bytes(), &got); decErr != nil || got.Completed != 1 || got.Failed != 1 || got.CostUSD != 0.5 {
		t.Fatalf("stdout: %v %s", decErr, out.String())
	}
}
