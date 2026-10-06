package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client 百炼 OpenAI 兼容端点客户端，仅依赖标准库。
type Client struct {
	cfg Config
	hc  *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{cfg: cfg, hc: &http.Client{Timeout: cfg.Timeout}}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// Summarize 发送日志统计摘要，返回中文诊断文本。
func (c *Client) Summarize(ctx context.Context, prompt string) (string, error) {
	reqBody := chatRequest{
		Model: c.cfg.Model,
		Messages: []chatMessage{
			{Role: "system", Content: "你是一名资深的后端稳定性工程师，擅长根据日志统计定位线上问题。请始终用简体中文回答。"},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用 LLM 失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("读取 LLM 响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM 返回状态码 %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", fmt.Errorf("解析 LLM 响应失败: %w", err)
	}
	if len(cr.Choices) == 0 || strings.TrimSpace(cr.Choices[0].Message.Content) == "" {
		return "", errors.New("LLM 返回内容为空")
	}
	return strings.TrimSpace(cr.Choices[0].Message.Content), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
