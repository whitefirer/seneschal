package workflow

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	ansiHome  = "\033[H"
	ansiClear = "\033[2J"
	ansiClrLn = "\033[K"
	ansiHide  = "\033[?25l"
	ansiShow  = "\033[?25h"
)

var spin = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type stepRec struct {
	stepId, name, action, status, dur, parentId string
	depth                                       int
	order                                       int
}

type stateStore struct {
	mu   sync.Mutex
	m    map[string]*stepRec
	keys []string
}

func newStateStore() *stateStore { return &stateStore{m: make(map[string]*stepRec)} }

func (s *stateStore) put(e ProgressEvent) {
	if e.Action == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := e.StepId
	if k == "" {
		k = e.Name
	}
	nm := e.Name
	if e.StepName != "" {
		nm = e.StepName
	}
	if r, ok := s.m[k]; ok {
		r.status = e.Status
		r.dur = e.Duration
		r.name = nm
	} else {
		s.m[k] = &stepRec{
			stepId: k, name: nm, action: e.Action,
			status: e.Status, dur: e.Duration,
			depth: e.Depth, parentId: e.ParentId,
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

func (s *stateStore) flushRunning() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.m {
		if r.status == "running" {
			r.status = "completed"
		}
	}
}

type RealtimePrinter struct {
	store     *stateStore
	sty       *ThemeStyle
	done      bool
	ticker    *time.Ticker
	stopCh    chan struct{}
	lastLines int
	frame     int
	tuiStyle  string
}

func NewRealtimePrinter(theme Theme, tuiStyle string) *RealtimePrinter {
	if tuiStyle == "" {
		tuiStyle = "hermes"
	}
	return &RealtimePrinter{
		store:    newStateStore(),
		sty:      NewThemeStyle(theme),
		stopCh:   make(chan struct{}),
		tuiStyle: tuiStyle,
	}
}

func (p *RealtimePrinter) Start() {
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

func (p *RealtimePrinter) render() {
	if p.tuiStyle == "claude" {
		p.renderClaude()
		return
	}
	p.renderHermes()
}

func (p *RealtimePrinter) renderHermes() {
	p.frame = (p.frame + 1) % len(spin)

	var b strings.Builder
	w := 72
	inner := w - 2
	contentW := inner - 2
	prim := p.sty.Primary()

	b.WriteString(prim.Render("╭" + strings.Repeat("─", inner) + "╮") + "\n")

	titleLeft := prim.Bold(true).Render(" goworkflow")
	if p.done {
		titleRight := p.sty.Gray().Render("Ctrl-C to exit")
		spacer := inner - termWidth(titleLeft) - termWidth(titleRight) - 2
		if spacer < 0 { spacer = 0 }
		b.WriteString("│" + titleLeft + strings.Repeat(" ", spacer) + "  " + titleRight + "│\n")
	} else {
		titleRight := p.sty.Gray().Render(spin[p.frame] + " Running...")
		spacer := inner - termWidth(titleLeft) - termWidth(titleRight) - 2
		if spacer < 0 { spacer = 0 }
		b.WriteString("│" + titleLeft + strings.Repeat(" ", spacer) + "  " + titleRight + "│\n")
	}
	b.WriteString(prim.Render("├" + strings.Repeat("─", inner) + "┤") + "\n")

	// Steps
	steps := p.store.all()
	doneCount, failCount, runCount := 0, 0, 0
	cap := 12
	start := 0
	if len(steps) > cap {
		start = len(steps) - cap
	}

	// Build children map for container stats
	children := map[string][]stepRec{}
	for i := range steps {
		s := &steps[i]
		if s.parentId != "" {
			children[s.parentId] = append(children[s.parentId], *s)
		}
	}

	// Count ALL depth-0 steps (not just visible window)
	parentTotal := 0
	for _, s := range steps {
		if s.depth == 0 {
			parentTotal++
			switch s.status {
			case "completed", "success", "done":
				doneCount++
			case "failed":
				failCount++
			case "running":
				runCount++
			}
		}
	}

	for i := start; i < len(steps); i++ {
		s := steps[i]
		childStats := ""
		if kids, ok := children[s.stepId]; ok && s.depth == 0 {
			okc, badc := 0, 0
			for _, k := range kids {
				if k.status == "failed" { badc++ } else if k.status != "running" { okc++ }
			}
			childStats = fmt.Sprintf("%d/%d", okc+badc, len(kids))
		}

		line := p.stepLine(s, childStats)
		pad := contentW - termWidth(line)
		if pad < 0 { pad = 0 }
		b.WriteString("│ " + line + strings.Repeat(" ", pad) + " │\n")
	}

	shown := len(steps) - start
	for i := shown; i < cap; i++ {
		b.WriteString("│" + strings.Repeat(" ", inner) + "│\n")
	}

	// Status bar
	b.WriteString(prim.Render("├" + strings.Repeat("─", inner) + "┤") + "\n")

	flag := p.sty.Gray().Render("○")
	if p.done {
		if failCount > 0 {
			flag = p.sty.Error().Render("✗")
		} else {
			flag = p.sty.Success().Render("✓")
		}
	} else if runCount > 0 {
		flag = prim.Render(spin[p.frame])
	}

	pct := 0
	if parentTotal > 0 {
		pct = int(float64(doneCount+failCount) / float64(parentTotal) * 100)
		if pct > 100 { pct = 100 }
	}

	bw := 16
	filled := pct * bw / 100
	barPlain := strings.Repeat("█", filled) + strings.Repeat("░", bw-filled)
	if failCount > 0 {
		barPlain = p.sty.Error().Render(barPlain)
	} else if p.done {
		barPlain = p.sty.Success().Render(barPlain)
	} else {
		barPlain = prim.Render(barPlain)
	}

	counts := fmt.Sprintf("○ %d done · %d running · %d failed · %d total  %s %d%%",
		doneCount, runCount, failCount, parentTotal,
		strings.Repeat("█", filled)+strings.Repeat("░", bw-filled),
		pct)
	statusPad := contentW - len([]rune(counts))
	if statusPad < 0 { statusPad = 0 }

	line := flag + " " +
		p.sty.Success().Render(fmt.Sprintf("%d done", doneCount)) + " · " +
		p.sty.Warning().Render(fmt.Sprintf("%d running", runCount)) + " · " +
		p.sty.Error().Render(fmt.Sprintf("%d failed", failCount)) + " · " +
		p.sty.Gray().Render(fmt.Sprintf("%d total", parentTotal)) +
		"  " + barPlain + fmt.Sprintf(" %d%%", pct)

	b.WriteString("│ " + line + strings.Repeat(" ", statusPad) + " │\n")
	b.WriteString(prim.Render("╰" + strings.Repeat("─", inner) + "╯") + "\n")

	out := b.String()
	newLines := strings.Count(out, "\n")
	if p.lastLines > newLines {
		for i := 0; i < p.lastLines-newLines+1; i++ {
			out += ansiClrLn + "\n"
		}
	}
	p.lastLines = newLines
	fmt.Fprint(os.Stdout, ansiHome+out)
}

func (p *RealtimePrinter) renderClaude() {
	p.frame = (p.frame + 1) % len(spin)

	var b strings.Builder
	w := 72
	prim := p.sty.Primary()
	rule := prim.Render(strings.Repeat("━", w))

	b.WriteString("\n")
	b.WriteString(rule + "\n")

	titleLeft := prim.Bold(true).Render("  goworkflow")
	if p.done {
		b.WriteString(titleLeft + "\n")
	} else {
		right := p.sty.Gray().Render(spin[p.frame] + " Running...")
		b.WriteString(titleLeft + "  " + right + "\n")
	}
	b.WriteString(rule + "\n")
	b.WriteString("\n")

	steps := p.store.all()
	doneCount, failCount, runCount := 0, 0, 0
	cap := 12
	start := 0
	if len(steps) > cap {
		start = len(steps) - cap
	}

	children := map[string][]stepRec{}
	for i := range steps {
		s := &steps[i]
		if s.parentId != "" {
			children[s.parentId] = append(children[s.parentId], *s)
		}
	}

	parentTotalCC := 0
	for _, s := range steps {
		if s.depth == 0 {
			parentTotalCC++
			switch s.status {
			case "completed", "success", "done":
				doneCount++
			case "failed":
				failCount++
			case "running":
				runCount++
			}
		}
	}

	for i := start; i < len(steps); i++ {
		s := steps[i]
		line := p.stepLineCC(s)
		b.WriteString("  " + line + "\n")
	}

	shown := len(steps) - start
	for i := shown; i < cap; i++ {
		b.WriteString("\n")
	}
	b.WriteString("\n")

	b.WriteString(rule + "\n")

	flag := p.sty.Gray().Render("○")
	if p.done {
		if failCount > 0 {
			flag = p.sty.Error().Render("✗")
		} else {
			flag = p.sty.Success().Render("✓")
		}
	} else if runCount > 0 {
		flag = prim.Render(spin[p.frame])
	}

	pct := 0
	if parentTotalCC > 0 {
		pct = int(float64(doneCount+failCount) / float64(parentTotalCC) * 100)
		if pct > 100 { pct = 100 }
	}

	bw := 16
	filled := pct * bw / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", bw-filled)
	if failCount > 0 {
		bar = p.sty.Error().Render(bar)
	} else if p.done {
		bar = p.sty.Success().Render(bar)
	} else {
		bar = prim.Render(bar)
	}

	stats := "  " + flag + " " +
		p.sty.Success().Render(fmt.Sprintf("%d done", doneCount)) + " · " +
		p.sty.Warning().Render(fmt.Sprintf("%d running", runCount)) + " · " +
		p.sty.Error().Render(fmt.Sprintf("%d failed", failCount)) + " · " +
		p.sty.Gray().Render(fmt.Sprintf("%d total", parentTotalCC))

	b.WriteString(stats + "    " + bar + fmt.Sprintf(" %d%%", pct) + "\n")
	b.WriteString(rule + "\n")

	out := b.String()
	newLines := strings.Count(out, "\n")
	if p.lastLines > newLines {
		for i := 0; i < p.lastLines-newLines+1; i++ {
			out += ansiClrLn + "\n"
		}
	}
	p.lastLines = newLines
	fmt.Fprint(os.Stdout, ansiHome+out)
}

func (p *RealtimePrinter) stepLine(s stepRec, childStats string) string {
	indent := strings.Repeat("  ", s.depth)
	icon := p.sty.Gray().Render("○")
	switch s.status {
	case "completed", "success", "done":
		icon = p.sty.Success().Render("✓")
	case "failed":
		icon = p.sty.Error().Render("✗")
	case "running":
		icon = p.sty.Warning().Render(spin[p.frame])
	}

	actIcon := actionIconTUI(s.action, p.sty)
	nm := p.sty.Primary().Render(truncS(s.name, 22-s.depth*2))
	tag := p.sty.Gray().Render(fmt.Sprintf("%-7s", s.action))
	if childStats != "" {
		tag = p.sty.Gray().Render(childStats)
	}
	dur := ""
	if s.dur != "" {
		dur = p.sty.Info().Render(" " + s.dur)
	}
	return fmt.Sprintf("%s%s %s %s %s%s", indent, icon, actIcon, nm, tag, dur)
}

func (p *RealtimePrinter) stepLineCC(s stepRec) string {
	indent := strings.Repeat("  ", s.depth)
	icon := p.sty.Gray().Render("○")
	switch s.status {
	case "completed", "success", "done":
		icon = p.sty.Success().Render("✓")
	case "failed":
		icon = p.sty.Error().Render("✗")
	case "running":
		icon = p.sty.Warning().Render(spin[p.frame])
	}
	nm := p.sty.Primary().Render(padRight(truncS(s.name, 24-s.depth*2), 26-s.depth*2))
	tag := p.sty.Gray().Render(fmt.Sprintf("%-10s", s.action))
	dur := ""
	if s.dur != "" {
		dur = p.sty.Info().Render(s.dur)
	}
	return fmt.Sprintf("%s%s %s %s %s", indent, icon, nm, tag, dur)
}

func actionIconTUI(act string, sty *ThemeStyle) string {
	switch act {
	case "shell":
		return sty.Info().Render("◇")
	case "http":
		return sty.Info().Render("○")
	case "log":
		return sty.Gray().Render("◆")
	case "sleep":
		return sty.Gray().Render("◌")
	case "condition":
		return sty.Warning().Render("◇")
	case "set":
		return sty.Gray().Render("◦")
	case "parallel":
		return sty.Info().Render("◎")
	case "foreach", "loop":
		return sty.Info().Render("◈")
	}
	return sty.Gray().Render("◦")
}

func truncS(s string, n int) string {
	if len(s) <= n { return s }
	return s[:n-1] + "…"
}

func padRight(s string, n int) string {
	rl := len([]rune(s))
	if rl >= n { return s }
	return s + strings.Repeat(" ", n-rl)
}

func stripAnsi(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	skip := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\033' {
			skip = true
			continue
		}
		if skip {
			if s[i] == 'm' { skip = false }
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func termWidth(s string) int {
	return len([]rune(stripAnsi(s)))
}

func (p *RealtimePrinter) Stop(result *WorkflowResult, startTime, endTime string) {
	p.done = true
	p.store.flushRunning()
	p.render()
	if p.ticker != nil {
		p.ticker.Stop()
	}
	close(p.stopCh)
	time.Sleep(80 * time.Millisecond)

	fmt.Fprint(os.Stdout, ansiShow+"\r\n\n")
	printFinal(p.sty, p.tuiStyle, result, startTime, endTime)
}

func printFinal(sty *ThemeStyle, tuiStyle string, result *WorkflowResult, startTime, endTime string) {
	ok, bad := 0, 0
	for _, s := range result.Steps {
		if s.Status == "failed" { bad++ } else { ok++ }
	}

	statusStyle := sty.Success()
	statusIcon := "✓"
	statusLabel := "SUCCESS"
	if result.Status == "failed" {
		statusStyle = sty.Error()
		statusIcon = "✗"
		statusLabel = "FAILED"
	}

	start, _ := time.Parse(time.RFC3339, startTime)
	end, _ := time.Parse(time.RFC3339, endTime)
	dur := end.Sub(start).Round(time.Millisecond).String()

	var lines []string
	lines = append(lines, statusStyle.Bold(true).Render(fmt.Sprintf("  %s %s", statusIcon, statusLabel)))
	stats := fmt.Sprintf("  %s/%s steps · %s",
		sty.Success().Render(fmt.Sprintf("%d", ok)),
		sty.Gray().Render(fmt.Sprintf("%d", len(result.Steps))),
		sty.Info().Render(dur))
	lines = append(lines, stats)
	if bad > 0 {
		lines = append(lines, sty.Error().Render(fmt.Sprintf("  %d failed", bad)))
	}

	if tuiStyle == "claude" {
		for _, l := range lines { fmt.Println(l) }
	} else {
		fmt.Println(sty.BoxStyle().Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
}
