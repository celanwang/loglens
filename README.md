# loglens

智能日志分析工具：读取应用运行日志，对错误、性能问题与请求调用链进行分析，生成便于开发者阅读的中文 HTML 问题分析报告。

## 功能特性

- **日志级别统计**：INFO / WARN / ERROR 等级别分布与占比
- **模块统计**：各模块日志数量、ERROR 数量、ERROR 比例
- **请求耗时分析**：`totalCost`（请求级）与 `cost`（操作级）双口径的平均 / P50 / P95 / P99
- **链路还原**：按 `traceId` 还原完整请求链路，展示模块流转与逐步耗时
- **错误归纳**：相似错误自动聚类（`error=` 字段 + 消息模板归一化）
- **Top N 慢请求**：超过阈值（默认 500ms，可配）的请求按耗时排序
- **未完成请求**：识别"有 request start 无 request end"的中断链路
- **时间趋势**：分钟级日志量与平均耗时趋势图表
- **候选根因**：基于故障链路内"首个异常点"的跨链路聚合推断
- **LLM 智能诊断**：接入阿里云百炼（默认 qwen3.7-plus），基于统计结论生成问题诊断与排查建议；未配置或调用失败时自动降级为规则归纳
- **容错解析**：乱格式行、字段缺失行、大小写抖动均能容错处理并计入统计

## 环境要求

- Go 1.26+（仅标准库，无第三方依赖）
- 查看 HTML 报告中的图表需要联网（ECharts 走 CDN，断网时图表不渲染但表格数据完整）

## 安装方式

```bash
git clone <repo-url> && cd loglens
go build -o loglens ./cmd/loglens
```

## 启动方式与使用示例

### 分析日志并生成报告

```bash
# 单文件
./loglens analyze testdata/sample.log

# 多文件合并分析 + 自定义输出与阈值
./loglens analyze -o report.html -slow-ms 300 -top 20 logs/app1.log logs/app2.log

# 不调用 LLM（纯规则归纳）
./loglens analyze -llm=false testdata/sample.log
```

分析完成后终端输出关键指标摘要，HTML 报告写入 `-o` 指定路径（默认 `loglens-report.html`）。仓库内 [`testdata/report.html`](testdata/report.html) 即为程序实际生成的报告样例。

### 还原请求链路

```bash
./loglens trace abc001 testdata/sample.log
```

输出该 traceId 的完整日志链路（按时间排序）、模块流转、总耗时与请求状态；traceId 不存在时给出相似候选。

### 生成测试日志

```bash
go run ./scripts/genlogs -out testdata/sample.log -lines 10000 -seed 42
```

固定种子可完全复现；生成的数据包含正常/慢/异常/超时/中断请求链路，以及约 2% 乱格式行与约 5% 字段缺失行。

## 配置（LLM 接入）

复制 `.env.example` 为 `.env` 并填入百炼 API Key：

```dotenv
DASHSCOPE_API_KEY=sk-xxx
LLM_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
LLM_MODEL=qwen3.7-plus
LLM_TIMEOUT=30s
LLM_ENABLED=true
```

进程环境变量优先于 `.env` 文件。未配置 key、`LLM_ENABLED=false` 或调用失败时，报告自动降级为规则归纳并标注原因。

## 日志格式

标准行格式（解析器对字段缺失、乱格式行有容错）：

```text
2026-08-10 10:00:01.123 INFO  [UserService] [traceId=abc001] request start userId=10001
└─ 时间戳(毫秒) ─┘ └级别┘  └── 模块 ──┘  └── traceId ──┘  └── 消息 + k=v 键值对 ──┘
```

## 测试

```bash
go test ./...      # 单元测试 + 基于 testdata/sample.log 的端到端测试
go vet ./...
```

## 项目结构

```
cmd/loglens/        CLI 入口（analyze / trace 子命令）
internal/
  parser/           日志解析与三级容错
  model/            核心数据结构（Entry / Report）
  analyze/          统计分析：级别/模块/耗时/链路/错误归纳/趋势/候选根因
  llm/              百炼 LLM 客户端（.env 配置 + 降级）
  report/           HTML 报告渲染（go:embed 模板 + ECharts）
scripts/genlogs/    测试日志生成器（种子固定可复现）
testdata/           样本日志与程序实际生成的分析报告
```

设计说明（架构 / 数据结构 / 分析流程 / 技术取舍 / 已知问题）见 [DESIGN.md](DESIGN.md)。
