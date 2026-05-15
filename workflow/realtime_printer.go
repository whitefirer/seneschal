package workflow

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// ── ANSI codes ──────────────────────────────────────────────────────────────

const (
	ansiHome    = "\033[H"
	ansiClear   = "\033[2J"
	ansiClrLn   = "\033[K"
	ansiHideCur = "\033[?25l"
	ansiShowCur = "\033[?25h"
	ansiAltOn   = "\033[?1049h"
	ansiAltOff  = "\033[?1049l"
)

var spin = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// ── Step record ─────────────────────────────────────────────────────────────

type stepRec struct {
	stepId, name, action, status, dur, output, parentId string
	depth                                               int
	order                                               int
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
		r.output = e.Output
		r.name = nm
	} else {
		s.m[k] = &stepRec{
			stepId: k, name: nm, action: e.Action,
			status: e.Status, dur: e.Duration, output: e.Output,
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

// ── RealtimePrinter ─────────────────────────────────────────────────────────

type RealtimePrinter struct {
	store    *stateStore
	sty      *ThemeStyle
	tuiStyle string

	frame   int
	done    bool
	scroll  int
	cursor  int
	selId   string
	detail  bool
	quitting bool

	termW int
	termH int

	result              *WorkflowResult
	startTime, endTime string

	ticker  *time.Ticker
	stopCh  chan struct{}
	lastOut string

	mu sync.Mutex

	// Terminal restore state
	origTermios *unix.Termios
}

func NewRealtimePrinter(theme Theme, tuiStyle string) *RealtimePrinter {
	if tuiStyle == "" {
		tuiStyle = "hermes"
	}
	return &RealtimePrinter{
		store:    newStateStore(),
		sty:      NewThemeStyle(theme),
		tuiStyle: tuiStyle,
		stopCh:   make(chan struct{}),
	}
}

// ── Lifecycle ───────────────────────────────────────────────────────────────

func (p *RealtimePrinter) Start() {
	// Enter raw mode for keyboard input
	p.enableRawMode()

	// Get terminal size
	if ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ); err == nil {
		p.termW = int(ws.Col)
		p.termH = int(ws.Row)
	}
	if p.termW < 20 {
		p.termW = 80
	}
	if p.termH < 8 {
		p.termH = 24
	}

	// Listen for SIGWINCH (resize)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	go func() {
		for range sigCh {
			if ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ); err == nil {
				p.mu.Lock()
				p.termW = int(ws.Col)
				p.termH = int(ws.Row)
				p.mu.Unlock()
			}
		}
	}()

	// Clear and hide cursor
	fmt.Fprint(os.Stdout, ansiClear+ansiHome+ansiHideCur)

	// Start ticker
	p.ticker = time.NewTicker(60 * time.Millisecond)
	go p.loop()

	// Start keyboard listener
	go p.keyLoop()
}

func (p *RealtimePrinter) loop() {
	for {
		select {
		case <-p.stopCh:
			return
		case <-p.ticker.C:
			p.frame = (p.frame + 1) % len(spin)
			p.render()
		}
	}
}

func (p *RealtimePrinter) Update(event ProgressEvent) {
	p.store.put(event)
}

// ── Keyboard ────────────────────────────────────────────────────────────────

func (p *RealtimePrinter) enableRawMode() {
	fd := int(os.Stdin.Fd())
	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return
	}
	p.origTermios = termios

	raw := *termios
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	unix.IoctlSetTermios(fd, unix.TCSETS, &raw)
}

func (p *RealtimePrinter) disableRawMode() {
	if p.origTermios != nil {
		unix.IoctlSetTermios(int(os.Stdin.Fd()), unix.TCSETS, p.origTermios)
	}
}

func (p *RealtimePrinter) keyLoop() {
	buf := make([]byte, 16)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return
		}
		p.handleKey(buf[:n])
	}
}

