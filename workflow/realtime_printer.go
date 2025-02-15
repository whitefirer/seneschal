package workflow

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// ANSI 控制码
const (
	ansiHome   = "\033[H"   // 光标到左上角
	ansiClear  = "\033[2J"  // 清屏
	ansiClrLn  = "\033[K"   // 清到行尾
	ansiHide   = "\033[?25l" // 隐藏光标
	ansiShow   = "\033[?25h" // 显示光标
)

var spin = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// stepRec 单步骤记录（去重用）
type stepRec struct {
	name, action, status, dur string
	order                     int
}

type stateStore struct {
	mu   sync.Mutex
	m    map[string]*stepRec
	keys []string
}

func newStateStore() *stateStore { return &stateStore{m: make(map[string]*stepRec)} }

func (s *stateStore) put(e ProgressEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := e.StepName
	if k == "" {
		k = e.Name
	}
	if r, ok := s.m[k]; ok {
		r.status = e.Status
		r.dur = e.Duration
	} else {
		s.m[k] = &stepRec{
			name: k, action: e.Action,
			status: e.Status, dur: e.Duration,
			order: len(s.keys),
		}
		s.keys = append(s.keys, k)
	}
}

func (s *stateStore) all() []stepRec {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]stepRec, len(s.keys))
	for i, k := range s.keys {
		out[i] = *s.m[k]
	}
	return out
}

// RealtimePrinter 实时进度打印机（纯 ANSI，无 TUI 依赖）
type RealtimePrinter struct {
	store     *stateStore
	sty       *ThemeStyle
	done      bool
	ticker    *time.Ticker
	stopCh    chan struct{}
	lastLines int
	frame     int
}

func NewRealtimePrinter(theme Theme) *RealtimePrinter {
	return &RealtimePrinter{
		store:  newStateStore(),
		sty:    NewThemeStyle(theme),
		stopCh: make(chan struct{}),
	}
}

func (p *RealtimePrinter) Start() {
	// 清屏 + 隐藏光标
	fmt.Fprint(os.Stdout, ansiClear+ansiHome+ansiHide)
	p.ticker = time.NewTicker(60 * time.Millisecond)
	go p.loop()
}

func (p *RealtimePrinter) loop() {
	for {
		select {
		case <-p.stopCh:
			return
		case <-p.ticker.C:
			p.render()
		}
	}
}

func (p *RealtimePrinter) Update(event ProgressEvent) {
	p.store.put(event)
}

// render 输出一帧
func (p *RealtimePrinter) render() {
	p.frame = (p.frame + 1) % len(spin)

	var b strings.Builder

	w := 80
	div := p.sty.Primary().Render(strings.Repeat("━", w))

	// 标题
	b.WriteString(div + "\n")
	title := p.sty.Primary().Bold(true).Render(" ⚡ goworkflow")
	if p.done {
		title += p.sty.Warning().Render("  [Ctrl-C 退出]")
	}
	b.WriteString(title + "\n" + div + "\n")

	// 步骤
	steps := p.store.all()
	doneCount, failCount, runCount := 0, 0, 0

	cap := 12
	start := 0
	if len(steps) > cap {
		start = len(steps) - cap
	}

	for i := start; i < len(steps); i++ {
		s := steps[i]
		b.WriteString(" " + p.stepLine(s) + "\n")
		switch s.status {
		case "completed", "success", "done":
			doneCount++
		case "failed":
			failCount++
		case "running":
			runCount++
		}
	}

	shown := len(steps) - start
	for i := shown; i < cap; i++ {
		b.WriteString(strings.Repeat(" ", w) + "\n")
	}

	// 底部统计
	b.WriteString(p.sty.Gray().Render(strings.Repeat("─", w)) + "\n")

	flag := " "
	if p.done {
		if failCount > 0 {
			flag = p.sty.Error().Render("✗")
		} else {
			flag = p.sty.Success().Render("✓")
		}
	} else if runCount > 0 {
		flag = p.sty.Primary().Render(spin[p.frame])
	}

	b.WriteString(" " + flag + " " +
		p.sty.Success().Render(fmt.Sprintf("%d done", doneCount)) + "  " +
		p.sty.Warning().Render(fmt.Sprintf("%d running", runCount)) + "  " +
		p.sty.Error().Render(fmt.Sprintf("%d failed", failCount)) + "  " +
		p.sty.Gray().Render(fmt.Sprintf("%d total", len(steps))))

	// 进度条
	if len(steps) > 0 {
		bw := 26
		r := float64(doneCount+failCount) / float64(len(steps))
		if r > 1 {
			r = 1
		}
		f := int(r * float64(bw))
		bar := strings.Repeat("█", f) + strings.Repeat("░", bw-f)
		if failCount > 0 {
			bar = p.sty.Error().Render(bar)
		} else if p.done {
			bar = p.sty.Success().Render(bar)
		} else {
			bar = p.sty.Primary().Render(bar)
		}
		b.WriteString("  " + bar + fmt.Sprintf(" %d%%", int(r*100)))
	}

	out := b.String()

	// 计算需要清除的旧行
	newLines := strings.Count(out, "\n")
	if p.lastLines > newLines {
		for i := 0; i < p.lastLines-newLines+1; i++ {
			out += ansiClrLn + "\n"
		}
	}
	p.lastLines = newLines

	// 移到顶 + 输出
	fmt.Fprint(os.Stdout, ansiHome+out)
}

