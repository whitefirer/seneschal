package workflow

import (
	"fmt"
	"os"
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

// ── bubbletea model ─────────────────────────────────────────────────────────

type tickMsg time.Time

type model struct {
	store    *stateStore
	sty      *ThemeStyle
	tuiStyle string
	frame    int
	done     bool
	quitting bool

	// Scroll/selection
	scroll  int
	cursor  int
	selId   string
	detail  bool

	// Terminal
	termW int
	termH int

	// Final result
	result              *WorkflowResult
	startTime, endTime string

	// Events from executor
	events chan ProgressEvent
}

// ── Constructor ─────────────────────────────────────────────────────────────

type RealtimePrinter struct {
	program *tea.Program
}

func NewRealtimePrinter(theme Theme, tuiStyle string) *RealtimePrinter {
	if tuiStyle == "" {
		tuiStyle = "hermes"
	}
	m := &model{
		store:    newStateStore(),
		sty:      NewThemeStyle(theme),
		tuiStyle: tuiStyle,
		events:   make(chan ProgressEvent, 256),
	}
	return &RealtimePrinter{program: tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())}
}

func (p *RealtimePrinter) Start() {
	go func() {
		if _, err := p.program.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
			os.Exit(1)
		}
		// Quit tea.Program so if Run() already returned, this is safe.
	}()
}

func (p *RealtimePrinter) Update(event ProgressEvent) {
	p.program.Send(event)
}

func (p *RealtimePrinter) Stop(result *WorkflowResult, startTime, endTime string) {
	p.program.Send(stopMsg{result: result, startTime: startTime, endTime: endTime})
	time.Sleep(300 * time.Millisecond)
	p.program.Quit()
}

type stopMsg struct {
	result              *WorkflowResult
	startTime, endTime string
}

// ── Init ────────────────────────────────────────────────────────────────────

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		listenEvents(m.events),
		tick(),
	)
}

func tick() tea.Cmd {
	return tea.Tick(60*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func listenEvents(ch chan ProgressEvent) tea.Cmd {
	return func() tea.Msg {
		ev := <-ch
		return ev
	}
}

// ── Update ──────────────────────────────────────────────────────────────────

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.termW = msg.Width
		m.termH = msg.Height
		return m, nil
	case ProgressEvent:
		m.store.put(msg)
		// Auto-scroll to bottom if at tail
		if m.scroll == 0 {
			// stay at tail — handled in View
		}
		return m, listenEvents(m.events)
	case tickMsg:
		m.frame = (m.frame + 1) % len(spin)
		return m, tick()
	case stopMsg:
		m.done = true
		m.store.flushRunning()
		m.result = msg.result
		m.startTime = msg.startTime
		m.endTime = msg.endTime
		return m, tea.Quit
	case tea.QuitMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if m.detail {
			m.detail = false
		} else {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	case "esc":
		if m.detail {
			m.detail = false
			m.selId = ""
		} else if m.selId != "" {
			m.selId = ""
		}
		return m, nil
	case "up", "k":
		m.moveCursor(-1)
		return m, nil
	case "down", "j":
		m.moveCursor(1)
		return m, nil
	case "pgup":
		m.scroll -= 10
		if m.scroll < 0 { m.scroll = 0 }
		return m, nil
	case "pgdown":
		steps := m.store.all()
		visible := m.visibleLines()
		m.scroll += 10
		maxScroll := len(steps) - visible
		if maxScroll < 0 { maxScroll = 0 }
		if m.scroll > maxScroll { m.scroll = maxScroll }
		return m, nil
	case "g":
		m.scroll = 0
		m.cursor = 0
		return m, nil
	case "G":
		steps := m.store.all()
		visible := m.visibleLines()
		m.scroll = len(steps) - visible
		if m.scroll < 0 { m.scroll = 0 }
		m.cursor = len(steps) - 1
		if m.cursor < 0 { m.cursor = 0 }
		return m, nil
	case "enter":
		return m.toggleDetail()
	}
	return m, nil
}

func (m *model) moveCursor(delta int) {
	steps := m.store.all()
	newCursor := m.cursor + delta
	if newCursor < 0 { newCursor = 0 }
	if newCursor >= len(steps) { newCursor = len(steps) - 1 }
	m.cursor = newCursor

	// Adjust scroll to keep cursor visible
	visible := m.visibleLines()
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	} else if m.cursor >= m.scroll+visible {
		m.scroll = m.cursor - visible + 1
	}
}