func (p *RealtimePrinter) handleKey(b []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Parse escape sequences
	switch {
	// Ctrl-C
	case len(b) == 1 && b[0] == 3:
		if p.detail {
			p.detail = false
		} else {
			p.quitting = true
			p.stopCh <- struct{}{}
			p.finalRender()
		}
		return

	// Escape
	case len(b) == 1 && b[0] == 27:
		if p.detail {
			p.detail = false
			p.selId = ""
		} else if p.selId != "" {
			p.selId = ""
		}
		return

	// Enter
	case len(b) == 1 && b[0] == 13:
		p.toggleDetail()
		return

	// Arrow keys / sequences
	case len(b) == 3 && b[0] == 27 && b[1] == 91:
		switch b[2] {
		case 65: // Up
			p.moveCursor(-1)
		case 66: // Down
			p.moveCursor(1)
		case 53: // PgUp (ESC [ 5 ~)
			if len(b) >= 4 && b[3] == 126 {
				p.scroll -= 10
				if p.scroll < 0 { p.scroll = 0 }
			}
		case 54: // PgDn
			if len(b) >= 4 && b[3] == 126 {
				steps := p.store.all()
				vis := p.visibleLines()
				p.scroll += 10
				maxScroll := len(steps) - vis
				if maxScroll < 0 { maxScroll = 0 }
				if p.scroll > maxScroll { p.scroll = maxScroll }
			}
		}
		return

	// Single char keys
	case len(b) == 1:
		switch b[0] {
		case 'k':
			p.moveCursor(-1)
		case 'j':
			p.moveCursor(1)
		case 'g':
			p.scroll = 0
			p.cursor = 0
		case 'G':
			steps := p.store.all()
			vis := p.visibleLines()
			p.scroll = len(steps) - vis
			if p.scroll < 0 { p.scroll = 0 }
			if len(steps) > 0 {
				p.cursor = len(steps) - 1
			}
		case 'q':
			if p.detail {
				p.detail = false
			} else {
				p.quitting = true
				p.stopCh <- struct{}{}
				p.finalRender()
			}
		}
	}
}

func (p *RealtimePrinter) moveCursor(delta int) {
	steps := p.store.all()
	newCur := p.cursor + delta
	if newCur < 0 { newCur = 0 }
	if newCur >= len(steps) { newCur = len(steps) - 1 }
	p.cursor = newCur

	vis := p.visibleLines()
	if p.cursor < p.scroll {
		p.scroll = p.cursor
	} else if p.cursor >= p.scroll+vis {
		p.scroll = p.cursor - vis + 1
	}
}

func (p *RealtimePrinter) toggleDetail() {
	steps := p.store.all()
	if p.cursor >= len(steps) { return }
	s := steps[p.cursor]
	if p.detail && p.selId == s.stepId {
		p.detail = false
		p.selId = ""
	} else {
		p.detail = true
		p.selId = s.stepId
	}
}

// ── Render ──────────────────────────────────────────────────────────────────

func (p *RealtimePrinter) visibleLines() int {
	h := 3 + 2 // header + footer
	avail := p.termH - h
	if avail < 1 { avail = 1 }
	return avail
}

func (p *RealtimePrinter) render() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.renderLocked()
}

func (p *RealtimePrinter) finalRender() {
	p.done = true
	p.store.flushRunning()
	p.renderLocked()
	// Show cursor and restore terminal
	fmt.Fprint(os.Stdout, ansiShowCur+"\r\n\n")
	p.printFinal()
	p.disableRawMode()
}

func (p *RealtimePrinter) renderLocked() {
	if p.quitting {
		return
	}
	var b strings.Builder

	if p.detail && p.selId != "" {
		b.WriteString(p.detailView())
	} else {
		b.WriteString(p.listView())
	}

	out := b.String()

	// Clear previous output
	if p.lastOut != "" {
		prevLines := strings.Count(p.lastOut, "\n")
		for i := 0; i < prevLines; i++ {
			out += ansiClrLn + "\n"
		}
	}
	p.lastOut = out

	fmt.Fprint(os.Stdout, ansiHome+out)
}

// ── List view ───────────────────────────────────────────────────────────────

