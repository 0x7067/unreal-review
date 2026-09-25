package findings

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const SchemaVersion = 1

type Type string

const (
	TypeRun     Type = "run"
	TypeFinding Type = "finding"
	TypeSummary Type = "summary"
)

type Anchor string

const (
	AnchorNew Anchor = "new"
	AnchorOld Anchor = "old"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityNote    Severity = "note"
)

type Status string

const (
	StatusRunning  Status = "running"
	StatusComplete Status = "complete"
	StatusFailed   Status = "failed"
)

type Source struct {
	Kind    string `json:"kind"`
	Base    string `json:"base,omitempty"`
	Head    string `json:"head,omitempty"`
	BaseSHA string `json:"base_sha,omitempty"`
	HeadSHA string `json:"head_sha,omitempty"`
	DiffSHA string `json:"diff_sha,omitempty"`
}

func (s Source) SameDiff(other Source) bool {
	return s.BaseSHA == other.BaseSHA && s.HeadSHA == other.HeadSHA && s.DiffSHA == other.DiffSHA
}

type Cost struct {
	AmountUSD         float64 `json:"amount_usd"`
	Currency          string  `json:"currency"`
	InputTokens       int64   `json:"input_tokens"`
	OutputTokens      int64   `json:"output_tokens"`
	ReasoningTokens   int64   `json:"reasoning_tokens,omitempty"`
	CachedInputTokens int64   `json:"cached_input_tokens,omitempty"`
	Requests          int     `json:"requests"`
}

func (c Cost) Format() string {
	currency := c.Currency
	if currency == "" {
		currency = "USD"
	}
	return fmt.Sprintf("%s %.6f (%d input, %d output, %d requests)", currency, c.AmountUSD, c.InputTokens, c.OutputTokens, c.Requests)
}

func (c Cost) Add(other Cost) Cost {
	currency := c.Currency
	if currency == "" {
		currency = other.Currency
	}
	if currency == "" {
		currency = "USD"
	}
	return Cost{
		AmountUSD:         c.AmountUSD + other.AmountUSD,
		Currency:          currency,
		InputTokens:       c.InputTokens + other.InputTokens,
		OutputTokens:      c.OutputTokens + other.OutputTokens,
		ReasoningTokens:   c.ReasoningTokens + other.ReasoningTokens,
		CachedInputTokens: c.CachedInputTokens + other.CachedInputTokens,
		Requests:          c.Requests + other.Requests,
	}
}

func (c Cost) Recorded() bool {
	return c.Requests > 0 || c.InputTokens > 0 || c.OutputTokens > 0 || c.AmountUSD > 0
}

type Run struct {
	ID        string    `json:"id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Model     string    `json:"model,omitempty"`
	Status    Status    `json:"status,omitempty"`
	Source    Source    `json:"source"`
	Cost      Cost      `json:"cost"`
}

type Finding struct {
	ID        string   `json:"id,omitempty"`
	Path      string   `json:"path"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	Anchor    Anchor   `json:"anchor"`
	Severity  Severity `json:"severity"`
	Body      string   `json:"body"`
}

type Report struct {
	Run      *Run
	Findings []Finding
	Summary  string
}

func (r Report) Complete() bool {
	if r.Run == nil || r.Run.Status == "" {
		return true
	}
	return r.Run.Status == StatusComplete
}

type record struct {
	V         int             `json:"v"`
	Type      Type            `json:"type"`
	ID        string          `json:"id,omitempty"`
	CreatedAt string          `json:"created_at,omitempty"`
	Model     string          `json:"model,omitempty"`
	Source    *Source         `json:"source,omitempty"`
	Path      string          `json:"path,omitempty"`
	StartLine json.RawMessage `json:"start_line,omitempty"`
	EndLine   json.RawMessage `json:"end_line,omitempty"`
	Anchor    string          `json:"anchor,omitempty"`
	Severity  string          `json:"severity,omitempty"`
	Body      string          `json:"body,omitempty"`
	Summary   string          `json:"summary,omitempty"`
	Findings  json.RawMessage `json:"findings,omitempty"`
	Cost      *Cost           `json:"cost,omitempty"`
	Status    string          `json:"status,omitempty"`
}

