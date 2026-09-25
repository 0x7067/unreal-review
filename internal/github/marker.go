package github

import (
	"fmt"
	"strconv"
	"strings"
)

const markerPrefix = "<!-- unreal-review "

type Status struct {
	Head    string
	Runs    int
	CostUSD float64
}

func StatusMarker(s Status) string {
	return fmt.Sprintf("%sstatus %s %d %s -->", markerPrefix,
		s.Head, s.Runs, strconv.FormatFloat(s.CostUSD, 'f', -1, 64))
}

func ParseStatus(body string) (Status, bool) {
	fields := markerFields(body)
	if len(fields) != 4 || fields[0] != "status" || fields[1] == "" {
		return Status{}, false
	}
	runs, err := strconv.Atoi(fields[2])
	if err != nil {
		return Status{}, false
	}
	cost, err := strconv.ParseFloat(fields[3], 64)
	if err != nil {
		return Status{}, false
	}
	return Status{Head: fields[1], Runs: runs, CostUSD: cost}, true
}

func FindingMarker(id string) string {
	return fmt.Sprintf("%sfinding %s -->", markerPrefix, id)
}

func ParseFinding(body string) (string, bool) {
	fields := markerFields(body)
	if len(fields) != 2 || fields[0] != "finding" || fields[1] == "" {
		return "", false
	}
	return fields[1], true
}

func markerFields(body string) []string {
	at := strings.Index(body, markerPrefix)
	if at < 0 {
		return nil
	}
	rest := body[at+len(markerPrefix):]
	end := strings.Index(rest, "-->")
	if end < 0 {
		return nil
	}
	return strings.Fields(rest[:end])
}

func WithoutMarker(body string) string {
	at := strings.Index(body, markerPrefix)
	if at < 0 {
		return body
	}
	end := strings.Index(body[at:], "-->")
	if end < 0 {
		return body
	}
	return strings.TrimSpace(body[:at] + body[at+end+len("-->"):])
}
