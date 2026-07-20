package workflow

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	// ai_token is an incremental event: append the token to the step's output
	// rather than overwriting it, so the detail view shows streaming text.
	if e.Type == "ai_token" {
		if r, ok := s.m[k]; ok {
			r.output += e.Output
			return
		}
		// First token before step_start registered the row: create it.
		s.m[k] = &stepRec{
			stepId: k, name: nm, action: e.Action,
			status: "running", output: e.Output,
			depth: e.Depth, parentId: e.ParentId,
			order: len(s.keys),
		}
		s.keys = append(s.keys, k)
		return
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
		if r.status == StatusRunning {
			r.status = StatusCompleted
		}
	}
}

// ── bubbletea model ─────────────────────────────────────────────────────────

type tickMsg time.Time
type doneMsg struct{}

type model struct {
	store    *stateStore
	sty      *ThemeStyle
	tuiStyle string
	frame    int
	done     bool
	quitting bool

	scroll int
	cursor int
	selId  string
	detail bool

	termW int
	termH int

	result             *WorkflowResult
	startTime, endTime string

	events chan ProgressEvent
	stopCh chan struct{}
}

// ── RealtimePrinter ─────────────────────────────────────────────────────────

type RealtimePrinter struct {
	EventCh  chan ProgressEvent
	store    *stateStore
	sty      *ThemeStyle
	tuiStyle string
	stopCh   chan struct{}
	// mu guards result/sTime/eTime: Stop is called from the workflow
	// goroutine while Run reads them on the TUI goroutine.
	mu     sync.Mutex
	result *WorkflowResult
	sTime  string
	eTime  string
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

func (p *RealtimePrinter) SetEventChannel(ch chan ProgressEvent) {
	p.EventCh = ch
}

// EventChannel returns the channel the Executor pushes progress events into
// (EventStreamer interface).
func (p *RealtimePrinter) EventChannel() chan ProgressEvent {
	return p.EventCh
}

// ── Printer interface (no-ops) ──────────────────────────────────────────────
// The TUI renders entirely from the event channel; the Executor's direct
// Print* calls are intentionally ignored here. Its lifecycle lives in
// Run/Stop (Runner interface) instead.

func (p *RealtimePrinter) PrintHeader(*Workflow) {}
func (p *RealtimePrinter) PrintStep(Step, int)   {}
func (p *RealtimePrinter) PrintStepResult(string, string, string, string, int) {
}
func (p *RealtimePrinter) PrintFooter(*WorkflowResult, string, string) {}
func (p *RealtimePrinter) PrintShell(string, string, int)              {}
func (p *RealtimePrinter) PrintLog(string, string, string, int)        {}
func (p *RealtimePrinter) PrintHTTPRequest(string, string)             {}
func (p *RealtimePrinter) PrintHTTPCall(string, string, int, time.Duration) {
}
func (p *RealtimePrinter) PrintCondition(string, bool) {}
func (p *RealtimePrinter) PrintSleep(string)           {}
func (p *RealtimePrinter) PrintForeach(int, string)    {}

// Run starts the bubbletea TUI on the current goroutine. It blocks until the user quits.
func (p *RealtimePrinter) Run() {
	res, sTime, eTime := p.resultSnapshot()
	m := &model{
		store:     p.store,
		sty:       p.sty,
		tuiStyle:  p.tuiStyle,
		events:    p.EventCh,
		stopCh:    p.stopCh,
		termW:     80,
		termH:     24,
		result:    res,
		startTime: sTime,
		endTime:   eTime,
	}
	prog := tea.NewProgram(m)
	if _, err := prog.Run(); err != nil {
		res, sTime, eTime = p.resultSnapshot()
		if res != nil {
			fmt.Print(renderFinalResult(p.sty, p.tuiStyle, res, sTime, eTime))
		}
	}
}

// Stop signals the TUI that the workflow is done and stores result data.
func (p *RealtimePrinter) Stop(result *WorkflowResult, startTime, endTime string) {
	p.mu.Lock()
	p.result = result
	p.sTime = startTime
	p.eTime = endTime
	p.mu.Unlock()
	close(p.stopCh)
}

// resultSnapshot returns the stored result data under the lock (Stop runs on
// the workflow goroutine, Run on the TUI goroutine).
func (p *RealtimePrinter) resultSnapshot() (*WorkflowResult, string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result, p.sTime, p.eTime
}

// ── Init ────────────────────────────────────────────────────────────────────

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		listenEvents(m.events),
		tickCmd(),
		waitDone(m.stopCh),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(60*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func listenEvents(ch <-chan ProgressEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return doneMsg{}
		}
		return ev
	}
}

func waitDone(ch chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-ch
		return doneMsg{}
	}
}

