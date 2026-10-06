// genlogs 生成贴近真实场景的测试日志：正常/慢/异常/超时/中断请求链路，
// 并注入约 2% 乱格式行与约 5% 字段缺失行。固定随机种子，输出可复现。
//
// 用法:
//
//	go run ./scripts/genlogs -out testdata/sample.log -lines 10000 -seed 42
package main

import (
	"bufio"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

type line struct {
	t   time.Time
	seq int
	txt string
}

func fmtLine(t time.Time, level, module, tid, msg string) string {
	return fmt.Sprintf("%s %-5s [%s] [traceId=%s] %s",
		t.Format("2006-01-02 15:04:05.000"), level, module, tid, msg)
}

type traceBuilder struct {
	t   time.Time
	tid string
	uid int
	rng *rand.Rand
	add func(time.Time, string)
}

func (b *traceBuilder) emit(level, module, msg string, gapMs int) {
	b.t = b.t.Add(time.Duration(gapMs) * time.Millisecond)
	b.add(b.t, fmtLine(b.t, level, module, b.tid, msg))
}

// emitCost 记录一条带 cost= 的操作日志，并让时钟前进该耗时（操作完成后才打日志）。
func (b *traceBuilder) emitCost(level, module, action string, costMs int) {
	b.emit(level, module, fmt.Sprintf("%s cost=%dms", action, costMs), costMs+b.gap())
}

func (b *traceBuilder) cost(min, max int) int { return min + b.rng.Intn(max-min) }
func (b *traceBuilder) gap() int              { return 2 + b.rng.Intn(12) }
func (b *traceBuilder) elapsed(start time.Time) int {
	return int(b.t.Sub(start).Milliseconds()) + b.gap()
}

// 正常用户查询链路
func (b *traceBuilder) normalUserFlow() {
	start := b.t
	b.emit("INFO", "Gateway", fmt.Sprintf("request start userId=%d", b.uid), 0)
	b.emitCost("INFO", "AuthService", "verify token", b.cost(5, 30))
	b.emitCost("INFO", "UserService", "load user success", b.cost(20, 120))
	if b.rng.Float64() < 0.6 {
		b.emitCost("INFO", "Cache", "cache hit", b.cost(1, 10))
	}
	b.emitCost("INFO", "Database", "query user", b.cost(5, 80))
	b.emit("INFO", "Gateway", fmt.Sprintf("request end totalCost=%dms", b.elapsed(start)), b.gap())
}

// 正常下单链路
func (b *traceBuilder) normalOrderFlow() {
	start := b.t
	b.emit("INFO", "Gateway", fmt.Sprintf("request start userId=%d", b.uid), 0)
	b.emitCost("INFO", "AuthService", "verify token", b.cost(5, 30))
	b.emitCost("INFO", "OrderService", "query order", b.cost(8, 60))
	b.emitCost("INFO", "Database", "query", b.cost(5, 60))
	b.emitCost("INFO", "PaymentService", "charge success", b.cost(30, 150))
	b.emitCost("INFO", "NotificationService", "send notify", b.cost(3, 20))
	b.emit("INFO", "Gateway", fmt.Sprintf("request end totalCost=%dms", b.elapsed(start)), b.gap())
}

// 慢请求链路（功能正常但耗时高）
func (b *traceBuilder) slowFlow() {
	start := b.t
	b.emit("INFO", "Gateway", fmt.Sprintf("request start userId=%d", b.uid), 0)
	b.emitCost("INFO", "AuthService", "verify token", b.cost(5, 30))
	b.emitCost("WARN", "Database", "slow query", 500+b.rng.Intn(1500))
	b.emitCost("INFO", "OrderService", "query order", b.cost(8, 60))
	b.emit("INFO", "Gateway", fmt.Sprintf("request end totalCost=%dms", b.elapsed(start)), b.gap())
}

// 数据库超时故障链：慢查询 → 上游失败 → Gateway 500
func (b *traceBuilder) dbErrFlow() {
	start := b.t
	b.emit("INFO", "Gateway", fmt.Sprintf("request start userId=%d", b.uid), 0)
	b.emitCost("INFO", "AuthService", "verify token", b.cost(5, 30))
	b.emitCost("INFO", "OrderService", "query order", b.cost(8, 60))
	b.emitCost("WARN", "Database", "slow query", 600+b.rng.Intn(1500))
	b.emit("ERROR", "OrderService", "query order failed error=database_timeout", b.gap())
	b.emit("ERROR", "Gateway", fmt.Sprintf("request failed status=500 totalCost=%dms", b.elapsed(start)), b.gap())
}

// 网络异常故障链
func (b *traceBuilder) netErrFlow() {
	start := b.t
	b.emit("INFO", "Gateway", fmt.Sprintf("request start userId=%d", b.uid), 0)
	b.emitCost("INFO", "AuthService", "verify token", b.cost(5, 30))
	b.emitCost("INFO", "OrderService", "query order", b.cost(8, 60))
	b.emitCost("ERROR", "PaymentService", "charge failed error=network_error", b.cost(20, 80))
	b.emit("ERROR", "Gateway", fmt.Sprintf("request failed status=502 totalCost=%dms", b.elapsed(start)), b.gap())
}

// 超时：一半表现为请求中断（无 end），一半表现为 Gateway 504
func (b *traceBuilder) timeoutFlow() {
	start := b.t
	b.emit("INFO", "Gateway", fmt.Sprintf("request start userId=%d", b.uid), 0)
	b.emitCost("INFO", "AuthService", "verify token", b.cost(5, 30))
	b.emitCost("WARN", "Database", "slow query", 2000+b.rng.Intn(2000))
	if b.rng.Float64() < 0.5 {
		b.emit("ERROR", "Gateway", fmt.Sprintf("request failed status=504 totalCost=%dms", b.elapsed(start)), b.gap())
	}
	// 否则请求悬空：有 start 无 end
}

// 中断链路：少量前置操作后请求消失
func (b *traceBuilder) unfinishedFlow() {
	b.emit("INFO", "Gateway", fmt.Sprintf("request start userId=%d", b.uid), 0)
	if b.rng.Float64() < 0.7 {
		b.emitCost("INFO", "AuthService", "verify token", b.cost(5, 30))
	}
}

var malformedPool = []string{
	"@@@@ corrupted log line @@@@",
	"java.lang.NullPointerException: Cannot invoke method on null object",
	"\tat com.example.order.OrderService.query(OrderService.java:128)",
	"\tat com.example.gateway.GatewayHandler.handle(GatewayHandler.java:54)",
	"Exception in thread \"worker-3\" java.lang.RuntimeException: connection reset",
	"2026-08-10 ##BROKEN## INFO [Gateway] ??? malformed timestamp",
	"PK\x03\x04 binary garbage from compressed dump",
	"......",
}

var (
	reTraceBracket  = regexp.MustCompile(` \[traceId=[^\]]+\]`)
	reModuleBracket = regexp.MustCompile(`\[[A-Za-z]+\] `)
	reLevel         = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}) +(INFO|WARN|ERROR|DEBUG|FATAL) +`)
	reTimestamp     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3} +`)
)

