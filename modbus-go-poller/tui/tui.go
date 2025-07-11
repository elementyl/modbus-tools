package tui

import (
	"fmt"
	"log"
	"modbus-tools/modbus-go-poller/config"
	"modbus-tools/modbus-go-poller/poller"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- STYLES ---
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#575B7E")).
			Padding(0, 1)

	baseStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240"))

	statusKeyStyle = lipgloss.NewStyle().Bold(true)

	alarmCritStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	alarmWarnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))

	changedStyle      = lipgloss.NewStyle().Background(lipgloss.Color("202")).Foreground(lipgloss.Color("0"))
	pointNameStyle    = lipgloss.NewStyle().Width(28)
	pointAddressStyle = lipgloss.NewStyle().Width(10).Align(lipgloss.Right)
	pointRawStyle     = lipgloss.NewStyle().Width(12).Align(lipgloss.Right)
	pointValueStyle   = lipgloss.NewStyle().Width(20).Align(lipgloss.Right)
	pointUnitStyle    = lipgloss.NewStyle().Width(10)
)

// --- MODEL ---
type tickMsg time.Time

type Model struct {
	state          *poller.PollerState
	log            *log.Logger
	viewport       viewport.Model
	textInput      textinput.Model
	ready          bool
	lastDataRender string
	lastChange     map[uint16]time.Time
}

func NewModel(ps *poller.PollerState, logger *log.Logger) Model {
	ti := textinput.New()
	ti.Placeholder = "set \"Point Name\" | write 40001 4 | write \"Point Name\" 25.5"
	ti.Focus()

	return Model{
		state:      ps,
		log:        logger,
		textInput:  ti,
		lastChange: make(map[uint16]time.Time),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// --- UPDATE ---
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.textInput.Focused() {
			switch msg.Type {
			case tea.KeyEnter:
				m.handleCommand()
				return m, nil // Keep text input focused
			case tea.KeyCtrlC, tea.KeyEsc:
				m.textInput.Blur()
				return m, nil
			}
		} else {
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "i", "c":
				m.textInput.Focus()
				return m, nil
			}
		}

	case tea.WindowSizeMsg:
		topPaneHeight := 8
		txRxPaneHeight := 4
		footerHeight := 3
		verticalMargin := topPaneHeight + txRxPaneHeight + footerHeight

		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMargin)
			m.viewport.Style = baseStyle
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMargin
		}
		m.lastDataRender = ""

	case tickMsg:
		newRender := m.renderDataPane()
		if m.lastDataRender != newRender {
			m.viewport.SetContent(newRender)
			m.lastDataRender = newRender
		}
		return m, tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
	}

	if m.textInput.Focused() {
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) handleCommand() {
	input := strings.TrimSpace(m.textInput.Value())
	defer m.textInput.SetValue("") // Clear input after every command
	if input == "" {
		return
	}
	m.log.Printf("TUI: User input: '%s'", input)

	var parts []string
	var inQuote bool
	var currentPart []rune
	for _, r := range input {
		if r == '"' {
			inQuote = !inQuote
		} else if r == ' ' && !inQuote {
			if len(currentPart) > 0 {
				parts = append(parts, string(currentPart))
				currentPart = []rune{}
			}
		} else {
			currentPart = append(currentPart, r)
		}
	}
	if len(currentPart) > 0 {
		parts = append(parts, string(currentPart))
	}
	if len(parts) == 0 {
		return
	}

	command := strings.ToLower(parts[0])
	switch command {
	case "set", "s", "clear", "c":
		if len(parts) < 2 {
			m.state.SetStatus("Error: command requires a point name.")
			return
		}
		pointName := parts[1]
		pointDef, found := m.state.Config.PointsByName[pointName]
		if !found {
			m.state.SetStatus(fmt.Sprintf("Error: Point '%s' not found.", pointName))
			return
		}
		if pointDef.Type != "bitmap" {
			m.state.SetStatus(fmt.Sprintf("Error: Point '%s' is not a bitmap point.", pointName))
			return
		}
		isSet := (command == "set" || command == "s")
		m.state.SendCommand(poller.SetBitCmd{Addr: pointDef.Address, Bit: *pointDef.Bit, Val: isSet})
		m.state.SetStatus(fmt.Sprintf("Queued %s \"%s\"", command, pointName))

	case "write", "w":
		if len(parts) < 3 {
			m.state.SetStatus("Error: 'write' requires target and value.")
			return
		}
		target := parts[1]
		valueStr := parts[2]

		var addr uint16
		var pointDef *config.PointDefinition

		addr64, err := strconv.ParseUint(target, 10, 16)
		if err == nil {
			addr = uint16(addr64)
			if pds, ok := m.state.Config.PointsByAddress[addr]; ok {
				pointDef = pds[0]
			}
		} else {
			pd, found := m.state.Config.PointsByName[target]
			if !found {
				m.state.SetStatus(fmt.Sprintf("Error: Point name '%s' not found.", target))
				return
			}
			pointDef = pd
			addr = pd.Address
		}

		valFloat, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			m.state.SetStatus(fmt.Sprintf("Error: Invalid numeric value '%s'.", valueStr))
			return
		}

		m.state.SendCommand(poller.WriteEngCmd{Addr: addr, EngVal: valFloat})

		pointName := fmt.Sprintf("Register %d", addr)
		if pointDef != nil {
			pointName = pointDef.PointName
		}
		m.state.SetStatus(fmt.Sprintf("Queued write to %s.", pointName))
	default:
		m.state.SetStatus(fmt.Sprintf("Error: Unknown command '%s'.", command))
	}
}