// ── Update ──────────────────────────────────────────────────────────────────

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.termW = msg.Width
		m.termH = msg.Height
		return m, nil
	case ProgressEvent:
		m.store.put(msg)
		return m, listenEvents(m.events)
	case tickMsg:
		m.frame = (m.frame + 1) % len(spin)
		if m.done {
			return m, nil // stop ticking when done
		}
		return m, tickCmd()
	case doneMsg:
		m.done = true
		m.store.flushRunning()
		return m, nil
	case tea.QuitMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q", "ctrl+c":
		if m.detail {
			m.detail = false
			m.selId = ""
		} else {
			m.quitting = true
			return tea.Quit
		}
	case "esc":
		if m.detail {
			m.detail = false
			m.selId = ""
		} else if m.selId != "" {
			m.selId = ""
		}
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "pgup":
		m.scroll -= 10
		if m.scroll < 0 {
			m.scroll = 0
		}
	case "pgdown":
		steps := m.store.all()
		vis := m.visibleLines()
		m.scroll += 10
		maxScroll := len(steps) - vis
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.scroll > maxScroll {
			m.scroll = maxScroll
		}
	case "g":
		m.scroll = 0
		m.cursor = 0
	case "G":
		steps := m.store.all()
		vis := m.visibleLines()
		m.scroll = len(steps) - vis
		if m.scroll < 0 {
			m.scroll = 0
		}
		if len(steps) > 0 {
			m.cursor = len(steps) - 1
		}
	case "enter":
		m.toggleDetail()
	}
	return nil
}

func (m *model) moveCursor(delta int) {
	steps := m.store.all()
	newCur := m.cursor + delta
	if newCur < 0 {
		newCur = 0
	}
	if newCur >= len(steps) {
		newCur = len(steps) - 1
	}
	m.cursor = newCur
	vis := m.visibleLines()
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	} else if m.cursor >= m.scroll+vis {
		m.scroll = m.cursor - vis + 1
	}
}

func (m *model) toggleDetail() {
	steps := m.store.all()
	if m.cursor >= len(steps) {
		return
	}
	s := steps[m.cursor]
	if m.detail && m.selId == s.stepId {
		m.detail = false
		m.selId = ""
	} else {
		m.detail = true
		m.selId = s.stepId
	}
}

// ── View ────────────────────────────────────────────────────────────────────

func (m *model) visibleLines() int {
	h := 3 + 2
	avail := m.termH - h
	if avail < 1 {
		avail = 1
	}
	return avail
}

func (m *model) View() string {
	if m.termW < 20 {
		m.termW = 80
	}
	if m.termH < 8 {
		m.termH = 24
	}

	if m.quitting {
		return m.finalView()
	}
	if m.detail && m.selId != "" {
		return m.detailView()
	}
	return m.listView()
}

// ── List view ───────────────────────────────────────────────────────────────