// mutate 对部分字段缺失容错场景的模拟：随机移除/降级一个字段。
func mutate(rng *rand.Rand, txt string) string {
	switch rng.Intn(5) {
	case 0:
		return reTraceBracket.ReplaceAllString(txt, "")
	case 1:
		return reModuleBracket.ReplaceAllString(txt, "")
	case 2:
		return reLevel.ReplaceAllString(txt, "$1 ")
	case 3:
		return reTimestamp.ReplaceAllString(txt, "")
	default:
		return reLevel.ReplaceAllStringFunc(txt, func(m string) string {
			parts := reLevel.FindStringSubmatch(m)
			if len(parts) != 3 {
				return m
			}
			return parts[1] + " " + strings.ToLower(parts[2]) + " "
		})
	}
}

func main() {
	out := flag.String("out", "testdata/sample.log", "输出文件路径")
	target := flag.Int("lines", 10000, "目标行数（近似）")
	seed := flag.Int64("seed", 42, "随机种子（固定可复现）")
	flag.Parse()

	rng := rand.New(rand.NewSource(*seed))
	base := time.Date(2026, 8, 10, 10, 0, 0, 0, time.Local)

	var lines []line
	seq := 0
	add := func(t time.Time, txt string) {
		lines = append(lines, line{t: t, seq: seq, txt: txt})
		seq++
	}

	traceN := 0
	newTraceID := func() string {
		n := traceN
		traceN++
		return fmt.Sprintf("%c%c%c%03d", 'a'+byte((n/676)%26), 'a'+byte((n/26)%26), 'a'+byte(n%26), n%1000)
	}
	randomStart := func() time.Time {
		return base.Add(time.Duration(rng.Intn(30*60*1000)) * time.Millisecond)
	}
	// 数据库故障集中在一个 3 分钟窗口，制造时间趋势上的明显异常尖峰
	dbErrStart := func() time.Time {
		return base.Add(10*time.Minute + time.Duration(rng.Intn(180))*time.Second)
	}

	for len(lines) < *target {
		roll := rng.Float64()
		b := &traceBuilder{tid: newTraceID(), uid: 10000 + rng.Intn(90000), rng: rng, add: add}
		switch {
		case roll < 0.68: // 正常请求
			b.t = randomStart()
			if rng.Float64() < 0.5 {
				b.normalUserFlow()
			} else {
				b.normalOrderFlow()
			}
		case roll < 0.78: // 慢请求
			b.t = randomStart()
			b.slowFlow()
		case roll < 0.85: // 数据库超时
			b.t = dbErrStart()
			b.dbErrFlow()
		case roll < 0.91: // 网络异常
			b.t = randomStart()
			b.netErrFlow()
		case roll < 0.96: // 超时
			b.t = randomStart()
			b.timeoutFlow()
		default: // 请求中断
			b.t = randomStart()
			b.unfinishedFlow()
		}
		// 周期性心跳：无 traceId 的模块状态行
		if traceN%40 == 0 {
			t := randomStart()
			add(t, fmtLine(t, "INFO", "Cache",
				fmt.Sprintf("cache stats hit_rate=0.%02d connections=%d", 80+rng.Intn(19), rng.Intn(200)), ""))
		}
	}

	// 约 5% 的日志行做字段缺失/格式降级
	mutated := 0
	for i := range lines {
		if rng.Float64() < 0.05 {
			lines[i].txt = mutate(rng, lines[i].txt)
			mutated++
		}
	}

	// 按时间戳归并排序，模拟多协程写入的真实交错
	sort.SliceStable(lines, func(i, j int) bool {
		if !lines[i].t.Equal(lines[j].t) {
			return lines[i].t.Before(lines[j].t)
		}
		return lines[i].seq < lines[j].seq
	})

	// 约 2% 的完全乱格式行，插入随机位置
	malformed := int(float64(len(lines)) * 0.02)
	for i := 0; i < malformed; i++ {
		pos := rng.Intn(len(lines) + 1)
		l := line{txt: malformedPool[rng.Intn(len(malformedPool))]}
		lines = append(lines[:pos], append([]line{l}, lines[pos:]...)...)
	}

	fh, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "创建输出文件失败:", err)
		os.Exit(1)
	}
	defer fh.Close()
	w := bufio.NewWriter(fh)
	for _, l := range lines {
		_, _ = w.WriteString(l.txt + "\n")
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "写入失败:", err)
		os.Exit(1)
	}
	fmt.Printf("生成完成: %s  共 %d 行，%d 个 traceId（字段降级 %d 行，乱格式 %d 行）\n",
		*out, len(lines), traceN, mutated, malformed)
}
