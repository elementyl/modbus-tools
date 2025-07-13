package tui

import (
	"fmt"
	"log"
	"modbus-tools/modbus-go-poller/config"
	"modbus-tools/modbus-go-poller/poller"
	"modbus-tools/modbus-go-poller/version"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- STYLES (Final Polished Version) ---
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

	changedStyle = lipgloss.NewStyle().Background(lipgloss.Color("202")).Foreground(lipgloss.Color("0"))

	// Column styles with padding for separation
	pointNameStyle    = lipgloss.NewStyle().Width(28).Padding(0, 1)
	pointAcronymStyle = lipgloss.NewStyle().Width(22).Padding(0, 1)
	rtuAddressStyle   = lipgloss.NewStyle().Width(11).Align(lipgloss.Right).Padding(0, 1)
	pointAddressStyle = lipgloss.NewStyle().Width(11).Align(lipgloss.Right).Padding(0, 1)
	pointRawStyle     = lipgloss.NewStyle().Width(11).Align(lipgloss.Right).Padding(0, 1)
	pointValueStyle   = lipgloss.NewStyle().Width(22).Align(lipgloss.Right).Padding(0, 1)
	pointUnitStyle    = lipgloss.NewStyle().Width(10).Padding(0, 1)
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
}

func NewModel(ps *poller.PollerState, logger *log.Logger) Model {
	ti := textinput.New()
	ti.Placeholder = "set PW1-START | write 40003 28 | write PW1-TANK-SP 25.5"
	ti.Focus()

	return Model{
		state:     ps,
		log:       logger,
		textInput: ti,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Tick(250 * time.Millisecond, func(t time.Time) tea.Msg {
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
				return m, nil
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
		// Layout constants
		topPaneHeight := 10    // The height of the panes (8) + borders (2)
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
	defer m.textInput.SetValue("")
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
			m.state.SetStatus("Error: command requires an acronym.")
			return
		}
		target := parts[1]
		points, found := m.state.Config.PointsByAcronym[strings.ToUpper(target)]
		if !found || len(points) == 0 {
			m.state.SetStatus(fmt.Sprintf("Error: Acronym '%s' not found.", target))
			return
		}
		pointDef := points[0]
		if pointDef.Type != "bitmap" {
			m.state.SetStatus(fmt.Sprintf("Error: Acronym '%s' is not a bitmap point.", target))
			return
		}
		isSet := (command == "set" || command == "s")
		m.state.SendCommand(poller.SetBitCmd{Addr: pointDef.Address, Bit: *pointDef.Bit, Val: isSet})
		m.state.SetStatus(fmt.Sprintf("Queued %s %s", command, target))

	case "write", "w":
		if len(parts) < 3 {
			m.state.SetStatus("Error: 'write' requires target and value.")
			return
		}
		target := parts[1]
		valueStr := parts[2]

		var addr uint16
		var pointDef *config.PointDefinition

		if pds, found := m.state.Config.PointsByAcronym[strings.ToUpper(target)]; found {
			pointDef = pds[0]
			addr = pointDef.Address
		} else {
			addr64, err := strconv.ParseUint(target, 10, 16)
			if err != nil {
				m.state.SetStatus(fmt.Sprintf("Error: Target '%s' not found.", target))
				return
			}
			addr = uint16(addr64)
			if pds, ok := m.state.Config.PointsByAddress[addr]; ok {
				pointDef = pds[0]
			}
		}

		valFloat, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			m.state.SetStatus(fmt.Sprintf("Error: Invalid numeric value '%s'.", valueStr))
			return
		}

		m.state.SendCommand(poller.WriteEngCmd{Addr: addr, EngVal: valFloat})

		displayName := target
		if pointDef != nil && pointDef.Acronym != "" {
			displayName = pointDef.Acronym
		}
		m.state.SetStatus(fmt.Sprintf("Queued write to %s.", displayName))
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
	_, _, _, alarms, _, _, _, _ := m.state.GetSnapshot()
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
	// Give this pane roughly half the screen width
	paneWidth := m.viewport.Width / 2
	return baseStyle.Width(paneWidth).Height(8).Render(content.String())
}

// In tui.go
func (m Model) renderStatusPane() string {
	_, _, _, _, tx, rx, timing, status := m.state.GetSnapshot()
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Status & Timing"),
		statusKeyStyle.Render("Version:  ")+version.Version,
		statusKeyStyle.Render("Last RTT: ")+fmt.Sprintf("%.2f ms", timing.RoundTripTimeMs),
		statusKeyStyle.Render("TX Count: ")+fmt.Sprintf("%d", tx.Count),
		statusKeyStyle.Render("RX Count: ")+fmt.Sprintf("%d", rx.Count),
		" ",
		statusKeyStyle.Render("Status:"),
		status,
	)

	// --- THIS IS THE CORRECTED WIDTH CALCULATION ---
	// Calculate the width of the left pane
	leftPaneWidth := m.viewport.Width / 2

	// The width of this pane is the total width minus the left pane's width,
	// minus 4 characters for borders and spacing.
	rightPaneWidth := m.viewport.Width - leftPaneWidth - 4

	return baseStyle.Width(rightPaneWidth).Height(8).Render(content)
}

