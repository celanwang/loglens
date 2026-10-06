# loglens 设计说明

> 智能日志分析工具：读取应用日志，对错误、性能问题与请求调用链进行分析，生成便于开发者阅读的中文 HTML 问题分析报告。

## 1. 技术选型

| 项 | 选择 | 理由 |
|---|---|---|
| 语言 | Go（本机 go1.26.8, darwin/arm64） | 单二进制交付、流式处理大文件性能好、标准库足够覆盖全部需求 |
| 交互形态 | CLI | PRD 未限定形态，CLI 最直接、评审成本最低 |
| 报告 | HTML（中文），图表用 ECharts | 单一输出格式，内嵌图表支撑"图表展示/趋势分析"增强项 |
| 第三方依赖 | **零外部依赖**（仅标准库） | `.env` 解析、百分位计算、HTTP 调用均手写，安装零负担 |
| LLM | 阿里云百炼（DashScope），模型 `qwen3.7-plus` | 走 OpenAI 兼容端点，标准库 `net/http` 直连，无需 SDK |

## 2. 整体架构

单进程管道式，模块间以内存数据结构衔接：

```
日志文件(多个)
   │  流式逐行读取
   ▼
parser    主正则 + k=v 抽取 + 三级容错（完整 / 部分字段缺失 / 不可解析）
   │  []model.Entry
   ▼
analyze   分析器组（各自独立遍历，输出汇总到 model.Report）
   │  ├─ LevelStats    级别分布
   │  ├─ ModuleStats   模块 × 日志数 / ERROR 数 / ERROR 率
   │  ├─ LatencyStats  cost=（操作级）与 totalCost=（请求级）双口径：平均 / P95 / P99
   │  ├─ TraceIndex    traceId → 有序日志链（链路还原 + 未完成请求检测）
   │  ├─ ErrorDigest   错误归组、Top N 慢请求、故障传播链
   │  ├─ Trend         按时间桶（分钟级）统计各级别数量与耗时趋势
   │  └─ RootCause     基于 trace 内故障传播时序推断候选根因（规则版）
   ▼
llm       可选增强：把错误归纳摘要发给百炼 qwen3.7-plus，生成自然语言诊断
   │       （无 API Key / 调用失败 → 自动降级为纯规则摘要，不阻塞报告）
   ▼
report    单一 model.Report → Go template 渲染 HTML（ECharts CDN 出图）
```

CLI 两个子命令：

```bash
loglens analyze -o report.html logs/a.log logs/b.log   # 全量分析 + 报告
loglens trace abc001 logs/a.log                        # 链路还原（终端输出）
```

## 3. 日志格式契约与解析容错

标准行（从 PRD 示例反推）：

```
2026-08-10 10:00:01.123 INFO  [UserService] [traceId=abc001] request start userId=10001
└─ 时间戳(毫秒) ─┘ └级别┘  └── 模块 ──┘  └── traceId ──┘  └── 消息 + k=v ──┘
```

解析策略（三级容错，对应测试数据噪声要求）：

1. **完全匹配**：主正则提取时间戳/级别/模块/traceId/消息，消息体内 `k=v`（`cost=108ms`、`status=500`）二次抽取；
2. **部分缺失**：缺 traceId / 模块 / 级别时尽力解析，对应字段置空并标记；
3. **不可解析**：归入 `PARSE_ERROR` 桶，保留原文行号，计入统计并在报告中呈现解析失败率。

## 4. 核心数据结构

```go
// 单条日志（parser 输出）
type Entry struct {
    Raw       string            // 原文
    Line      int               // 行号（含文件序号，便于定位）
    File      string            // 来源文件
    Time      time.Time         // 解析失败时为零值
    Level     string            // INFO/WARN/ERROR/.../UNKNOWN
    Module    string            // 缺失为 ""
    TraceID   string            // 缺失为 ""
    Msg       string            // 消息体
    Fields    map[string]string // k=v 抽取（cost/totalCost/status/error/...）
    ParseErr  bool              // 是否不可解析
}

// 报告中间模型（渲染与分析解耦）
type Report struct {
    Overview   Overview           // 总行数/时间范围/解析失败率/各级别计数
    Modules    []ModuleStat       // 模块 × 总数/ERROR 数/ERROR 率
    Latency    LatencyStat        // cost 与 totalCost 双口径（总体 + 分模块）
    Errors     []ErrorGroup       // 归组后的错误：模式/次数/模块/影响 trace 数/首末次
    SlowTraces []TraceSummary     // Top N 慢请求
    Traces     map[string][]int   // traceId → Entry 索引（链路还原）
    Trend      []TrendPoint       // 时间桶 × 级别计数 / 平均耗时
    RootCauses []RootCause        // 候选根因（规则推断）
    LLMSummary string             // LLM 诊断（降级时为空）
}
```

## 5. 关键分析规则