type runRecord struct {
	V         int     `json:"v"`
	Type      Type    `json:"type"`
	ID        string  `json:"id,omitempty"`
	CreatedAt string  `json:"created_at,omitempty"`
	Model     string  `json:"model,omitempty"`
	Status    string  `json:"status,omitempty"`
	Source    *Source `json:"source,omitempty"`
	Cost      *Cost   `json:"cost,omitempty"`
}

func Parse(r io.Reader) (Report, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return Report{}, fmt.Errorf("read findings: %w", err)
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return Report{}, nil
	}
	if raw[0] == '[' {
		var records []json.RawMessage
		if err := json.Unmarshal(raw, &records); err != nil {
			return Report{}, fmt.Errorf("decode findings array: %w", err)
		}
		return reportFromRaw(records)
	}
	if bundle, ok, err := parseBundle(raw); err != nil {
		return Report{}, err
	} else if ok {
		return bundle, nil
	}
	return parseJSONL(raw)
}

func parseBundle(raw []byte) (Report, bool, error) {
	var rec record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return Report{}, false, nil
	}
	if len(rec.Findings) == 0 && rec.Type != "" {
		return Report{}, false, nil
	}
	if rec.Type != "" && rec.Type != TypeFinding && rec.Type != TypeRun && rec.Type != TypeSummary {
		return Report{}, false, nil
	}
	if len(rec.Findings) == 0 && rec.Summary == "" && rec.Body == "" {
		return Report{}, false, nil
	}
	if rec.Type != "" && len(rec.Findings) == 0 {
		return Report{}, false, nil
	}
	var nested []json.RawMessage
	if len(rec.Findings) > 0 {
		if err := json.Unmarshal(rec.Findings, &nested); err != nil {
			return Report{}, false, fmt.Errorf("decode nested findings: %w", err)
		}
	}
	report, err := reportFromRaw(nested)
	if err != nil {
		return Report{}, true, err
	}
	if rec.Summary != "" {
		report.Summary = rec.Summary
	} else if rec.Body != "" && rec.Type == TypeSummary {
		report.Summary = rec.Body
	}
	return report, true, nil
}

func parseJSONL(raw []byte) (Report, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(nil, 4<<20)
	var records []json.RawMessage
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		records = append(records, append(json.RawMessage(nil), line...))
	}
	if err := scanner.Err(); err != nil {
		return Report{}, fmt.Errorf("read findings jsonl: %w", err)
	}
	return reportFromRaw(records)
}

func reportFromRaw(raws []json.RawMessage) (Report, error) {
	var report Report
	for i, raw := range raws {
		var rec record
		if err := json.Unmarshal(raw, &rec); err != nil {
			return Report{}, fmt.Errorf("decode findings record %d: %w", i, err)
		}
		if rec.V != 0 && rec.V != SchemaVersion {
			return Report{}, fmt.Errorf("record %d: unsupported schema version %d", i, rec.V)
		}
		typ := rec.Type
		if typ == "" {
			if rec.Path != "" {
				typ = TypeFinding
			} else if rec.Body != "" || rec.Summary != "" {
				typ = TypeSummary
			} else {
				return Report{}, fmt.Errorf("record %d: type must be set", i)
			}
		}
		switch typ {
		case TypeRun:
			run, err := parseClosedRun(raw)
			if err != nil {
				return Report{}, fmt.Errorf("record %d: %w", i, err)
			}
			report.Run = &run
		case TypeFinding:
			finding, err := parseFinding(rec)
			if err != nil {
				return Report{}, fmt.Errorf("record %d: %w", i, err)
			}
			report.Findings = append(report.Findings, finding)
		case TypeSummary:
			body := strings.TrimSpace(rec.Body)
			if body == "" {
				body = strings.TrimSpace(rec.Summary)
			}
			if body == "" {
				return Report{}, fmt.Errorf("record %d: summary body must be set", i)
			}
			report.Summary = body
		default:
			return Report{}, fmt.Errorf("record %d: unknown type %q", i, typ)
		}
	}
	return report, nil
}