func (m *model) toggleDetail() (tea.Model, tea.Cmd) {
	steps := m.store.all()
	if m.cursor >= len(steps) { return m, nil }
	s := steps[m.cursor]
	if m.detail && m.selId == s.stepId {
		m.detail = false
		m.selId = ""
	} else {
		m.detail = true
		m.selId = s.stepId
	}
	return m, nil
}

// ── View ────────────────────────────────────────────────────────────────────

type layoutSizes struct {
	headerH, footerH, stepH int
	listW                    int
}

func (m *model) visibleLines() int {
	h := m.headerHeight() + m.footerHeight()
	avail := m.termH - h
	if avail < 1 { avail = 1 }
	return avail
}

func (m *model) headerHeight() int { return 3 }
func (m *model) footerHeight() int { return 2 }

func (m *model) View() string {
	if m.termW < 20 { m.termW = 80 }
	if m.termH < 8 { m.termH = 24 }

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

	// Header
	if m.tuiStyle == "hermes" {
		b.WriteString(prim.Render("╭" + strings.Repeat("─", w-2) + "╮") + "\n")
	} else {
		b.WriteString(prim.Render(strings.Repeat("━", w)) + "\n")
	}

	title := prim.Bold(true).Render(" goworkflow")
	if m.done {
		b.WriteString(m.headerLine(title+"  "+gray.Render("Ctrl-C to exit"), w, m.tuiStyle) + "\n")
	} else {
		b.WriteString(m.headerLine(title+"  "+gray.Render(spin[m.frame]+" Running..."), w, m.tuiStyle) + "\n")
	}

	if m.tuiStyle == "hermes" {
		b.WriteString(prim.Render("├" + strings.Repeat("─", w-2) + "┤") + "\n")
	} else {
		b.WriteString(prim.Render(strings.Repeat("━", w)) + "\n")
	}

	// Steps
	steps := m.store.all()
	visible := m.visibleLines()
	if m.scroll+visible > len(steps) && len(steps) >= visible {
		m.scroll = len(steps) - visible
	}
	if m.scroll < 0 { m.scroll = 0 }

	// Children map
	children := map[string][]stepRec{}
	for i := range steps {
		s := &steps[i]
		if s.parentId != "" {
			children[s.parentId] = append(children[s.parentId], *s)
		}
	}

	// Stats (full list, not just visible)
	doneCount, failCount, runCount, parentTotal := 0, 0, 0, 0
	for _, s := range steps {
		if s.depth == 0 {
			parentTotal++
			switch s.status {
			case "completed", "success", "done": doneCount++
			case "failed": failCount++
			case "running": runCount++
			}
		}
	}

	// Render steps
	actualShown := 0
	for i := m.scroll; i < len(steps) && actualShown < visible; i++ {
		s := steps[i]
		childStats := ""
		if kids, ok := children[s.stepId]; ok && s.depth == 0 {
			okc, badc := 0, 0
			for _, k := range kids {
				if k.status == "failed" { badc++ } else if k.status != "running" { okc++ }
			}
			childStats = fmt.Sprintf("%d/%d", okc+badc, len(kids))
		}
		selected := i == m.cursor
		line := m.stepLine(s, childStats, selected, w-4)
		b.WriteString(line + "\n")
		actualShown++
	}
	for i := actualShown; i < visible; i++ {
		b.WriteString(strings.Repeat(" ", w) + "\n")
	}

	// Footer
	if m.tuiStyle == "hermes" {
		b.WriteString(prim.Render("├" + strings.Repeat("─", w-2) + "┤") + "\n")
	} else {
		b.WriteString(prim.Render(strings.Repeat("━", w)) + "\n")
	}

	flag := gray.Render("○")
	if m.done {
		if failCount > 0 {
			flag = m.sty.Error().Render("✗")
		} else {
			flag = m.sty.Success().Render("✓")
		}
	} else if runCount > 0 {
		flag = prim.Render(spin[m.frame])
	}

	pct := 0
	if parentTotal > 0 {
		pct = int(float64(doneCount+failCount) / float64(parentTotal) * 100)
		if pct > 100 { pct = 100 }
	}
	bw := 16
	filled := pct * bw / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", bw-filled)
	if failCount > 0 {
		bar = m.sty.Error().Render(bar)
	} else if m.done {
		bar = m.sty.Success().Render(bar)
	} else {
		bar = prim.Render(bar)
	}

	stats := flag + " " +
		m.sty.Success().Render(fmt.Sprintf("%d done", doneCount)) + " · " +
		m.sty.Warning().Render(fmt.Sprintf("%d running", runCount)) + " · " +
		m.sty.Error().Render(fmt.Sprintf("%d failed", failCount)) + " · " +
		gray.Render(fmt.Sprintf("%d total", parentTotal)) +
		"  " + bar + fmt.Sprintf(" %d%%", pct)

	b.WriteString(m.footerLine(stats+"  ↑↓ scroll  Enter detail  q quit", w, m.tuiStyle) + "\n")

	if m.tuiStyle == "hermes" {
		b.WriteString(prim.Render("╰" + strings.Repeat("─", w-2) + "╯") + "\n")
	} else {
		b.WriteString(prim.Render(strings.Repeat("━", w)) + "\n")
	}

	return b.String()
}

