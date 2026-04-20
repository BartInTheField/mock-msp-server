package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const maxLogs = 200

type view int

const (
	viewDashboard view = iota
	viewList
	viewDetail
)

type detailMode int

const (
	detailList detailMode = iota
	detailJSON
)

type moduleDef struct {
	id    string
	label string
}

var modules = []moduleDef{
	{"locations", "Locations"},
	{"sessions", "Sessions"},
	{"cdrs", "CDRs"},
	{"tariffs", "Tariffs"},
	{"tokens", "Tokens"},
}

// pullShortcuts returns the module/key pairs the role can pull. The returned
// slice preserves the order used by TUI rendering and [1]..[n] keys.
func pullShortcuts(role string) []struct {
	Mod moduleDef
	Key string
} {
	ids := []string{"locations", "sessions", "cdrs", "tariffs", "tokens"}
	if role == RoleCPO {
		ids = []string{"tokens"}
	}
	out := make([]struct {
		Mod moduleDef
		Key string
	}, 0, len(ids))
	for i, id := range ids {
		for _, m := range modules {
			if m.id == id {
				out = append(out, struct {
					Mod moduleDef
					Key string
				}{m, fmt.Sprintf("%d", i+1)})
				break
			}
		}
	}
	return out
}

// --- Colors (Tokyo Night) ---
var (
	cBlue    = lipgloss.Color("#7aa2f7")
	cGreen   = lipgloss.Color("#9ece6a")
	cCyan    = lipgloss.Color("#7dcfff")
	cPurple  = lipgloss.Color("#bb9af7")
	cRed     = lipgloss.Color("#f7768e")
	cOrange  = lipgloss.Color("#e0af68")
	cOrange2 = lipgloss.Color("#ff9e64")
	cText    = lipgloss.Color("#c0caf5")
	cMuted   = lipgloss.Color("#565f89")
)

// --- Messages ---
type logMsg LogEntry
type stateChangeMsg struct{}
type pullResultMsg string
type registerResultMsg string

func waitForLog(ch <-chan LogEntry) tea.Cmd {
	return func() tea.Msg { return logMsg(<-ch) }
}
func waitForStateChange(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg { <-ch; return stateChangeMsg{} }
}

// --- Model ---
type model struct {
	srv          *Server
	url          string
	port         int
	peerURL      string
	logCh        <-chan LogEntry
	stateCh      <-chan struct{}
	logs         []LogEntry
	view         view
	activeMod    string
	activeKey    string
	selectedIdx  int
	detailScroll int
	detailPath   []string
	detailMode   detailMode
	subIdx       int
	width        int
	height       int
}

func newModel(srv *Server, url string, port int, peerURL string, logCh <-chan LogEntry, stateCh <-chan struct{}) model {
	return model{
		srv:     srv,
		url:     url,
		port:    port,
		peerURL: peerURL,
		logCh:   logCh,
		stateCh: stateCh,
		logs:    make([]LogEntry, 0, maxLogs),
		view:    viewDashboard,
		width:   120,
		height:  40,
	}
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{waitForLog(m.logCh), waitForStateChange(m.stateCh)}
	if m.peerURL != "" {
		cmds = append(cmds, registerCmd(m.srv, m.peerURL, 300*time.Millisecond))
	}
	return tea.Batch(cmds...)
}

