package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"unreal-review/internal/findings"
)

func ParseLogCost(r io.Reader) (findings.Cost, []string, error) {
	cost := findings.Cost{Currency: "USD"}
	var generationIDs []string
	decoder := json.NewDecoder(r)
	for {
		var value any
		if err := decoder.Decode(&value); err != nil {
			if errors.Is(err, io.EOF) {
				return cost, unique(generationIDs), nil
			}
			return findings.Cost{}, nil, fmt.Errorf("decode agent log: %w", err)
		}
		walkCost(value, &cost, &generationIDs)
	}
}

func walkCost(value any, cost *findings.Cost, generationIDs *[]string) {
	switch current := value.(type) {
	case map[string]any:
		kind, _ := current["Kind"].(string)
		if kind == "" {
			kind, _ = current["kind"].(string)
		}
		if kind == "model_response" {
			cost.Requests++
		}
		if id, ok := stringVal(current["ID"]); ok && looksLikeGenerationID(id) {
			*generationIDs = append(*generationIDs, id)
		}
		if id, ok := stringVal(current["id"]); ok && looksLikeGenerationID(id) {
			*generationIDs = append(*generationIDs, id)
		}
		if usage, ok := current["Usage"].(map[string]any); ok {
			absorbUsage(cost, usage)
		}
		if usage, ok := current["usage"].(map[string]any); ok {
			absorbUsage(cost, usage)
		}
		for _, child := range current {
			walkCost(child, cost, generationIDs)
		}
	case []any:
		for _, child := range current {
			walkCost(child, cost, generationIDs)
		}
	}
}

func absorbUsage(cost *findings.Cost, usage map[string]any) {
	cost.InputTokens += int64Val(usage, "InputTokens", "input_tokens", "prompt_tokens")
	cost.OutputTokens += int64Val(usage, "OutputTokens", "output_tokens", "completion_tokens")
	cost.ReasoningTokens += int64Val(usage, "ReasoningTokens", "reasoning_tokens")
	cost.CachedInputTokens += int64Val(usage, "CachedInputTokens", "cached_input_tokens", "cached_tokens")
	if amount, ok := floatVal(usage, "cost", "Cost", "total_cost", "amount_usd"); ok {
		cost.AmountUSD += amount
		return
	}
	for _, key := range []string{"Raw", "raw"} {
		raw, ok := usage[key].(map[string]any)
		if !ok {
			continue
		}
		if amount, ok := floatVal(raw, "cost", "total_cost"); ok {
			cost.AmountUSD += amount
			return
		}
	}
}

func FetchOpenRouterCost(ctx context.Context, apiKey string, generationIDs []string) (findings.Cost, error) {
	sum := findings.Cost{Currency: "USD"}
	client := &http.Client{Timeout: 15 * time.Second}
	for _, id := range generationIDs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openrouter.ai/api/v1/generation?id="+url.QueryEscape(id), nil)
		if err != nil {
			return findings.Cost{}, fmt.Errorf("openrouter generation %s: %w", id, err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("User-Agent", "unreal-review")
		resp, err := client.Do(req)
		if err != nil {
			return findings.Cost{}, fmt.Errorf("openrouter generation %s: %w", id, err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return findings.Cost{}, fmt.Errorf("openrouter generation %s: read: %w", id, err)
		}
		if resp.StatusCode >= 300 {
			return findings.Cost{}, fmt.Errorf("openrouter generation %s: %s: %s", id, resp.Status, bytes.TrimSpace(body))
		}
		var envelope map[string]any
		if err := json.Unmarshal(body, &envelope); err != nil {
			return findings.Cost{}, fmt.Errorf("openrouter generation %s: decode: %w", id, err)
		}
		data := envelope
		if nested, ok := envelope["data"].(map[string]any); ok {
			data = nested
		}
		sum.Requests++
		sum.InputTokens += int64Val(data, "native_tokens_prompt", "tokens_prompt", "prompt_tokens")
		sum.OutputTokens += int64Val(data, "native_tokens_completion", "tokens_completion", "completion_tokens")
		if amount, ok := floatVal(data, "total_cost", "cost"); ok {
			sum.AmountUSD += amount
		} else if usage, ok := data["usage"].(map[string]any); ok {
			absorbUsage(&sum, usage)
		}
	}
	return sum, nil
}

func looksLikeGenerationID(id string) bool {
	return strings.HasPrefix(id, "gen-") || strings.HasPrefix(id, "resp-") || strings.HasPrefix(id, "chatcmpl-")
}

func stringVal(value any) (string, bool) {
	s, ok := value.(string)
	s = strings.TrimSpace(s)
	return s, ok && s != ""
}

func int64Val(m map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch n := m[key].(type) {
		case float64:
			return int64(n)
		case json.Number:
			v, _ := n.Int64()
			return v
		}
	}
	return 0
}

func floatVal(m map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		switch n := m[key].(type) {
		case float64:
			return n, true
		case json.Number:
			v, err := n.Float64()
			if err != nil {
				continue
			}
			return v, true
		}
	}
	return 0, false
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