func (m Model) renderTxRxPane() string {
	_, _, _, _, tx, rx, _, _ := m.state.GetSnapshot()
	var content strings.Builder
	content.WriteString(titleStyle.Render("Last Transaction (Hex)") + "\n")
	content.WriteString(fmt.Sprintf("TX [%d]: [%s] %s", tx.Count, tx.Timestamp, tx.Hex) + "\n")
	content.WriteString(fmt.Sprintf("RX [%d]: [%s] %s", rx.Count, rx.Timestamp, rx.Hex))
	return baseStyle.Width(m.viewport.Width - 2).Render(content.String())
}

func (m Model) renderDataPane() string {
	datastore, _, lastChange, _, _, _, _, _ := m.state.GetSnapshot()

	var sortedAddresses []uint16
	for addr := range m.state.Config.PointsByAddress {
		sortedAddresses = append(sortedAddresses, addr)
	}
	sort.Slice(sortedAddresses, func(i, j int) bool { return sortedAddresses[i] < sortedAddresses[j] })

	isSecondaryReg := make(map[uint16]bool)
	if _, ok := datastore[40008]; ok {
		isSecondaryReg[40009] = true
	}

	excludeHighlight := map[string]bool{
		"PW1-HBEAT": true,
	}

	var content strings.Builder
	header := lipgloss.JoinHorizontal(lipgloss.Left,
		pointNameStyle.Render("Point Name"),
		pointAcronymStyle.Render("Acronym"),
		rtuAddressStyle.Render("RTU Addr"),
		pointAddressStyle.Render("Modbus Addr"),
		pointRawStyle.Render("Raw Value"),
		pointValueStyle.Render("Value"),
		pointUnitStyle.Render("Unit"),
	)
	content.WriteString(titleStyle.Width(m.viewport.Width).Render(header) + "\n")

	for _, addr := range sortedAddresses {
		if isSecondaryReg[addr] {
			continue
		}
		pointDefs, _ := m.state.Config.PointsByAddress[addr]
		currentVal := datastore[addr]
		pd := pointDefs[0]
		style := lipgloss.NewStyle()
		if !excludeHighlight[pd.Acronym] && time.Since(lastChange[addr]) < 3*time.Second {
			style = changedStyle
		}

		if pd.Type == "bitmap" {
			line := lipgloss.JoinHorizontal(lipgloss.Left,
				pointNameStyle.Render(fmt.Sprintf("Reg %d (Bitmap)", addr)),
				pointAcronymStyle.Render(""),
				rtuAddressStyle.Render(""),
				pointAddressStyle.Render(fmt.Sprintf("%d", addr)),
				pointRawStyle.Render(fmt.Sprintf("%d", currentVal)),
				pointValueStyle.Render(fmt.Sprintf("(%016b)", currentVal)),
				pointUnitStyle.Render(""),
			)
			content.WriteString(style.Render(line) + "\n")

			sort.Slice(pointDefs, func(i, j int) bool { return *pointDefs[i].Bit < *pointDefs[j].Bit })
			for _, bitPd := range pointDefs {
				isSet := (currentVal>>*bitPd.Bit)&1 == 1
				stateText := bitPd.StateOff
				if isSet {
					stateText = bitPd.StateOn
				}
				var bitVal int
				if isSet {
					bitVal = 1
				}
				bitStyle := lipgloss.NewStyle()
				if !excludeHighlight[bitPd.Acronym] && time.Since(lastChange[addr]) < 3*time.Second {
					bitStyle = changedStyle
				}
				bitLine := lipgloss.JoinHorizontal(lipgloss.Left,
					pointNameStyle.Render("  "+bitPd.PointName),
					pointAcronymStyle.Render(bitPd.Acronym),
					rtuAddressStyle.Render(fmt.Sprintf("%s %d", bitPd.IOType, bitPd.IONumber)),
					pointAddressStyle.Render(fmt.Sprintf(".../%d", *bitPd.Bit)),
					pointRawStyle.Render(fmt.Sprintf("%d", bitVal)),
					pointValueStyle.Render(stateText),
					pointUnitStyle.Render(""),
				)
				content.WriteString(bitStyle.Render(bitLine) + "\n")
			}
		} else { // analog
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
				valStr = t.Local().Format("2006-01-02 15:04:05")
				addrStr = "40008-9"
				rawStr = fmt.Sprintf("%d", unixTime)
			} else {
				scaledVal := poller.ScaleValue(currentVal, pd)
				valStr = fmt.Sprintf("%.2f", scaledVal)
				addrStr = fmt.Sprintf("%d", addr)
			}
			line := lipgloss.JoinHorizontal(lipgloss.Left,
				pointNameStyle.Render(pd.PointName),
				pointAcronymStyle.Render(pd.Acronym),
				rtuAddressStyle.Render(fmt.Sprintf("%s %d", pd.IOType, pd.IONumber)),
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