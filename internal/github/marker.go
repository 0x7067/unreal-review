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
	fields, _ := markerFields(body)
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
	fields, _ := markerFields(body)
	if len(fields) != 2 || fields[0] != "finding" || fields[1] == "" {
		return "", false
	}
	return fields[1], true
}

func ResolvedMarker(id string) string {
	return fmt.Sprintf("%sresolved %s -->", markerPrefix, id)
}

func ParseResolved(body string) (string, bool) {
	fields, _ := markerFields(body)
	if len(fields) != 2 || fields[0] != "resolved" || fields[1] == "" {
		return "", false
	}
	return fields[1], true
}

func DroppedMarker(payload string) string {
	return fmt.Sprintf("%sdropped %s -->", markerPrefix, payload)
}

func ParseDropped(body string) []string {
	var payloads []string
	for {
		fields, rest := markerFields(body)
		if fields == nil {
			return payloads
		}
		if len(fields) == 2 && fields[0] == "dropped" {
			payloads = append(payloads, fields[1])
		}
		body = rest
	}
}

func markerFields(body string) ([]string, string) {
	at := strings.Index(body, markerPrefix)
	if at < 0 {
		return nil, ""
	}
	rest := body[at+len(markerPrefix):]
	end := strings.Index(rest, "-->")
	if end < 0 {
		return nil, ""
	}
	return strings.Fields(rest[:end]), rest[end+len("-->"):]
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