func (p *RealtimePrinter) stepLine(s stepRec) string {
	var icon string
	switch s.status {
	case "completed", "success", "done":
		icon = p.sty.Success().Render("✓")
	case "failed":
		icon = p.sty.Error().Render("✗")
	case "running":
		icon = p.sty.Warning().Render(spin[p.frame])
	default:
		icon = p.sty.Gray().Render("·")
	}

	act := actionI(s.action, p.sty)
	nm := p.sty.Secondary().Render(truncS(s.name, 28))
	tag := p.sty.Gray().Render(fmt.Sprintf("%-8s", s.action))
	dur := ""
	if s.dur != "" {
		dur = p.sty.Gray().Render(" " + s.dur)
	}
	return fmt.Sprintf("%s %s %s %s%s", icon, act, nm, tag, dur)
}

func actionI(act string, sty *ThemeStyle) string {
	switch act {
	case "shell":
		return sty.Primary().Render("💻")
	case "http":
		return sty.Info().Render("🌐")
	case "log":
		return sty.Gray().Render("📢")
	case "sleep":
		return sty.Gray().Render("💤")
	case "condition":
		return sty.Secondary().Render("🔀")
	case "set":
		return sty.Gray().Render("📝")
	case "parallel":
		return sty.Primary().Render("⚡")
	case "foreach", "loop":
		return sty.Info().Render("↻")
	}
	return sty.Gray().Render("◦")
}

func truncS(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// Stop 停止动画并显示最终结果
func (p *RealtimePrinter) Stop(result *WorkflowResult, startTime, endTime string) {
	p.done = true
	// 最后刷新一帧
	p.render()
	// 停止定时器
	if p.ticker != nil {
		p.ticker.Stop()
	}
	close(p.stopCh)
	time.Sleep(100 * time.Millisecond)

	// 显示光标，换行输出最终结果
	fmt.Fprint(os.Stdout, ansiShow+"\r\n\n")
	printFinal2(p.sty, result, startTime, endTime)
}

func printFinal2(sty *ThemeStyle, result *WorkflowResult, startTime, endTime string) {
	div := strings.Repeat("━", 60)
	clr := sty.Success()
	lab := "✅ SUCCESS"
	if result.Status == "failed" {
		clr = sty.Error()
		lab = "❌ FAILED"
	}
	fmt.Println(clr.Render(div))
	fmt.Println(clr.Bold(true).Render("  " + lab))
	fmt.Println(clr.Render(div))
	fmt.Println()

	ok, bad, ct := 0, 0, 0
	for _, s := range result.Steps {
		if s.Action == "parallel" || s.Action == "foreach" {
			ct++
		}
		if s.Status == "failed" {
			bad++
		} else {
			ok++
		}
	}
	fmt.Printf("  📦 %d steps (%d containers)\n", len(result.Steps), ct)
	fmt.Printf("  %s %d passed\n", sty.Success().Render("✓"), ok-ct)
	if bad > 0 {
		fmt.Printf("  %s %d failed\n", sty.Error().Render("✗"), bad)
	}
	sa, _ := time.Parse(time.RFC3339, startTime)
	sb, _ := time.Parse(time.RFC3339, endTime)
	fmt.Printf("  ⏱  %s\n", sb.Sub(sa).Round(time.Millisecond))
	fmt.Println()
}