func (m *model) listView() string {
	var b strings.Builder
	prim := m.sty.Primary()
	gray := m.sty.Gray()
	w := m.termW
	inner := w - 2
	contentW := inner - 2

	if m.tuiStyle == "hermes" {
		b.WriteString(prim.Render("╭"+strings.Repeat("─", inner)+"╮") + "\n")
	} else {
		b.WriteString(prim.Render(strings.Repeat("━", w)) + "\n")
	}

	title := prim.Bold(true).Render(" seneschal")
	if m.done {
		b.WriteString(hermesLine(title+"  "+gray.Render("Ctrl-C to exit"), inner, m.tuiStyle) + "\n")
	} else {
		b.WriteString(hermesLine(title+"  "+gray.Render(spin[m.frame]+" Running..."), inner, m.tuiStyle) + "\n")
	}

	if m.tuiStyle == "hermes" {
		b.WriteString(prim.Render("├"+strings.Repeat("─", inner)+"┤") + "\n")
	} else {
		b.WriteString(prim.Render(strings.Repeat("━", w)) + "\n")
	}

	steps := m.store.all()
	vis := m.visibleLines()
	maxScroll := len(steps) - vis
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
	if m.scroll < 0 {
		m.scroll = 0
	}

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
			switch {
			case isSuccessStatus(s.status):
				doneC++
			case s.status == StatusFailed:
				failC++
			case s.status == StatusRunning:
				runC++
			}
		}
	}

	shown := 0
	for i := m.scroll; i < len(steps) && shown < vis; i++ {
		s := steps[i]
		childStats := ""
		if kids, ok := children[s.stepId]; ok && s.depth == 0 {
			okc, badc := 0, 0
			for _, k := range kids {
				if k.status == "failed" {
					badc++
				} else if k.status != "running" {
					okc++
				}
			}
			childStats = fmt.Sprintf("%d/%d", okc+badc, len(kids))
		}
		sel := i == m.cursor
		line := m.stepLine(s, childStats, sel, contentW)
		if m.tuiStyle == "hermes" {
			b.WriteString("│ " + line + " │\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
		shown++
	}
	for i := shown; i < vis; i++ {
		if m.tuiStyle == "hermes" {
			b.WriteString("│" + strings.Repeat(" ", inner) + "│\n")
		} else {
			b.WriteString("\n")
		}
	}

	if m.tuiStyle == "hermes" {
		b.WriteString(prim.Render("├"+strings.Repeat("─", inner)+"┤") + "\n")
	} else {
		b.WriteString(prim.Render(strings.Repeat("━", w)) + "\n")
	}

	flag := gray.Render("○")
	if m.done {
		if failC > 0 {
			flag = m.sty.Error().Render("✗")
		} else {
			flag = m.sty.Success().Render("✓")
		}
	} else if runC > 0 {
		flag = prim.Render(spin[m.frame])
	}

	pct := 0
	if parentTotal > 0 {
		pct = int(float64(doneC+failC) / float64(parentTotal) * 100)
		if pct > 100 {
			pct = 100
		}
	}
	bw := 16
	filled := pct * bw / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", bw-filled)
	if failC > 0 {
		bar = m.sty.Error().Render(bar)
	} else if m.done {
		bar = m.sty.Success().Render(bar)
	} else {
		bar = prim.Render(bar)
	}

	stats := flag + " " +
		m.sty.Success().Render(fmt.Sprintf("%d done", doneC)) + " · " +
		m.sty.Warning().Render(fmt.Sprintf("%d running", runC)) + " · " +
		m.sty.Error().Render(fmt.Sprintf("%d failed", failC)) + " · " +
		gray.Render(fmt.Sprintf("%d total", parentTotal)) +
		"  " + bar + fmt.Sprintf(" %d%%", pct)

	hint := " ↑↓:scroll  Enter:detail  q:quit"
	if m.tuiStyle == "hermes" {
		statsPad := contentW - len([]rune(stats+hint))
		if statsPad < 0 {
			statsPad = 0
		}
		b.WriteString("│ " + stats + strings.Repeat(" ", statsPad) + hint + " │\n")
		b.WriteString(prim.Render("╰"+strings.Repeat("─", inner)+"╯") + "\n")
	} else {
		b.WriteString("  " + stats + "  " + gray.Render(hint) + "\n")
		b.WriteString(prim.Render(strings.Repeat("━", w)) + "\n")
	}

	return b.String()
}

// ── Detail view ─────────────────────────────────────────────────────────────

func (m *model) detailView() string {
	steps := m.store.all()
	var s stepRec
	for _, st := range steps {
		if st.stepId == m.selId {
			s = st
			break
		}
	}

	var b strings.Builder
	prim := m.sty.Primary()
	w := m.termW
	inner := w - 2
	contentW := inner - 4

	// Header
	b.WriteString(prim.Render("╭"+strings.Repeat("─", inner)+"╮") + "\n")
	header := prim.Bold(true).Render(" " + actionIconTUI(s.action, m.sty) + " " + s.name)
	escHint := m.sty.Gray().Render("Esc to go back")
	spacer := inner - termWidth(header) - termWidth(escHint) - 2
	if spacer < 0 {
		spacer = 0
	}
	b.WriteString("│" + header + strings.Repeat(" ", spacer) + "  " + escHint + "│\n")
	b.WriteString(prim.Render("├"+strings.Repeat("─", inner)+"┤") + "\n")

	// Metadata
	b.WriteString("│  " + m.sty.Gray().Render("Action:") + "    " + m.sty.Gray().Render(fmt.Sprintf("%-20s", s.action)) + strings.Repeat(" ", contentW-30) + "│\n")
	statusLine := "│  " + m.sty.Gray().Render("Status:") + "    " + statusRender(s.status, m.sty)
	if s.dur != "" {
		statusLine += "  " + m.sty.Gray().Render("Duration:") + " " + m.sty.Info().Render(s.dur)
	}
	statusPad := inner - termWidth(statusLine) + 2 // +2 for "│ "
	if statusPad < 0 {
		statusPad = 0
	}
	statusLine += strings.Repeat(" ", statusPad) + "│\n"
	b.WriteString(statusLine)

	// Divider
	b.WriteString(prim.Render("├"+strings.Repeat("─", inner)+"┤") + "\n")

	// Output section
	bodyH := m.termH - 6 // header + meta + divider + footer
	if bodyH < 3 {
		bodyH = 3
	}

	output := s.output
	if output == "" {
		output = "(no output)"
	}
	lines := strings.Split(output, "\n")

	// Scrollable output
	showLines := lines
	if len(lines) > bodyH {
		start := len(lines) - bodyH
		if start < 0 {
			start = 0
		}
		showLines = lines[start:]
	}

	for _, l := range showLines {
		trimmed := l
		if termWidth(trimmed) > contentW {
			// Truncate to fit
			for termWidth(trimmed) > contentW && len(trimmed) > 3 {
				trimmed = trimmed[:len(trimmed)-1]
			}
			trimmed += "..."
		}
		pad := contentW - termWidth(trimmed)
		if pad < 0 {
			pad = 0
		}
		b.WriteString("│  " + m.sty.Gray().Render(trimmed) + strings.Repeat(" ", pad) + " │\n")
	}

	// Pad remaining space
	shown := len(showLines)
	for i := shown; i < bodyH; i++ {
		b.WriteString("│" + strings.Repeat(" ", inner) + "│\n")
	}

	// Footer
	b.WriteString(prim.Render("╰"+strings.Repeat("─", inner)+"╯") + "\n")

	return b.String()
}

// ── Final view ──────────────────────────────────────────────────────────────

func (m *model) finalView() string {
	if m.result == nil {
		return ""
	}
	return renderFinalResult(m.sty, m.tuiStyle, m.result, m.startTime, m.endTime)
}

// ── Step lines ──────────────────────────────────────────────────────────────

func (m *model) stepLine(s stepRec, childStats string, sel bool, fullW int) string {
	indent := strings.Repeat("  ", s.depth)
	nameW := 26 - s.depth*2
	if nameW < 3 {
		nameW = 3
	}

	icon := m.sty.Gray().Render("○")
	switch {
	case isSuccessStatus(s.status):
		icon = m.sty.Success().Render("✓")
	case s.status == StatusFailed:
		icon = m.sty.Error().Render("✗")
	case s.status == StatusRunning:
		icon = m.sty.Warning().Render(spin[m.frame])
	}

	actIcon := actionIconTUI(s.action, m.sty)
	nm := m.sty.Primary().Render(truncS(s.name, nameW))
	tag := m.sty.Gray().Render(fmt.Sprintf("%-7s", s.action))
	if childStats != "" {
		tag = m.sty.Gray().Render(childStats)
	}
	dur := ""
	if s.dur != "" {
		dur = m.sty.Info().Render(" " + s.dur)
	}

	line := fmt.Sprintf("%s%s %s %s %s%s", indent, icon, actIcon, nm, tag, dur)
	// Pad to full width, then highlight if selected
	pad := fullW - termWidth(line)
	if pad < 0 {
		pad = 0
	}
	line += strings.Repeat(" ", pad)
	if sel {
		line = lipgloss.NewStyle().Background(lipgloss.Color("236")).Render(line)
	}
	return line
}

func (m *model) shortStepLine(s stepRec, sel bool) string {
	indent := strings.Repeat("  ", s.depth)
	nameW := 22 - s.depth*2
	if nameW < 3 {
		nameW = 3
	}

	icon := "○"
	switch {
	case isSuccessStatus(s.status):
		icon = "✓"
	case s.status == StatusFailed:
		icon = "✗"
	case s.status == StatusRunning:
		icon = spin[m.frame]
	}

	line := fmt.Sprintf("%s%s %s", indent, icon, truncS(s.name, nameW))
	if sel {
		line = lipgloss.NewStyle().Background(lipgloss.Color("236")).Render(line)
	}
	return line
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func hermesLine(content string, width int, style string) string {
	if style == "hermes" {
		return "│" + content + strings.Repeat(" ", width-termWidth(content)) + "│"
	}
	return content
}

// actionIconTUI returns the canonical action glyph (see printer.go) colored
// by action category for the TUI.
func actionIconTUI(act string, sty *ThemeStyle) string {
	glyph := actionIcon(act)
	switch act {
	case "shell", "http", "parallel", "foreach", "loop":
		return sty.Info().Render(glyph)
	case "condition":
		return sty.Warning().Render(glyph)
	}
	return sty.Gray().Render(glyph)
}

func truncS(s string, n int) string {
	if n < 3 {
		n = 3
	}
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func statusRender(st string, sty *ThemeStyle) string {
	switch {
	case isSuccessStatus(st):
		return sty.Success().Render(st)
	case st == StatusFailed:
		return sty.Error().Render(st)
	case st == StatusRunning:
		return sty.Warning().Render(st)
	}
	return sty.Gray().Render(st)
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
			if s[i] == 'm' {
				skip = false
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func termWidth(s string) int {
	return len([]rune(stripAnsi(s)))
}