// ── Detail view ─────────────────────────────────────────────────────────────

func (m *model) detailView() string {
	listW := m.termW * 40 / 100
	if listW < 25 { listW = 25 }
	detailW := m.termW - listW - 1

	var b strings.Builder
	prim := m.sty.Primary()

	// Top border spans full width
	b.WriteString(prim.Render("╭" + strings.Repeat("─", m.termW-2) + "╮") + "\n")

	title := prim.Bold(true).Render(" goworkflow")
	if m.done {
		b.WriteString(m.headerLine(title+"  "+m.sty.Gray().Render("Ctrl-C to exit"), m.termW, m.tuiStyle) + "\n")
	} else {
		b.WriteString(m.headerLine(title+"  "+m.sty.Gray().Render(spin[m.frame]+" Running..."), m.termW, m.tuiStyle) + "\n")
	}

	// Divider between header and content
	b.WriteString(prim.Render("├" + strings.Repeat("─", listW) + "┬" + strings.Repeat("─", detailW) + "┤") + "\n")

	// Step list (left)
	steps := m.store.all()
	visibleH := m.termH - m.headerHeight() - m.footerHeight()
	if visibleH < 1 { visibleH = 1 }

	children := map[string][]stepRec{}
	for i := range steps {
		s := &steps[i]
		if s.parentId != "" {
			children[s.parentId] = append(children[s.parentId], *s)
		}
	}

	for i := m.scroll; i < len(steps) && (i-m.scroll) < visibleH; i++ {
		s := steps[i]
		selected := i == m.cursor
		line := m.shortStepLine(s, selected, listW-2)
		b.WriteString("│" + line + strings.Repeat(" ", listW-2-len(line)) + "│")

		// Right side: detail for selected step
		if i == m.cursor && m.selId == s.stepId {
			detail := m.renderDetail(s, detailW)
			b.WriteString(detail)
		} else {
			b.WriteString(strings.Repeat(" ", detailW))
		}
		b.WriteString("\n")
	}
	for i := len(steps) - m.scroll; i < visibleH; i++ {
		b.WriteString("│" + strings.Repeat(" ", listW) + "│" + strings.Repeat(" ", detailW) + "\n")
	}

	// Footer divider
	b.WriteString(prim.Render("├" + strings.Repeat("─", listW) + "┴" + strings.Repeat("─", detailW) + "┤") + "\n")

	// Status
	b.WriteString(m.footerLine("  Esc back  ↑↓ scroll  q quit", m.termW, m.tuiStyle) + "\n")
	b.WriteString(prim.Render("╰" + strings.Repeat("─", m.termW-2) + "╯") + "\n")

	return b.String()
}

func (m *model) renderDetail(s stepRec, width int) string {
	pad := width - 4
	if pad < 10 { pad = 10 }

	title := m.sty.Primary().Bold(true).Render(" STEP DETAIL ")
	div := m.sty.Gray().Render(strings.Repeat("─", pad-len(" STEP DETAIL ")))
	b := title + div + "\n\n"

	name := truncS(s.name, 30)
	b += fmt.Sprintf("  Name:      %s\n", m.sty.Primary().Render(name))
	b += fmt.Sprintf("  Action:    %s\n", m.sty.Gray().Render(s.action))
	b += fmt.Sprintf("  Status:    %s\n", statusRender(s.status, m.sty))
	if s.dur != "" {
		b += fmt.Sprintf("  Duration:  %s\n", m.sty.Info().Render(s.dur))
	}

	if s.output != "" {
		b += "\n" + m.sty.Gray().Render(strings.Repeat("─", pad)) + "\n"
		output := strings.ReplaceAll(s.output, "\n", "\n  ")
		if len(output) > 500 {
			output = output[:500] + "..."
		}
		b += "  " + m.sty.Gray().Render(output)
	}

	return b
}