func (p *RealtimePrinter) listView() string {
	var b strings.Builder
	prim := p.sty.Primary()
	gray := p.sty.Gray()
	w := p.termW
	inner := w - 2
	contentW := inner - 2

	// Header
	if p.tuiStyle == "hermes" {
		b.WriteString(prim.Render("╭" + strings.Repeat("─", inner) + "╮") + "\n")
	} else {
		b.WriteString(prim.Render(strings.Repeat("━", w)) + "\n")
	}

	title := prim.Bold(true).Render(" goworkflow")
	if p.done {
		b.WriteString(mkBoxLine(title+"  "+gray.Render("Ctrl-C to exit"), inner, p.tuiStyle) + "\n")
	} else {
		b.WriteString(mkBoxLine(title+"  "+gray.Render(spin[p.frame]+" Running..."), inner, p.tuiStyle) + "\n")
	}

	if p.tuiStyle == "hermes" {
		b.WriteString(prim.Render("├" + strings.Repeat("─", inner) + "┤") + "\n")
	} else {
		b.WriteString(prim.Render(strings.Repeat("━", w)) + "\n")
	}

	// Steps
	steps := p.store.all()
	vis := p.visibleLines()
	// Clamp scroll
	maxScroll := len(steps) - vis
	if maxScroll < 0 { maxScroll = 0 }
	if p.scroll > maxScroll { p.scroll = maxScroll }
	if p.scroll < 0 { p.scroll = 0 }

	children := map[string][]stepRec{}
	for i := range steps {
		s := &steps[i]
		if s.parentId != "" {
			children[s.parentId] = append(children[s.parentId], *s)
		}
	}

	doneC, failC, runC, parentTotal := 0, 0, 0, 0
	for _, s := range steps {
		if s.depth == 0 {
			parentTotal++
			switch s.status {
			case "completed", "success", "done": doneC++
			case "failed": failC++
			case "running": runC++
			}
		}
	}

	shown := 0
	for i := p.scroll; i < len(steps) && shown < vis; i++ {
		s := steps[i]
		childStats := ""
		if kids, ok := children[s.stepId]; ok && s.depth == 0 {
			okc, badc := 0, 0
			for _, k := range kids {
				if k.status == "failed" { badc++ } else if k.status != "running" { okc++ }
			}
			childStats = fmt.Sprintf("%d/%d", okc+badc, len(kids))
		}
		sel := i == p.cursor
		line := p.stepLine(s, childStats, sel)
		pad := contentW - termWidth(line)
		if pad < 0 { pad = 0 }
		if p.tuiStyle == "hermes" {
			b.WriteString("│ " + line + strings.Repeat(" ", pad) + " │\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
		shown++
	}
	for i := shown; i < vis; i++ {
		if p.tuiStyle == "hermes" {
			b.WriteString("│" + strings.Repeat(" ", inner) + "│\n")
		} else {
			b.WriteString("\n")
		}
	}

	// Status bar
	if p.tuiStyle == "hermes" {
		b.WriteString(prim.Render("├" + strings.Repeat("─", inner) + "┤") + "\n")
	} else {
		b.WriteString(prim.Render(strings.Repeat("━", w)) + "\n")
	}

	flag := gray.Render("○")
	if p.done {
		if failC > 0 {
			flag = p.sty.Error().Render("✗")
		} else {
			flag = p.sty.Success().Render("✓")
		}
	} else if runC > 0 {
		flag = prim.Render(spin[p.frame])
	}

	pct := 0
	if parentTotal > 0 {
		pct = int(float64(doneC+failC) / float64(parentTotal) * 100)
		if pct > 100 { pct = 100 }
	}
	bw := 16
	filled := pct * bw / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", bw-filled)
	if failC > 0 {
		bar = p.sty.Error().Render(bar)
	} else if p.done {
		bar = p.sty.Success().Render(bar)
	} else {
		bar = prim.Render(bar)
	}

	stats := flag + " " +
		p.sty.Success().Render(fmt.Sprintf("%d done", doneC)) + " · " +
		p.sty.Warning().Render(fmt.Sprintf("%d running", runC)) + " · " +
		p.sty.Error().Render(fmt.Sprintf("%d failed", failC)) + " · " +
		gray.Render(fmt.Sprintf("%d total", parentTotal)) +
		"  " + bar + fmt.Sprintf(" %d%%", pct)

	hint := " ↑↓:scroll  Enter:detail  q:quit"
	if p.tuiStyle == "hermes" {
		statsPad := contentW - len([]rune(stats+hint))
		if statsPad < 0 { statsPad = 0 }
		b.WriteString("│ " + stats + strings.Repeat(" ", statsPad) + hint + " │\n")
		b.WriteString(prim.Render("╰" + strings.Repeat("─", inner) + "╯") + "\n")
	} else {
		b.WriteString("  " + stats + "  " + gray.Render(hint) + "\n")
		b.WriteString(prim.Render(strings.Repeat("━", w)) + "\n")
	}

	return b.String()
}

// ── Detail view ─────────────────────────────────────────────────────────────

func (p *RealtimePrinter) detailView() string {
	var b strings.Builder
	prim := p.sty.Primary()
	w := p.termW
	inner := w - 2
	listW := w * 40 / 100
	if listW < 25 { listW = 25 }
	detailW := w - listW - 3 // -3 for borders

	b.WriteString(prim.Render("╭" + strings.Repeat("─", inner) + "╮") + "\n")

	title := prim.Bold(true).Render(" goworkflow")
	if p.done {
		b.WriteString(mkBoxLine(title+"  "+p.sty.Gray().Render("Ctrl-C to exit"), inner, p.tuiStyle) + "\n")
	} else {
		b.WriteString(mkBoxLine(title+"  "+p.sty.Gray().Render(spin[p.frame]+" Running..."), inner, p.tuiStyle) + "\n")
	}

	b.WriteString(prim.Render("├" + strings.Repeat("─", listW) + "┬" + strings.Repeat("─", detailW) + "┤") + "\n")

	// Steps (left)
	steps := p.store.all()
	visH := p.termH - 3 - 2 // header + footer
	if visH < 1 { visH = 1 }

	for i := p.scroll; i < len(steps) && (i-p.scroll) < visH; i++ {
		s := steps[i]
		sel := i == p.cursor
		line := p.shortStepLine(s, sel)
		pad := listW - termWidth(line)
		if pad < 0 { pad = 0 }
		b.WriteString("│" + line + strings.Repeat(" ", pad) + "│")

		if s.stepId == p.selId {
			b.WriteString(p.renderDetail(s, detailW))
		} else {
			b.WriteString(strings.Repeat(" ", detailW))
		}
		b.WriteString("\n")
	}
	for i := len(steps) - p.scroll; i < visH; i++ {
		b.WriteString("│" + strings.Repeat(" ", listW) + "│" + strings.Repeat(" ", detailW) + "\n")
	}

	b.WriteString(prim.Render("├" + strings.Repeat("─", listW) + "┴" + strings.Repeat("─", detailW) + "┤") + "\n")
	b.WriteString(mkBoxLine("  Esc:back  ↑↓:scroll  q:quit", inner, p.tuiStyle) + "\n")
	b.WriteString(prim.Render("╰" + strings.Repeat("─", inner) + "╯") + "\n")

	return b.String()
}

func (p *RealtimePrinter) renderDetail(s stepRec, width int) string {
	pad := width - 2
	if pad < 10 { pad = 10 }

	title := " " + p.sty.Primary().Bold(true).Render("STEP DETAIL")
	div := p.sty.Gray().Render(strings.Repeat("─", pad-utf8.RuneCountInString(" STEP DETAIL ")))
	out := title + div + "\n\n"

	out += "  " + p.sty.Gray().Render("Name:") + "      " + p.sty.Primary().Render(truncS(s.name, 25)) + "\n"
	out += "  " + p.sty.Gray().Render("Action:") + "    " + p.sty.Gray().Render(s.action) + "\n"
	out += "  " + p.sty.Gray().Render("Status:") + "    " + statusRender(s.status, p.sty) + "\n"
	if s.dur != "" {
		out += "  " + p.sty.Gray().Render("Duration:") + "  " + p.sty.Info().Render(s.dur) + "\n"
	}
	if s.output != "" {
		out += "\n " + p.sty.Gray().Render(strings.Repeat("─", pad-1)) + "\n"
		output := s.output
		if len(output) > 400 {
			output = output[:400] + "..."
		}
		out += " " + p.sty.Gray().Render(output)
	}
	return out
}

func statusRender(st string, sty *ThemeStyle) string {
	switch st {
	case "completed", "success", "done": return sty.Success().Render(st)
	case "failed": return sty.Error().Render(st)
	case "running": return sty.Warning().Render(st)
	}
	return sty.Gray().Render(st)
}

// ── Step lines ──────────────────────────────────────────────────────────────

func (p *RealtimePrinter) stepLine(s stepRec, childStats string, sel bool) string {
	indent := strings.Repeat("  ", s.depth)
	nameW := 26 - s.depth*2
	if nameW < 5 { nameW = 5 }

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
	nm := p.sty.Primary().Render(truncS(s.name, nameW))
	tag := p.sty.Gray().Render(fmt.Sprintf("%-7s", s.action))
	if childStats != "" {
		tag = p.sty.Gray().Render(childStats)
	}
	dur := ""
	if s.dur != "" {
		dur = p.sty.Info().Render(" " + s.dur)
	}

	line := fmt.Sprintf("%s%s %s %s %s%s", indent, icon, actIcon, nm, tag, dur)
	if sel {
		line = "\033[48;5;236m" + line + "\033[0m"
	}
	return line
}

func (p *RealtimePrinter) shortStepLine(s stepRec, sel bool) string {
	indent := strings.Repeat("  ", s.depth)
	nameW := 22 - s.depth*2
	if nameW < 5 { nameW = 5 }

	icon := "○"
	switch s.status {
	case "completed", "success", "done": icon = "✓"
	case "failed": icon = "✗"
	case "running": icon = spin[p.frame]
	}

	line := fmt.Sprintf("%s%s %s", indent, icon, truncS(s.name, nameW))
	if sel {
		line = "\033[48;5;236m" + line + "\033[0m"
	}
	return line
}

// ── Final ───────────────────────────────────────────────────────────────────

func (p *RealtimePrinter) Stop(result *WorkflowResult, startTime, endTime string) {
	p.result = result
	p.startTime = startTime
	p.endTime = endTime
	p.quitting = true
	p.stopCh <- struct{}{}

	if p.ticker != nil {
		p.ticker.Stop()
	}

	p.finalRender()
	p.disableRawMode()
	time.Sleep(100 * time.Millisecond)
}

func (p *RealtimePrinter) printFinal() {
	ok, bad := 0, 0
	for _, s := range p.result.Steps {
		if s.Status == "failed" { bad++ } else { ok++ }
	}

	statusStyle := p.sty.Success()
	statusIcon := "✓"
	statusLabel := "SUCCESS"
	if p.result.Status == "failed" {
		statusStyle = p.sty.Error()
		statusIcon = "✗"
		statusLabel = "FAILED"
	}

	st, _ := time.Parse(time.RFC3339, p.startTime)
	et, _ := time.Parse(time.RFC3339, p.endTime)
	dur := et.Sub(st).Round(time.Millisecond).String()

	var lines []string
	lines = append(lines, statusStyle.Bold(true).Render(fmt.Sprintf("  %s %s", statusIcon, statusLabel)))
	lines = append(lines, fmt.Sprintf("  %s/%s steps · %s",
		p.sty.Success().Render(fmt.Sprintf("%d", ok)),
		p.sty.Gray().Render(fmt.Sprintf("%d", len(p.result.Steps))),
		p.sty.Info().Render(dur)))
	if bad > 0 {
		lines = append(lines, p.sty.Error().Render(fmt.Sprintf("  %d failed", bad)))
	}

	if p.tuiStyle == "claude" {
		for _, l := range lines { fmt.Println(l) }
	} else {
		fmt.Println(p.sty.BoxStyle().Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func mkBoxLine(content string, width int, style string) string {
	if style == "hermes" {
		return "│" + content + strings.Repeat(" ", width-termWidth(content)) + "│"
	}
	return content
}

func actionIconTUI(act string, sty *ThemeStyle) string {
	switch act {
	case "shell": return sty.Info().Render("◇")
	case "http": return sty.Info().Render("○")
	case "log": return sty.Gray().Render("◆")
	case "sleep": return sty.Gray().Render("◌")
	case "condition": return sty.Warning().Render("◇")
	case "set": return sty.Gray().Render("◦")
	case "parallel": return sty.Info().Render("◎")
	case "foreach", "loop": return sty.Info().Render("◈")
	}
	return sty.Gray().Render("◦")
}

func truncS(s string, n int) string {
	if n < 3 { n = 3 }
	if len(s) <= n { return s }
	return s[:n-1] + "…"
}

func stripAnsi(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	skip := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\033' { skip = true; continue }
		if skip { if s[i] == 'm' { skip = false }; continue }
		b.WriteByte(s[i])
	}
	return b.String()
}

func termWidth(s string) int {
	return len([]rune(stripAnsi(s)))
}