func parseClosedRun(raw []byte) (Run, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var rec runRecord
	if err := dec.Decode(&rec); err != nil {
		return Run{}, err
	}
	return parseRun(rec)
}

func parseRun(rec runRecord) (Run, error) {
	run := Run{
		ID:     rec.ID,
		Model:  rec.Model,
		Status: Status(strings.TrimSpace(rec.Status)),
	}
	switch run.Status {
	case "", StatusRunning, StatusComplete, StatusFailed:
	default:
		return Run{}, fmt.Errorf("status must be running, complete, or failed")
	}
	if rec.Source != nil {
		run.Source = *rec.Source
	}
	if rec.Cost != nil {
		run.Cost = *rec.Cost
	}
	if run.Cost.Currency == "" {
		run.Cost.Currency = "USD"
	}
	if rec.CreatedAt != "" {
		parsed, err := time.Parse(time.RFC3339, rec.CreatedAt)
		if err != nil {
			return Run{}, fmt.Errorf("created_at: %w", err)
		}
		run.CreatedAt = parsed
	}
	return run, nil
}

func parseFinding(rec record) (Finding, error) {
	start, err := parseLine(rec.StartLine)
	if err != nil {
		return Finding{}, fmt.Errorf("start_line: %w", err)
	}
	end, err := parseLine(rec.EndLine)
	if err != nil {
		return Finding{}, fmt.Errorf("end_line: %w", err)
	}
	return normalize(Finding{
		ID:        rec.ID,
		Path:      rec.Path,
		StartLine: start,
		EndLine:   end,
		Anchor:    Anchor(rec.Anchor),
		Severity:  Severity(rec.Severity),
		Body:      rec.Body,
	})
}

func normalize(finding Finding) (Finding, error) {
	finding.Path = strings.TrimSpace(finding.Path)
	if finding.Path == "" {
		return Finding{}, fmt.Errorf("path must be set")
	}
	if finding.EndLine == 0 {
		finding.EndLine = finding.StartLine
	}
	if finding.StartLine < 1 || finding.EndLine < finding.StartLine {
		return Finding{}, fmt.Errorf("line range %d-%d is invalid", finding.StartLine, finding.EndLine)
	}
	finding.Anchor = Anchor(strings.TrimSpace(string(finding.Anchor)))
	switch finding.Anchor {
	case "":
		finding.Anchor = AnchorNew
	case AnchorNew, AnchorOld:
	default:
		return Finding{}, fmt.Errorf("anchor must be new or old")
	}
	finding.Severity = Severity(strings.TrimSpace(string(finding.Severity)))
	switch finding.Severity {
	case SeverityError, SeverityWarning, SeverityNote:
	case "":
		return Finding{}, fmt.Errorf("severity must be set (error, warning, or note)")
	default:
		return Finding{}, fmt.Errorf("severity must be error, warning, or note")
	}
	finding.Body = strings.TrimSpace(finding.Body)
	if finding.Body == "" {
		return Finding{}, fmt.Errorf("body must be set")
	}
	if finding.ID == "" {
		finding.ID = Fingerprint(finding)
	}
	return finding, nil
}

func parseLine(raw json.RawMessage) (int, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return 0, nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, fmt.Errorf("must be an integer")
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("must be an integer")
	}
	return n, nil
}

