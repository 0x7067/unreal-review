package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/0x7067/unreal-review/internal/eval"
	"github.com/0x7067/unreal-review/review"
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

func TestCompleteReportWithSaveErrorIsFailed(t *testing.T) {
	prec := 1.0
	saved := eval.PR38Score{
		Run: 1, Status: "complete", Err: "write findings: disk full",
		Hits: 5, Recall: 1, Precision: &prec, Produced: 5, CostUSD: 9, HitIDs: []string{"F1", "F2", "F3", "F5", "F6"},
	}
	ok := eval.PR38Score{Run: 2, Status: "complete", Hits: 1, Recall: 0.2, Precision: &prec, Produced: 1, CostUSD: 0.5, HitIDs: []string{"F1"}}
	if saved.Completed() || !ok.Completed() {
		t.Fatalf("completed saved=%v ok=%v", saved.Completed(), ok.Completed())
	}
	report := eval.AggregatePR38([]eval.PR38Score{saved, ok})
	if report.Completed != 1 || report.Failed != 1 || report.CostUSD != 0.5 || math.Abs(report.Mean.Recall-0.2) > 1e-12 || report.Union != 1 || report.UnionHits[0] != "F1" {
		t.Fatalf("save error leaked: completed=%d failed=%d cost=%v recall=%v union=%v", report.Completed, report.Failed, report.CostUSD, report.Mean.Recall, report.UnionHits)
	}
	var out, errOut bytes.Buffer
	err := finishPR38(&out, &errOut, report, true, t.TempDir())
	if err == nil || !bytes.Contains(errOut.Bytes(), []byte("pr38 run 1 failed: write findings: disk full")) || bytes.Contains(errOut.Bytes(), []byte("pr38 run 2 failed")) {
		t.Fatalf("err=%v stderr=%s", err, errOut.String())
	}
	if decErr := json.Unmarshal(out.Bytes(), &report); decErr != nil || report.Completed != 1 || report.Failed != 1 {
		t.Fatalf("stdout: %v %s", decErr, out.String())
	}
}

func TestRunPR38sReportsTheRunFinishedBeforeCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	prec := 1.0
	calls := 0
	var out, errOut bytes.Buffer
	err := runPR38s(ctx, &out, &errOut, t.TempDir(), 3, true, func(n int) (eval.PR38Score, error) {
		calls++
		if n != 1 {
			t.Fatalf("started run %d after cancel", n)
		}
		cancel()
		return eval.PR38Score{Status: "complete", Hits: 1, Recall: 0.2, Precision: &prec, Produced: 1, CostUSD: 0.5, HitIDs: []string{"F1"}}, nil
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	if !bytes.Contains(errOut.Bytes(), []byte("pr38 run 1 done")) || bytes.Contains(errOut.Bytes(), []byte("pr38 run 2")) {
		t.Fatalf("stderr=%s", errOut.String())
	}
	var got eval.PR38Report
	if decErr := json.Unmarshal(out.Bytes(), &got); decErr != nil || got.Completed != 1 || len(got.Runs) != 1 || got.Runs[0].Run != 1 || got.CostUSD != 0.5 {
		t.Fatalf("report: %v %s", decErr, out.String())
	}
}

func TestRunPR38sKeepsEarlierRunsWhenSetupFails(t *testing.T) {
	prec := 1.0
	var out, errOut bytes.Buffer
	err := runPR38s(context.Background(), &out, &errOut, t.TempDir(), 2, true, func(n int) (eval.PR38Score, error) {
		if n == 2 {
			return eval.PR38Score{}, errors.New("fetch failed")
		}
		return eval.PR38Score{Status: "complete", Hits: 1, Recall: 0.2, Precision: &prec, Produced: 1, CostUSD: 0.5, HitIDs: []string{"F1"}}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "fetch failed") {
		t.Fatalf("err=%v", err)
	}
	var got eval.PR38Report
	if decErr := json.Unmarshal(out.Bytes(), &got); decErr != nil || got.Completed != 1 || len(got.Runs) != 1 || got.Runs[0].Run != 1 || got.CostUSD != 0.5 {
		t.Fatalf("report: %v %s", decErr, out.String())
	}
	if bytes.Contains(errOut.Bytes(), []byte("pr38 run 1 failed")) {
		t.Fatalf("stderr=%s", errOut.String())
	}
}

func TestEvalPR38ValidRunsPrintAggregate(t *testing.T) {
	secrets["OPENROUTER_API_KEY"] = "dummy"
	t.Cleanup(func() { delete(secrets, "OPENROUTER_API_KEY") })
	t.Setenv("UNREAL_HARNESS_LLM_MODEL", "offline")
	orig := pr38Eval
	t.Cleanup(func() { pr38Eval = orig })
	prec := 0.5
	var out, errOut bytes.Buffer
	pr38Eval = func(ctx context.Context, root, model string, reviewer review.Agent, runs int, decompose, asJSON bool) error {
		if runs != 2 || !asJSON || model != "offline" || reviewer == nil {
			t.Fatalf("flags runs=%d json=%v model=%q reviewer=%v", runs, asJSON, model, reviewer != nil)
		}
		return runPR38s(ctx, &out, &errOut, root, runs, asJSON, func(n int) (eval.PR38Score, error) {
			return eval.PR38Score{Status: "complete", Hits: 2, Recall: 0.4, Precision: &prec, ErrorShare: 0.92, Produced: 4, CostUSD: 0.5, HitIDs: []string{"F1", "F2"}}, nil
		})
	}
	if err := cmdEval([]string{"--corpus", "pr38", "--runs", "2", "--json", "--out", t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	var got eval.PR38Report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Completed != 2 || len(got.Runs) != 2 || math.Abs(got.Mean.Recall-0.4) > 1e-12 || got.Mean.Precision == nil || math.Abs(*got.Mean.Precision-0.5) > 1e-12 {
		t.Fatalf("report: %v %s", err, out.String())
	}
	if !bytes.Contains(errOut.Bytes(), []byte("pr38 run 1 done")) || !bytes.Contains(errOut.Bytes(), []byte("pr38 run 2 done")) {
		t.Fatalf("stderr=%s", errOut.String())
	}
}