func statusRender(st string, sty *ThemeStyle) string {
	switch st {
	case "completed", "success", "done":
		return sty.Success().Render(st)
	case "failed":
		return sty.Error().Render(st)
	case "running":
		return sty.Warning().Render(st)
	}
	return sty.Gray().Render(st)
}

// ── Final view ──────────────────────────────────────────────────────────────

func (m *model) finalView() string {
	var lines []string

	ok, bad := 0, 0
	for _, s := range m.result.Steps {
		if s.Status == "failed" { bad++ } else { ok++ }
	}

	statusStyle := m.sty.Success()
	statusIcon := "✓"
	statusLabel := "SUCCESS"
	if m.result.Status == "failed" {
		statusStyle = m.sty.Error()
		statusIcon = "✗"
		statusLabel = "FAILED"
	}

	st, _ := time.Parse(time.RFC3339, m.startTime)
	et, _ := time.Parse(time.RFC3339, m.endTime)
	dur := et.Sub(st).Round(time.Millisecond).String()

	lines = append(lines, statusStyle.Bold(true).Render(fmt.Sprintf("  %s %s", statusIcon, statusLabel)))
	lines = append(lines, fmt.Sprintf("  %s/%s steps · %s",
		m.sty.Success().Render(fmt.Sprintf("%d", ok)),
		m.sty.Gray().Render(fmt.Sprintf("%d", len(m.result.Steps))),
		m.sty.Info().Render(dur)))
	if bad > 0 {
		lines = append(lines, m.sty.Error().Render(fmt.Sprintf("  %d failed", bad)))
	}

	if m.tuiStyle == "claude" {
		return strings.Join(lines, "\n") + "\n\n"
	}
	return m.sty.BoxStyle().Render(strings.Join(lines, "\n")) + "\n\n"
}

// ── Step line rendering ─────────────────────────────────────────────────────

func (m *model) stepLine(s stepRec, childStats string, selected bool, maxW int) string {
	indent := strings.Repeat("  ", s.depth)
	// Determine the space for name based on available width
	nameW := maxW - len(indent) - 35
	if nameW < 5 { nameW = 5 }
	if nameW > 26 { nameW = 26 }
	nameW -= s.depth * 2

	icon := m.sty.Gray().Render("○")
	switch s.status {
	case "completed", "success", "done":
		icon = m.sty.Success().Render("✓")
	case "failed":
		icon = m.sty.Error().Render("✗")
	case "running":
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

	// Highlight selected
	if selected {
		line = lipgloss.NewStyle().Background(lipgloss.Color("236")).Render(line)
	}
	return line
}

func (m *model) shortStepLine(s stepRec, selected bool, maxW int) string {
	indent := strings.Repeat("  ", s.depth)
	nameW := maxW - len(indent) - 12
	if nameW < 5 { nameW = 5 }
	nameW -= s.depth * 2

	icon := "○"
	switch s.status {
	case "completed", "success", "done": icon = "✓"
	case "failed": icon = "✗"
	case "running": icon = spin[m.frame]
	}

	line := fmt.Sprintf("%s%s %s", indent, icon, truncS(s.name, nameW))

	if selected {
		line = lipgloss.NewStyle().Background(lipgloss.Color("236")).Render(line)
	}
	return line
}

func (m *model) headerLine(content string, w int, style string) string {
	if style == "hermes" {
		return "│" + content + strings.Repeat(" ", w-2-lipglossWidth(content)) + "│"
	}
	return content
}

func (m *model) footerLine(content string, w int, style string) string {
	if style == "hermes" {
		return "│ " + content + strings.Repeat(" ", w-4-lipglossWidth(content)) + " │"
	}
	return "  " + content
}

func lipglossWidth(s string) int {
	clean := s
	for {
		i := strings.Index(clean, "\033")
		if i < 0 { break }
		j := strings.IndexByte(clean[i:], 'm')
		if j < 0 { break }
		clean = clean[:i] + clean[i+j+1:]
	}
	return len([]rune(clean))
}

// ── Icons and helpers ──────────────────────────────────────────────────────

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