func Fingerprint(finding Finding) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		finding.Path,
		strconv.Itoa(finding.StartLine),
		strconv.Itoa(finding.EndLine),
		string(finding.Anchor),
		finding.Body,
	}, "\x1f")))
	return hex.EncodeToString(sum[:8])
}

func AppendFinding(path string, finding Finding) (Finding, error) {
	finding, err := normalize(finding)
	if err != nil {
		return Finding{}, err
	}
	start, err := json.Marshal(finding.StartLine)
	if err != nil {
		return Finding{}, fmt.Errorf("encode start_line: %w", err)
	}
	end, err := json.Marshal(finding.EndLine)
	if err != nil {
		return Finding{}, fmt.Errorf("encode end_line: %w", err)
	}
	err = appendLine(path, record{
		V:         SchemaVersion,
		Type:      TypeFinding,
		ID:        finding.ID,
		Path:      finding.Path,
		StartLine: start,
		EndLine:   end,
		Anchor:    string(finding.Anchor),
		Severity:  string(finding.Severity),
		Body:      finding.Body,
	})
	if err != nil {
		return Finding{}, err
	}
	return finding, nil
}

func AppendSummary(path, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("body must be set")
	}
	return appendLine(path, record{V: SchemaVersion, Type: TypeSummary, Body: body})
}

func appendLine(path string, rec record) error {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() > 0 {
		var last [1]byte
		if _, err := file.ReadAt(last[:], info.Size()-1); err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if last[0] != '\n' {
			if _, err := file.WriteString("\n"); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
		}
	}
	enc := json.NewEncoder(file)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
		return fmt.Errorf("encode %s: %w", rec.Type, err)
	}
	return nil
}

func Write(w io.Writer, report Report) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if report.Run != nil {
		cost := report.Run.Cost
		if cost.Currency == "" {
			cost.Currency = "USD"
		}
		rec := runRecord{
			V:         SchemaVersion,
			Type:      TypeRun,
			ID:        report.Run.ID,
			Model:     report.Run.Model,
			Status:    string(report.Run.Status),
			Source:    &report.Run.Source,
			CreatedAt: report.Run.CreatedAt.UTC().Format(time.RFC3339),
			Cost:      &cost,
		}
		if err := enc.Encode(rec); err != nil {
			return fmt.Errorf("encode run: %w", err)
		}
	}
	for _, finding := range report.Findings {
		if finding.ID == "" {
			finding.ID = Fingerprint(finding)
		}
		start, err := json.Marshal(finding.StartLine)
		if err != nil {
			return fmt.Errorf("encode start_line: %w", err)
		}
		end, err := json.Marshal(finding.EndLine)
		if err != nil {
			return fmt.Errorf("encode end_line: %w", err)
		}
		rec := record{
			V:         SchemaVersion,
			Type:      TypeFinding,
			ID:        finding.ID,
			Path:      finding.Path,
			StartLine: start,
			EndLine:   end,
			Anchor:    string(finding.Anchor),
			Severity:  string(finding.Severity),
			Body:      finding.Body,
		}
		if err := enc.Encode(rec); err != nil {
			return fmt.Errorf("encode finding %s: %w", finding.ID, err)
		}
	}
	if strings.TrimSpace(report.Summary) != "" {
		if err := enc.Encode(record{
			V:    SchemaVersion,
			Type: TypeSummary,
			Body: report.Summary,
		}); err != nil {
			return fmt.Errorf("encode summary: %w", err)
		}
	}
	return nil
}

func ReadFile(path string) (Report, error) {
	file, err := os.Open(path)
	if err != nil {
		return Report{}, err
	}
	defer func() { _ = file.Close() }()
	report, err := Parse(file)
	if err != nil {
		return Report{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return report, nil
}

func WriteFile(path string, report Report) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tmpName := tmp.Name()
	writeErr := Write(tmp, report)
	closeErr := tmp.Close()
	if writeErr != nil {
		_ = os.Remove(tmpName)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("write %s: %w", path, closeErr)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