// --- VIEW ---
func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}
	topPanes := lipgloss.JoinHorizontal(lipgloss.Left,
		m.renderAlarmsPane(),
		m.renderStatusPane(),
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		topPanes,
		m.renderTxRxPane(),
		m.viewport.View(),
		m.renderFooter(),
	)
}

func (m Model) renderAlarmsPane() string {
	_, _, alarms, _, _, _, _ := m.state.GetSnapshot()
	var content strings.Builder
	content.WriteString(titleStyle.Render("Active Alarms") + "\n")
	if len(alarms) == 0 {
		content.WriteString("No active alarms.")
	} else {
		keys := make([]string, 0, len(alarms))
		for k := range alarms {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			alarm := alarms[k]
			style := alarmWarnStyle
			if alarm.Severity == "CRITICAL" {
				style = alarmCritStyle
			}
			content.WriteString(style.Render(fmt.Sprintf("[%s] %s", alarm.Severity, alarm.Message)) + "\n")
		}
	}
	return baseStyle.Width(m.viewport.Width/2 - 2).Height(6).Render(content.String())
}

func (m Model) renderStatusPane() string {
	_, _, _, tx, rx, timing, status := m.state.GetSnapshot()
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Status & Timing"),
		statusKeyStyle.Render("Status: ")+status,
		statusKeyStyle.Render("Last RTT: ")+fmt.Sprintf("%.2f ms", timing.RoundTripTimeMs),
		statusKeyStyle.Render("TX Count: ")+fmt.Sprintf("%d", tx.Count),
		statusKeyStyle.Render("RX Count: ")+fmt.Sprintf("%d", rx.Count),
	)
	return baseStyle.Width(m.viewport.Width/2 - 2).Height(6).Render(content)
}

func (m Model) renderTxRxPane() string {
	_, _, _, tx, rx, _, _ := m.state.GetSnapshot()
	var content strings.Builder
	content.WriteString(titleStyle.Render("Last Transaction (Hex)") + "\n")
	content.WriteString(fmt.Sprintf("TX [%d]: [%s] %s", tx.Count, tx.Timestamp, tx.Hex) + "\n")
	content.WriteString(fmt.Sprintf("RX [%d]: [%s] %s", rx.Count, rx.Timestamp, rx.Hex))
	return baseStyle.Render(content.String())
}