func registerCmd(srv *Server, peerURL string, delay time.Duration) tea.Cmd {
	return func() tea.Msg {
		if delay > 0 {
			time.Sleep(delay)
		}
		return registerResultMsg(srv.Register(peerURL))
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case logMsg:
		m.logs = append(m.logs, LogEntry(msg))
		if len(m.logs) > maxLogs {
			m.logs = m.logs[len(m.logs)-maxLogs:]
		}
		return m, waitForLog(m.logCh)

	case stateChangeMsg:
		return m, waitForStateChange(m.stateCh)

	case pullResultMsg:
		return m, nil

	case registerResultMsg:
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := k.String()

	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if key == "q" {
		if m.view == viewDashboard {
			return m, tea.Quit
		}
		m.view = viewDashboard
		m.selectedIdx = 0
		return m, nil
	}
	if key == "esc" {
		switch m.view {
		case viewDetail:
			if len(m.detailPath) > 0 {
				m.detailPath = m.detailPath[:len(m.detailPath)-1]
				m.subIdx = 0
				m.detailScroll = 0
				m.detailMode = detailJSON
				return m, nil
			}
			m.view = viewList
			m.selectedIdx = 0
			m.detailMode = detailJSON
		case viewList:
			m.view = viewDashboard
			m.selectedIdx = 0
		}
		return m, nil
	}

	switch m.view {
	case viewDashboard:
		for _, sc := range pullShortcuts(m.srv.Role) {
			if key == sc.Key {
				return m, pullCmd(m.srv, sc.Mod.id)
			}
		}
		switch key {
		case "j", "down":
			if m.selectedIdx < len(modules)-1 {
				m.selectedIdx++
			}
		case "k", "up":
			if m.selectedIdx > 0 {
				m.selectedIdx--
			}
		case "enter":
			mod := modules[m.selectedIdx]
			m.view = viewList
			m.activeMod = mod.id
			m.selectedIdx = 0
		case "c":
			m.logs = m.logs[:0]
		case "a":
			return m, pullAllCmd(m.srv)
		case "r", "R":
			if m.peerURL != "" {
				return m, registerCmd(m.srv, m.peerURL, 0)
			}
		}

	case viewList:
		entries := m.listEntries()
		switch key {
		case "j", "down":
			if m.selectedIdx < len(entries)-1 {
				m.selectedIdx++
			}
		case "k", "up":
			if m.selectedIdx > 0 {
				m.selectedIdx--
			}
		case "enter":
			if m.selectedIdx < len(entries) {
				m.view = viewDetail
				m.activeKey = entries[m.selectedIdx].key
				m.detailScroll = 0
				m.detailPath = nil
				m.subIdx = 0
				m.detailMode = detailJSON
			}
		case "p":
			return m, pullCmd(m.srv, m.activeMod)
		}

	case viewDetail:
		obj, _ := m.resolveDetail()
		children := m.detailChildren(obj)
		inList := m.detailMode == detailList && len(children) > 0
		switch key {
		case "v":
			if len(children) > 0 {
				if m.detailMode == detailList {
					m.detailMode = detailJSON
				} else {
					m.detailMode = detailList
				}
				m.detailScroll = 0
			}
		case "enter":
			if inList && m.subIdx < len(children) {
				m.detailPath = append(m.detailPath, children[m.subIdx].id)
				m.subIdx = 0
				m.detailScroll = 0
				m.detailMode = detailJSON
			}
		case "j", "down":
			if inList {
				if m.subIdx < len(children)-1 {
					m.subIdx++
				}
			} else {
				m.detailScroll++
			}
		case "k", "up":
			if inList {
				if m.subIdx > 0 {
					m.subIdx--
				}
			} else {
				m.detailScroll--
			}
		case "pgdown", " ":
			if !inList {
				m.detailScroll += m.detailPageSize()
			}
		case "pgup", "b":
			if !inList {
				m.detailScroll -= m.detailPageSize()
			}
		case "g", "home":
			if !inList {
				m.detailScroll = 0
			}
		case "G", "end":
			if !inList {
				m.detailScroll = m.detailMaxScroll()
			}
		}
		if max := m.detailMaxScroll(); m.detailScroll > max {
			m.detailScroll = max
		}
		if m.detailScroll < 0 {
			m.detailScroll = 0
		}
	}
	return m, nil
}

func pullCmd(srv *Server, module string) tea.Cmd {
	return func() tea.Msg {
		return pullResultMsg(srv.PullModule(module))
	}
}

func pullAllCmd(srv *Server) tea.Cmd {
	return func() tea.Msg {
		mods := srv.PullableModules()
		results := make([]string, 0, len(mods))
		for _, mod := range mods {
			results = append(results, srv.PullModule(mod))
		}
		return pullResultMsg(strings.Join(results, " | "))
	}
}

type storeEntry struct {
	key string
	obj map[string]any
}

func (m model) listEntries() []storeEntry {
	m.srv.State.mu.RLock()
	defer m.srv.State.mu.RUnlock()
	store := m.srv.State.Store(m.activeMod)
	out := make([]storeEntry, 0, len(store))
	for k, v := range store {
		out = append(out, storeEntry{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

type subEntry struct {
	id    string
	label string
	aux   string
}

// resolveDetail walks detailPath from the root module object to the current
// sub-object. Returns ok=false if any step can't be resolved (stale path).
func (m model) resolveDetail() (map[string]any, bool) {
	m.srv.State.mu.RLock()
	obj, ok := m.srv.State.Store(m.activeMod)[m.activeKey]
	m.srv.State.mu.RUnlock()
	if !ok {
		return nil, false
	}
	for i, step := range m.detailPath {
		switch m.activeMod {
		case "locations":
			switch i {
			case 0:
				e, ok := findChild(asMapSlice(obj["evses"]), "uid", step)
				if !ok {
					return nil, false
				}
				obj = e
			case 1:
				c, ok := findChild(asMapSlice(obj["connectors"]), "id", step)
				if !ok {
					return nil, false
				}
				obj = c
			default:
				return nil, false
			}
		case "sessions":
			if i == 0 && step == "charging_preferences" {
				cp, ok := obj["charging_preferences"].(map[string]any)
				if !ok {
					return nil, false
				}
				obj = cp
			} else {
				return nil, false
			}
		default:
			return nil, false
		}
	}
	return obj, true
}

// detailChildren returns drillable sub-objects of the current detail obj.
func (m model) detailChildren(obj map[string]any) []subEntry {
	if obj == nil {
		return nil
	}
	switch m.activeMod {
	case "locations":
		switch len(m.detailPath) {
		case 0:
			evses := asMapSlice(obj["evses"])
			out := make([]subEntry, 0, len(evses))
			for _, e := range evses {
				uid, _ := e["uid"].(string)
				status, _ := e["status"].(string)
				out = append(out, subEntry{id: uid, label: uid, aux: status})
			}
			return out
		case 1:
			conns := asMapSlice(obj["connectors"])
			out := make([]subEntry, 0, len(conns))
			for _, c := range conns {
				id, _ := c["id"].(string)
				std, _ := c["standard"].(string)
				out = append(out, subEntry{id: id, label: "Connector " + id, aux: std})
			}
			return out
		}
	case "sessions":
		if len(m.detailPath) == 0 {
			if _, ok := obj["charging_preferences"].(map[string]any); ok {
				return []subEntry{{id: "charging_preferences", label: "charging_preferences", aux: "preferences"}}
			}
		}
	}
	return nil
}

// --- View ---
func (m model) View() string {
	header := m.renderHeader()
	body := m.renderBody()
	return lipgloss.JoinVertical(lipgloss.Left, header, body)
}

func (m model) renderHeader() string {
	label := "Mock MSP OCPI 2.1.1 / 2.2.1"
	if m.srv.Role == RoleCPO {
		label = "Mock CPO OCPI 2.1.1 / 2.2.1"
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(cText).Render(label)
	crumb := ""
	switch m.view {
	case viewList:
		crumb = lipgloss.NewStyle().Foreground(cMuted).Render(" > " + m.activeMod)
	case viewDetail:
		parts := []string{m.activeMod, m.activeKey}
		parts = append(parts, m.detailPath...)
		crumb = lipgloss.NewStyle().Foreground(cMuted).Render(" > " + strings.Join(parts, " > "))
	}
	right := lipgloss.NewStyle().Foreground(cMuted).Render(fmt.Sprintf("%s | :%d", m.url, m.port))

	inner := m.width - 4
	if inner < 20 {
		inner = 20
	}
	left := title + crumb
	gap := inner - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(cBlue).
		Padding(0, 1).
		Width(m.width - 2).
		Render(line)
}

func (m model) renderBody() string {
	leftW := 40
	rightW := m.width - leftW - 2
	if rightW < 20 {
		rightW = 20
	}
	bodyH := m.height - 5
	if bodyH < 10 {
		bodyH = 10
	}

	left := m.renderLeftPanel(leftW, bodyH)
	right := m.renderRightPanel(rightW, bodyH)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (m model) renderLeftPanel(w, h int) string {
	status := m.renderStatusBox(w)
	var middle string
	if m.view == viewDashboard {
		middle = m.renderBrowseBox(w) + "\n" + m.renderPullBox(w)
	} else {
		middle = m.renderNavBox(w)
	}
	return lipgloss.NewStyle().Width(w).Render(
		lipgloss.JoinVertical(lipgloss.Left, status, middle),
	)
}

func (m model) renderStatusBox(w int) string {
	m.srv.State.mu.RLock()
	registered := m.srv.State.PeerCredentials != nil
	var peerBizName string
	if registered {
		peerBizName = peerName(m.srv.State.PeerCredentials)
	}
	peerVersion := m.srv.State.PeerVersion
	counts := m.srv.State.Counts
	m.srv.State.mu.RUnlock()

	regValue := "No"
	regColor := cRed
	if registered {
		regValue = peerBizName
		if regValue == "" {
			regValue = "Yes"
		}
		regColor = cGreen
	}

	peerLabel := "CPO Registered:"
	if m.srv.Role == RoleCPO {
		peerLabel = "MSP Registered:"
	}
	protoValue := "—"
	protoColor := cMuted
	if peerVersion != "" {
		protoValue = peerVersion
		protoColor = cCyan
	}
	lines := []string{
		kvRow(peerLabel, regValue, regColor, w-4),
		kvRow("Protocol:", protoValue, protoColor, w-4),
	}
	for _, mod := range modules {
		n := 0
		switch mod.id {
		case "locations":
			n = counts.Locations
		case "sessions":
			n = counts.Sessions
		case "cdrs":
			n = counts.CDRs
		case "tariffs":
			n = counts.Tariffs
		case "tokens":
			n = counts.Tokens
		}
		lines = append(lines, kvRow(mod.label+":", fmt.Sprintf("%d", n), cText, w-4))
	}
	return boxed("Status", strings.Join(lines, "\n"), cGreen, w)
}

func kvRow(label, value string, valColor lipgloss.Color, w int) string {
	l := lipgloss.NewStyle().Foreground(cMuted).Render(label)
	v := lipgloss.NewStyle().Foreground(valColor).Render(value)
	gap := w - lipgloss.Width(l) - lipgloss.Width(v)
	if gap < 1 {
		gap = 1
	}
	return l + strings.Repeat(" ", gap) + v
}

func (m model) renderBrowseBox(w int) string {
	var b strings.Builder
	m.srv.State.mu.RLock()
	counts := m.srv.State.Counts
	m.srv.State.mu.RUnlock()
	for i, mod := range modules {
		n := 0
		switch mod.id {
		case "locations":
			n = counts.Locations
		case "sessions":
			n = counts.Sessions
		case "cdrs":
			n = counts.CDRs
		case "tariffs":
			n = counts.Tariffs
		case "tokens":
			n = counts.Tokens
		}
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(cText)
		if i == m.selectedIdx {
			prefix = "> "
			style = lipgloss.NewStyle().Foreground(cCyan)
		}
		row := style.Render(fmt.Sprintf("%s%s (%d)", prefix, mod.label, n))
		b.WriteString(row + " " + ownershipTag(m.srv.Role, mod.id))
		b.WriteString("\n")
	}
	b.WriteString(lipgloss.NewStyle().Foreground(cMuted).Render("\nj/k navigate, Enter to open"))
	return boxed("Browse", b.String(), cCyan, w)
}

func ownershipTag(role, module string) string {
	if OwnsModule(role, module) {
		return lipgloss.NewStyle().Foreground(cGreen).Render("asset")
	}
	return lipgloss.NewStyle().Foreground(cOrange2).Render("received")
}

func ownershipDetailHeader(role, module string) string {
	partyLabel := "MSP"
	peerLabel := "CPO"
	if role == RoleCPO {
		partyLabel = "CPO"
		peerLabel = "MSP"
	}
	label := lipgloss.NewStyle().Foreground(cMuted).Render("Ownership: ")
	if OwnsModule(role, module) {
		return label + lipgloss.NewStyle().Foreground(cGreen).Render("asset of this "+partyLabel)
	}
	return label + lipgloss.NewStyle().Foreground(cOrange2).Render("received from "+peerLabel)
}

func (m model) renderPullBox(w int) string {
	keyStyle := lipgloss.NewStyle().Foreground(cPurple)
	var b strings.Builder
	for _, sc := range pullShortcuts(m.srv.Role) {
		b.WriteString(keyStyle.Render("["+sc.Key+"]") + " " + sc.Mod.label + "\n")
	}
	b.WriteString(keyStyle.Render("[a]") + " Pull All\n")
	if m.peerURL != "" {
		b.WriteString(keyStyle.Render("[R]") + " Register w/ peer\n")
	}
	b.WriteString(keyStyle.Render("[c]") + " Clear Logs\n")
	b.WriteString(keyStyle.Render("[q]") + " Quit")
	return boxed("Pull", b.String(), cPurple, w)
}

func (m model) renderNavBox(w int) string {
	keyStyle := lipgloss.NewStyle().Foreground(cPurple)
	var b strings.Builder
	b.WriteString(keyStyle.Render("[j/k]") + " Scroll\n")
	if m.view == viewList {
		b.WriteString(keyStyle.Render("[Enter]") + " View detail\n")
		b.WriteString(keyStyle.Render("[p]") + " Pull " + m.activeMod + "\n")
	}
	if m.view == viewDetail {
		obj, _ := m.resolveDetail()
		hasChildren := len(m.detailChildren(obj)) > 0
		if hasChildren && m.detailMode == detailList {
			b.WriteString(keyStyle.Render("[Enter]") + " Drill into sub-object\n")
			b.WriteString(keyStyle.Render("[v]") + " View raw JSON\n")
		} else {
			b.WriteString(keyStyle.Render("[pgup/pgdn]") + " Page\n")
			b.WriteString(keyStyle.Render("[g/G]") + " Top/Bottom\n")
			if hasChildren {
				b.WriteString(keyStyle.Render("[v]") + " View sub-objects\n")
			}
		}
	}
	b.WriteString(keyStyle.Render("[Esc]") + " Back\n")
	b.WriteString(keyStyle.Render("[q]") + " Dashboard")
	return boxed("Navigation", b.String(), cPurple, w)
}

func (m model) renderRightPanel(w, h int) string {
	var title, content string
	contentH := h - 3 // border (2) + title (1)
	if contentH < 1 {
		contentH = 1
	}
	contentW := w - 4
	switch m.view {
	case viewDashboard:
		title = "Request Log"
		content = m.renderRequestLog(contentW, contentH)
	case viewList:
		label := m.activeMod
		for _, mod := range modules {
			if mod.id == m.activeMod {
				label = mod.label
			}
		}
		title = label
		content = m.renderObjectList(contentW, contentH)
	case viewDetail:
		title = m.activeKey
		if len(m.detailPath) > 0 {
			title = m.detailPath[len(m.detailPath)-1]
		}
		content = m.renderObjectDetail(contentW, contentH)
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cMuted).
		Width(w).
		Height(h)
	titleStyle := lipgloss.NewStyle().Foreground(cText).Render(" " + title + " ")
	body := lipgloss.JoinVertical(lipgloss.Left, titleStyle, content)
	return style.Render(body)
}

var methodColors = map[string]lipgloss.Color{
	"GET":    cGreen,
	"POST":   cBlue,
	"PUT":    cOrange,
	"PATCH":  cPurple,
	"DELETE": cRed,
	"OUT":    cCyan,
}

func (m model) renderRequestLog(w, h int) string {
	if len(m.logs) == 0 {
		return lipgloss.NewStyle().Foreground(cMuted).Render("Waiting for requests...")
	}
	start := 0
	if len(m.logs) > h {
		start = len(m.logs) - h
	}
	visible := m.logs[start:]
	// reverse so newest on top
	rev := make([]LogEntry, len(visible))
	for i, e := range visible {
		rev[len(visible)-1-i] = e
	}
	var b strings.Builder
	tsStyle := lipgloss.NewStyle().Foreground(cMuted)
	urlStyle := lipgloss.NewStyle().Foreground(cText)
	for _, l := range rev {
		ts := l.Timestamp
		if len(ts) >= 19 {
			ts = ts[11:19]
		}
		color, ok := methodColors[l.Method]
		if !ok {
			color = cText
		}
		method := lipgloss.NewStyle().Foreground(color).Width(6).Render(l.Method)
		line := fmt.Sprintf("%s %s %s", tsStyle.Render(ts), method, urlStyle.Render(l.URL))
		if lipgloss.Width(line) > w {
			line = truncAnsi(line, w)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func truncAnsi(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	// rough truncation — lipgloss doesn't expose ansi-aware truncation directly
	return s[:w]
}

func (m model) renderObjectList(w, h int) string {
	entries := m.listEntries()
	if len(entries) == 0 {
		return lipgloss.NewStyle().Foreground(cMuted).Render("No objects. Press [p] to pull or [Esc] to go back.")
	}
	start := 0
	if m.selectedIdx >= h {
		start = m.selectedIdx - h + 1
	}
	end := start + h
	if end > len(entries) {
		end = len(entries)
	}
	tag := ownershipTag(m.srv.Role, m.activeMod)
	var b strings.Builder
	for i := start; i < end; i++ {
		e := entries[i]
		label := e.key
		for _, k := range []string{"name", "uid", "id"} {
			if v, ok := e.obj[k].(string); ok && v != "" {
				label = v
				break
			}
		}
		selected := i == m.selectedIdx
		prefix := " "
		lblColor := cText
		keyColor := cMuted
		if selected {
			prefix = ">"
			lblColor = cCyan
			keyColor = cCyan
		}
		aux := ""
		if label != e.key {
			aux = lipgloss.NewStyle().Foreground(cMuted).Render(" (" + e.key + ")")
		}
		row := lipgloss.NewStyle().Foreground(keyColor).Render(prefix) +
			" " + lipgloss.NewStyle().Foreground(lblColor).Render(label) + aux +
			" " + tag
		b.WriteString(row)
		b.WriteString("\n")
	}
	return b.String()
}

var jsonKeyRe = regexp.MustCompile(`^(\s*)"([^"]+)"(:)\s*(.*)$`)

func (m model) renderObjectDetail(w, h int) string {
	obj, ok := m.resolveDetail()
	if !ok {
		return lipgloss.NewStyle().Foreground(cRed).Render("Object not found")
	}
	ownershipHeader := ownershipDetailHeader(m.srv.Role, m.activeMod)
	children := m.detailChildren(obj)
	if m.detailMode == detailList && len(children) > 0 {
		return m.renderChildList(ownershipHeader, children, w, h)
	}
	raw, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return lipgloss.NewStyle().Foreground(cRed).Render(err.Error())
	}
	lines := strings.Split(string(raw), "\n")
	total := len(lines)
	start := m.detailScroll
	if start > total-1 {
		start = total - 1
	}
	if start < 0 {
		start = 0
	}
	end := start + h - 2 // reserve 1 line for ownership header + 1 for footer
	if end > total {
		end = total
	}
	keyStyle := lipgloss.NewStyle().Foreground(cBlue)
	textStyle := lipgloss.NewStyle().Foreground(cText)
	var b strings.Builder
	b.WriteString(ownershipHeader)
	b.WriteString("\n")
	for _, line := range lines[start:end] {
		match := jsonKeyRe.FindStringSubmatch(line)
		if match != nil {
			indent, jsonKey, colon, rest := match[1], match[2], match[3], match[4]
			valColor := valueColor(rest)
			b.WriteString(indent + keyStyle.Render("\""+jsonKey+"\"") + colon + " " +
				lipgloss.NewStyle().Foreground(valColor).Render(rest))
		} else {
			b.WriteString(textStyle.Render(line))
		}
		b.WriteString("\n")
	}
	footer := fmt.Sprintf("%d-%d/%d  [j/k ↑↓ pgup/pgdn g/G]", start+1, end, total)
	b.WriteString(lipgloss.NewStyle().Foreground(cMuted).Render(footer))
	return b.String()
}

func (m model) detailLineCount() int {
	obj, ok := m.resolveDetail()
	if !ok {
		return 0
	}
	raw, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return 0
	}
	return strings.Count(string(raw), "\n") + 1
}

func (m model) renderChildList(header string, children []subEntry, w, h int) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	hint := "Enter to drill, v to view raw JSON, Esc to go back"
	b.WriteString(lipgloss.NewStyle().Foreground(cMuted).Render(hint))
	b.WriteString("\n\n")
	start := 0
	maxRows := h - 4
	if maxRows < 1 {
		maxRows = 1
	}
	if m.subIdx >= maxRows {
		start = m.subIdx - maxRows + 1
	}
	end := start + maxRows
	if end > len(children) {
		end = len(children)
	}
	for i := start; i < end; i++ {
		c := children[i]
		prefix := " "
		lbl := lipgloss.NewStyle().Foreground(cText)
		aux := lipgloss.NewStyle().Foreground(cMuted)
		if i == m.subIdx {
			prefix = ">"
			lbl = lipgloss.NewStyle().Foreground(cCyan)
			aux = lipgloss.NewStyle().Foreground(cCyan)
		}
		row := lbl.Render(prefix+" "+c.label)
		if c.aux != "" {
			row += " " + aux.Render("("+c.aux+")")
		}
		b.WriteString(row)
		b.WriteString("\n")
	}
	return b.String()
}

func (m model) detailPageSize() int {
	// matches renderObjectDetail's visible JSON-line count:
	// header box (3) + right-panel border (2) + title (1) + ownership (1)
	// + footer (1) + header-bottom-gap (2) = 10
	h := m.height - 10
	if h < 3 {
		return 3
	}
	return h
}

func (m model) detailMaxScroll() int {
	n := m.detailLineCount() - m.detailPageSize()
	if n < 0 {
		return 0
	}
	return n
}

func valueColor(raw string) lipgloss.Color {
	t := strings.TrimSuffix(strings.TrimSpace(raw), ",")
	if t == "true" || t == "false" {
		return cOrange2
	}
	if t == "null" {
		return cMuted
	}
	if len(t) > 0 && (t[0] == '-' || (t[0] >= '0' && t[0] <= '9')) {
		return cOrange2
	}
	if strings.HasPrefix(t, "\"") && strings.HasSuffix(t, "\"") {
		return cGreen
	}
	return cText
}

func boxed(title, content string, color lipgloss.Color, width int) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color).
		Padding(0, 1).
		Width(width - 2)
	titled := lipgloss.NewStyle().Foreground(color).Render(" " + title + " ")
	return box.Render(lipgloss.JoinVertical(lipgloss.Left, titled, content))
}
