package main

import (
	"strings"
	"testing"

	"unreal-review/internal/eval"
)

func TestSelectPlantedPilotCases(t *testing.T) {
	all, err := selectPlanted(eval.Corpus, "")
	if err != nil || len(all) != len(eval.Corpus) {
		t.Fatalf("default selection: %d err=%v", len(all), err)
	}
	picked, err := selectPlanted(eval.Corpus, "clean, race,once-failure,wrap-break")
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"clean", "race", "once-failure", "wrap-break"} {
		if i >= len(picked) || picked[i].Name != want {
			t.Fatalf("selection order: %+v", picked)
		}
	}
	for _, tc := range []struct{ names, message string }{
		{"unknown", `unknown planted case "unknown"`},
		{"race,race", `duplicate planted case "race"`},
		{"race, race", `duplicate planted case "race"`},
		{"race,", `unknown planted case ""`},
	} {
		if _, err := selectPlanted(eval.Corpus, tc.names); err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Fatalf("selection %q: %v", tc.names, err)
		}
	}
}

func TestSelectMartianRejectsDuplicateWorkspaceScopes(t *testing.T) {
	cases := []eval.MartianCase{{Name: "first"}, {Name: "second"}}
	picked, err := selectMartian(cases, " second,first ")
	if err != nil || len(picked) != 2 || picked[0].Name != "second" || picked[1].Name != "first" {
		t.Fatalf("selection: %+v err=%v", picked, err)
	}
	if _, err := selectMartian(cases, "first, first"); err == nil || !strings.Contains(err.Error(), "duplicate martian case") {
		t.Fatalf("duplicate workspace must fail before concurrent setup: %v", err)
	}
}