func (m Model) renderDataPane() string {
	datastore, prevData, _, _, _, _, _ := m.state.GetSnapshot()
	var sortedAddresses []uint16
	for addr := range m.state.Config.PointsByAddress {
		sortedAddresses = append(sortedAddresses, addr)
	}
	sort.Slice(sortedAddresses, func(i, j int) bool { return sortedAddresses[i] < sortedAddresses[j] })

	isSecondaryReg := make(map[uint16]bool)
	if _, ok := datastore[40008]; ok {
		isSecondaryReg[40009] = true
	}

	for _, addr := range sortedAddresses {
		if datastore[addr] != prevData[addr] {
			m.lastChange[addr] = time.Now()
		}
	}

	var content strings.Builder
	header := lipgloss.JoinHorizontal(lipgloss.Left,
		pointNameStyle.Render("Point Name"),
		pointAddressStyle.Render("Address"),
		pointRawStyle.Render("Raw Value"),
		pointValueStyle.Render("Value"),
		pointUnitStyle.Render("Unit"),
	)
	content.WriteString(titleStyle.Render(header) + "\n")

	for _, addr := range sortedAddresses {
		if isSecondaryReg[addr] {
			continue
		}
		pointDefs, _ := m.state.Config.PointsByAddress[addr]
		currentVal := datastore[addr]
		style := lipgloss.NewStyle()
		if time.Since(m.lastChange[addr]) < 2*time.Second {
			style = changedStyle
		}

		if pointDefs[0].Type == "bitmap" {
			line := lipgloss.JoinHorizontal(lipgloss.Left,
				pointNameStyle.Render(fmt.Sprintf("Reg %d (Bitmap)", addr)),
				pointAddressStyle.Render(""),
				pointRawStyle.Render(fmt.Sprintf("%d", currentVal)),
				pointValueStyle.Render(fmt.Sprintf("(%016b)", currentVal)),
			)
			content.WriteString(style.Render(line) + "\n")
			sort.Slice(pointDefs, func(i, j int) bool { return *pointDefs[i].Bit < *pointDefs[j].Bit })
			for _, pd := range pointDefs {
				isSet := (currentVal>>*pd.Bit)&1 == 1
				stateText := pd.StateOff
				if isSet {
					stateText = pd.StateOn
				}
				bitLine := lipgloss.JoinHorizontal(lipgloss.Left,
					pointNameStyle.Render("  "+pd.PointName),
					pointAddressStyle.Render(fmt.Sprintf(".../%d", *pd.Bit)),
					pointRawStyle.Render(""),
					pointValueStyle.Render(stateText),
				)
				content.WriteString(style.Render(bitLine) + "\n")
			}
		} else { // analog
			pd := pointDefs[0]
			var valStr, addrStr, rawStr string
			if pd.DataType == "signed" {
				rawStr = fmt.Sprintf("%d", int16(currentVal))
			} else {
				rawStr = fmt.Sprintf("%d", currentVal)
			}
			if addr == 40008 {
				highWord, _ := datastore[40008]
				lowWord, _ := datastore[40009]
				unixTime := (uint32(highWord) << 16) | uint32(lowWord)
				t := time.Unix(int64(unixTime), 0)
				valStr = t.UTC().Format("15:04:05 UTC")
				addrStr = "40008-9"
				rawStr = fmt.Sprintf("%d", unixTime)
			} else {
				scaledVal := poller.ScaleValue(currentVal, pd)
				valStr = fmt.Sprintf("%.2f", scaledVal)
				addrStr = fmt.Sprintf("%d", addr)
			}
			line := lipgloss.JoinHorizontal(lipgloss.Left,
				pointNameStyle.Render(pd.PointName),
				pointAddressStyle.Render(addrStr),
				pointRawStyle.Render(rawStr),
				pointValueStyle.Render(valStr),
				pointUnitStyle.Render(pd.Unit),
			)
			content.WriteString(style.Render(line) + "\n")
		}
	}
	return content.String()
}

func (m Model) renderFooter() string {
	help := "Use arrow keys or mouse to scroll | (i) to input command | (q) to quit"
	if m.textInput.Focused() {
		help = "Enter command and press Esc to cancel"
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.textInput.View(),
		help,
	)
}