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

func writeEnv(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadConfigExplicitProvider(t *testing.T) {
	cfg := LoadConfig(writeEnv(t,
		"LLM_PROVIDER=qwen\nDASHSCOPE_API_KEY=sk-qwen-1\nLLM_MODEL=qwen-plus\nLLM_TIMEOUT=10s\n"))
	if cfg.Provider != "qwen" || cfg.APIKey != "sk-qwen-1" {
		t.Errorf("qwen Provider 解析错误: %+v", cfg)
	}
	if cfg.Model != "qwen-plus" || cfg.Timeout != 10*time.Second {
		t.Errorf("显式覆盖未生效: %+v", cfg)
	}
	if !cfg.Available() {
		t.Error("配置完整时 Available 应为 true")
	}
}

func TestLoadConfigDeepSeekPreferred(t *testing.T) {
	// 两家 Key 都存在且未指定 Provider 时，优先 DeepSeek
	cfg := LoadConfig(writeEnv(t, "DEEPSEEK_API_KEY=sk-ds-1\nDASHSCOPE_API_KEY=sk-qwen-1\n"))
	if cfg.Provider != "deepseek" || cfg.APIKey != "sk-ds-1" {
		t.Errorf("DeepSeek 应优先: %+v", cfg)
	}
	if cfg.BaseURL != "https://api.deepseek.com" || cfg.Model != "deepseek-flash" {
		t.Errorf("DeepSeek 预设端点/模型错误: %+v", cfg)
	}
}

func TestLoadConfigKeyFallback(t *testing.T) {
	// 显式指定 deepseek 但只有 qwen 的 Key：回退共用
	cfg := LoadConfig(writeEnv(t, "LLM_PROVIDER=deepseek\nDASHSCOPE_API_KEY=sk-shared\n"))
	if cfg.APIKey != "sk-shared" {
		t.Errorf("应回退使用另一家 Key: %+v", cfg)
	}
	// 通用 LLM_API_KEY 优先级最高
	cfg = LoadConfig(writeEnv(t, "LLM_API_KEY=sk-generic\nDEEPSEEK_API_KEY=sk-ds-1\n"))
	if cfg.APIKey != "sk-generic" {
		t.Errorf("LLM_API_KEY 应优先: %+v", cfg)
	}
}

func TestLoadConfigDefaultsAndDisabled(t *testing.T) {
	cfg := LoadConfig(filepath.Join(t.TempDir(), "nonexistent.env"))
	if cfg.Provider != "deepseek" || cfg.BaseURL != "https://api.deepseek.com" || cfg.Model != "deepseek-flash" {
		t.Errorf("缺省应为 DeepSeek 预设: %+v", cfg)
	}
	if cfg.Timeout != defaultTimeout {
		t.Errorf("缺省超时错误: %+v", cfg)
	}
	if cfg.Available() {
		t.Error("无 API Key 时 Available 应为 false")
	}

	cfg = LoadConfig(writeEnv(t, "DEEPSEEK_API_KEY=sk-ds-1\nLLM_ENABLED=off\n"))
	if cfg.Enabled {
		t.Error("LLM_ENABLED=off 应禁用")
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

	cfg := Config{APIKey: "sk-test", BaseURL: srv.URL, Model: "deepseek-flash", Timeout: 5 * time.Second, Enabled: true}
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
