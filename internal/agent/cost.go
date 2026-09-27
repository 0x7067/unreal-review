package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"unreal-review/internal/findings"
)

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
