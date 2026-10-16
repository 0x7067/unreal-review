package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/0x7067/unreal-review/findings"
	"github.com/0x7067/unreal-review/internal/eval"
)

func TestRunLabeledPrintsEachCaseAndThePool(t *testing.T) {
	pr31, err := eval.ScoreLabeled("pr31", findings.Report{
		Run: &findings.Run{Status: findings.StatusComplete, Cost: findings.Cost{AmountUSD: 0.1, Requests: 2}},
		Findings: []findings.Finding{
			{Path: "internal/eval/martian.go", StartLine: 197, EndLine: 197, Severity: findings.SeverityError},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pr25, err := eval.ScoreLabeled("pr25", findings.Report{
		Run: &findings.Run{Status: findings.StatusFailed},
		Findings: []findings.Finding{
			{Path: "internal/eval/martian.go", StartLine: 266, EndLine: 266, Severity: findings.SeverityError},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pr25.Err = "judge stalled"
	var out, errOut bytes.Buffer
	err = runLabeledCases(context.Background(), &out, &errOut, t.TempDir(), 1, false, []string{"pr31", "pr25"}, func(n int, name string) (eval.PR38Score, error) {
		if name == "pr31" {
			return pr31, nil
		}
		return pr25, nil
	})
	if err == nil || err.Error() != "1 of 1 labeled runs failed" {
		t.Fatalf("err=%v", err)
	}
	if !bytes.Contains(errOut.Bytes(), []byte("pr31 run 1 done")) || !bytes.Contains(errOut.Bytes(), []byte("pr25 run 1 failed: judge stalled")) || bytes.Contains(errOut.Bytes(), []byte("pr31 run 1 failed")) {
		t.Fatalf("stderr=%s", errOut.String())
	}
	text := out.String()
	if !strings.Contains(text, "pr31\n") || !strings.Contains(text, "pr25\n") || !strings.Contains(text, "pooled\n") || !strings.Contains(text, "1/5") || !strings.Contains(text, "pr31-profile") {
		t.Fatalf("table:\n%s", text)
	}
	if strings.Contains(text, "five real labels") {
		t.Fatalf("pr38 rule leaked into the labeled report:\n%s", text)
	}
}

func TestRunLabeledKeepsEarlierCasesWhenSetupFails(t *testing.T) {
	pr31, err := eval.ScoreLabeled("pr31", findings.Report{
		Run: &findings.Run{Status: findings.StatusComplete, Cost: findings.Cost{AmountUSD: 0.1, Requests: 1}},
		Findings: []findings.Finding{
			{Path: "internal/agent/bash_boundary.go", StartLine: 108, EndLine: 111, Severity: findings.SeverityError},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	err = runLabeledCases(context.Background(), &out, &errOut, t.TempDir(), 1, true, []string{"pr31", "pr25"}, func(n int, name string) (eval.PR38Score, error) {
		if name == "pr25" {
			return eval.PR38Score{}, errors.New("fetch failed")
		}
		return pr31, nil
	})
	if err == nil || !strings.Contains(err.Error(), "fetch failed") {
		t.Fatalf("err=%v", err)
	}
	var got eval.LabeledReport
	if decErr := json.Unmarshal(out.Bytes(), &got); decErr != nil || len(got.Cases) != 2 || got.Cases[0].Completed != 1 || got.Cases[0].CostUSD != 0.1 || len(got.Cases[1].Runs) != 0 || got.Pooled.Completed != 0 || len(got.Pooled.Runs) != 0 || got.Pooled.Real != 9 || got.SpentCostUSD != 0.1 {
		t.Fatalf("report: %v %s", decErr, out.String())
	}
	if got.Cases[0].Union != 1 || got.Cases[0].UnionHits[0] != "pr31-symlink" {
		t.Fatalf("union %v", got.Cases[0].UnionHits)
	}
}