- **百分位**：排序后线性插值；`cost=` 按模块聚合，`totalCost=` 按请求聚合，均输出 平均/P95/P99。
- **慢请求**：`totalCost > 阈值`（默认 500ms，CLI 可配）；同时检出"有 start 无 end"的未完成请求。
- **错误归组**：以 `error=` 字段为主键，缺失时退化为消息模板归一化（数字/ID 替换为占位符）后的模式串。
- **候选根因**：同一 traceId 内按时间序找首个 ERROR/WARN 事件，结合跨 trace 的共现统计（如 `slow query` 先于 `database_timeout` 出现的频次）给出排序候选。
- **LLM 总结**：把错误归组 Top N + 慢请求 Top N + 根因候选压缩为 prompt，调用百炼生成 3~5 条中文诊断与处置建议。

## 6. LLM 接入（.env 抽象）

```dotenv
# .env.example
DASHSCOPE_API_KEY=sk-xxx
LLM_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
LLM_MODEL=qwen3.7-plus
LLM_TIMEOUT=30s
LLM_ENABLED=true
```

- 仅标准库 `net/http` POST `/chat/completions`（OpenAI 兼容模式）；
- 未配置 key、`LLM_ENABLED=false`、调用超时或返回异常 → 静默降级为规则摘要，报告中标注"LLM 未启用"。

## 7. 项目结构

```
loglens/
├── cmd/loglens/main.go      # CLI 入口（flag 解析，analyze / trace）
├── internal/
│   ├── model/               # Entry / Report 等数据结构
│   ├── parser/              # 解析与容错
│   ├── analyze/             # 统计 / 耗时 / trace / 错误归纳 / 趋势 / 根因
│   ├── llm/                 # 百炼客户端 + 降级
│   └── report/              # HTML 模板渲染（go:embed 模板）
├── scripts/genlogs/main.go  # 测试日志生成器（种子固定可复现）
├── testdata/                # 生成的样例日志 + 实际产出的报告
├── .env.example
├── README.md / DESIGN.md / AI_USAGE.md
└── go.mod                   # 零外部依赖
```

## 8. 测试数据生成

`scripts/genlogs` 参数化生成（默认 ~10000 行）：8 个模拟模块（Gateway/UserService/OrderService/PaymentService/Database/Cache/AuthService/NotificationService）、~500 个 traceId；约 70% 正常请求，注入 timeout / database error / network error / 慢请求，~2% 完全乱格式、~5% 字段缺失，并预埋若干条"慢查询→超时→上游 500"的因果故障链供根因分析验证。

## 9. 测试策略

- 单测重点：`parser`（三级容错、边界行）、百分位计算、错误归组、根因推断；
- 端到端：对生成日志跑 `analyze`，校验报告文件产出且关键指标与脚本预期一致；
- LLM 部分以接口注入 mock 测试，不依赖真实 API。

## 10. 主要技术取舍

| 取舍 | 决定 | 原因 |
|---|---|---|
| ECharts 走 CDN | 报告体积 vs 离线可看 | 选 CDN：报告小、实现简单；已知断网时图表不渲染（表格数据仍在） |
| 零外部依赖 | 手写 .env 解析/百分位/HTTP | 免 go get，评审开箱即用 |
| 聚类用规则模板而非 embedding | 确定性、无外部调用 | 满足"相似错误聚类"，语义聚类留给 LLM 总结 |
| LLM 为可选增强层 | 失败降级不阻塞 | 评审环境不可控，保证核心链路稳定 |

## 11. 当前已知问题（交付时最终确认）

- HTML 图表依赖 ECharts CDN，离线环境图表不渲染；
- 多文件按文件名序拼接时间线，跨文件乱序日志按时间戳重排后链路仍正确，但行号定位以单文件为准；
- LLM 诊断质量依赖 prompt 中的统计摘要完整性。

## 12. 下一步改进方向

- 增量分析（缓存已解析文件指纹）；
- 日志格式自适应（从样本自动推断正则）；
- embedding 级语义聚类。

## 13. 开发阶段与提交计划

按 conventional commits 中文风格（`type(scope): 描述`）阶段提交：

1. `chore(project): 初始化 Go 模块与项目骨架`
2. `feat(parser): 实现日志解析与三级容错`
3. `feat(analyze): 实现级别/模块/耗时统计`
4. `feat(analyze): 实现 trace 链路还原与错误归纳`
5. `feat(analyze): 实现趋势分析与候选根因推断`
6. `feat(llm): 接入百炼 LLM 异常总结与降级`
7. `feat(report): 实现中文 HTML 报告渲染`
8. `feat(cli): 实现 analyze 与 trace 子命令`
9. `chore(testdata): 添加日志生成脚本与测试数据`
10. `test(core): 补齐解析与统计单元测试`
11. `docs(delivery): 补齐 README/AI_USAGE 与验收记录`
