package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/celanwang/loglens/internal/model"
)

func TestLoadConfigFromDotEnv(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "# comment\n" +
		"DASHSCOPE_API_KEY=sk-test-123\n" +
		"LLM_MODEL=qwen-plus\n" +
		"LLM_TIMEOUT=10s\n" +
		"LLM_ENABLED=true\n" +
		"bad line without equals\n"
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig(envFile)
	if cfg.APIKey != "sk-test-123" {
		t.Errorf("APIKey 读取错误: %q", cfg.APIKey)
	}
	if cfg.Model != "qwen-plus" || cfg.Timeout != 10*time.Second {
		t.Errorf("模型/超时读取错误: %+v", cfg)
	}
	if !cfg.Available() {
		t.Error("配置完整时 Available 应为 true")
	}
}

func TestLoadConfigDefaultsAndDisabled(t *testing.T) {
	cfg := LoadConfig(filepath.Join(t.TempDir(), "nonexistent.env"))
	if cfg.BaseURL != defaultBaseURL || cfg.Model != defaultModel || cfg.Timeout != defaultTimeout {
		t.Errorf("缺省配置错误: %+v", cfg)
	}
	if cfg.Available() {
		t.Error("无 API Key 时 Available 应为 false")
	}
}

func TestAvailableRejectsPlaceholder(t *testing.T) {
	cfg := Config{Enabled: true, APIKey: "sk-xxx"}
	if cfg.Available() {
		t.Error("占位符 key 不应视为可用")
	}
}

func TestSummarizeWithMockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("请求路径错误: %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer sk-") {
			t.Errorf("缺少 Authorization 头")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"1. 数据库超时是主因"}}]}`))
	}))
	defer srv.Close()

	cfg := Config{APIKey: "sk-test", BaseURL: srv.URL, Model: "qwen3.7-plus", Timeout: 5 * time.Second, Enabled: true}
	got, err := NewClient(cfg).Summarize(context.Background(), "测试 prompt")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1. 数据库超时是主因" {
		t.Errorf("返回内容错误: %q", got)
	}
}

func TestSummarizeErrorPaths(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API-key"}}`))
	}))
	defer srv.Close()
	cfg := Config{APIKey: "sk-bad", BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second, Enabled: true}
	if _, err := NewClient(cfg).Summarize(context.Background(), "x"); err == nil {
		t.Error("非 200 响应应返回错误")
	} else if !strings.Contains(err.Error(), "401") {
		t.Errorf("错误应包含状态码: %v", err)
	}

	cfg.BaseURL = "http://127.0.0.1:1"
	if _, err := NewClient(cfg).Summarize(context.Background(), "x"); err == nil {
		t.Error("连接失败应返回错误而非 panic")
	}
}

func TestBuildPrompt(t *testing.T) {
	r := &model.Report{
		Overview: model.Overview{TotalLines: 100, FailedLines: 2, LevelCounts: map[string]int{"ERROR": 5}},
		Errors: []model.ErrorGroup{
			{Key: "database_timeout", Count: 3, Modules: []string{"OrderService"}, TraceCount: 2},
		},
		SlowTraces:      []model.TraceSummary{{TraceID: "t1", TotalCost: 900, Modules: []string{"Gateway", "Database"}}},
		UnfinishedTotal: 1,
		RootCauses:      []model.RootCause{{Module: "Database", Candidate: "slow query", Hits: 2}},
		SlowThresholdMs: 500,
	}
	p := BuildPrompt(r)
	for _, kw := range []string{"数据概览", "database_timeout", "t1", "slow query", "未完成请求", "诊断"} {
		if !strings.Contains(p, kw) {
			t.Errorf("prompt 应包含 %q:\n%s", kw, p)
		}
	}
}
