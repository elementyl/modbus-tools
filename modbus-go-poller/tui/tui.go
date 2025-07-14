// ===== C:\Projects\modbus-tools\modbus-go-poller\tui\tui.go =====
package tui

import (
	"fmt"
	"log"
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

	changedStyle = lipgloss.NewStyle().Background(lipgloss.Color("202")).Foreground(lipgloss.Color("0"))

	pointNameStyle    = lipgloss.NewStyle().Width(28).Padding(0, 1)
	pointAcronymStyle = lipgloss.NewStyle().Width(22).Padding(0, 1)
	rtuAddressStyle   = lipgloss.NewStyle().Width(11).Align(lipgloss.Right).Padding(0, 1)
	pointAddressStyle = lipgloss.NewStyle().Width(11).Align(lipgloss.Right).Padding(0, 1)
	pointRawStyle     = lipgloss.NewStyle().Width(11).Align(lipgloss.Right).Padding(0, 1)
	pointValueStyle   = lipgloss.NewStyle().Width(22).Padding(0, 1)
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
	ti.Placeholder = "set PW1-START | write 40003 28 | raise PW1-STOP 5"
	ti.Focus()

	return Model{
		state:     ps,
		log:       logger,
		textInput: ti,
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
		topPaneHeight := 10
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
	case "raise", "r":
		if len(parts) < 2 {
			m.state.SetStatus("Error: 'raise' requires an acronym.")
			return
		}
		target := strings.ToUpper(parts[1])
		points, found := m.state.Config.PointsByAcronym[target]
		if !found || len(points) == 0 {
			m.state.SetStatus(fmt.Sprintf("Error: Acronym '%s' not found.", target))
			return
		}
		pointDef := points[0]
		if pointDef.IOType != "DO" {
			m.state.SetStatus(fmt.Sprintf("Error: Acronym '%s' is not a Digital Output (DO).", target))
			return
		}
		durationTenths := 10
		if len(parts) > 2 {
			var err error
			durationTenths, err = strconv.Atoi(parts[2])
			if err != nil || durationTenths <= 0 {
				m.state.SetStatus("Error: Invalid duration. Must be a positive integer.")
				return
			}
		}
		duration := time.Duration(durationTenths*100) * time.Millisecond
		m.state.SendCommand(poller.PulseDOUCmd{Addr: pointDef.Address, Bit: *pointDef.Bit, Duration: duration, Acronym: pointDef.Acronym})
		m.state.SetStatus(fmt.Sprintf("Queued raise %s for %.1fs", target, duration.Seconds()))
	case "write", "w":
		if len(parts) < 3 {
			m.state.SetStatus("Error: 'write' requires target and value.")
			return
		}
		target := parts[1]
		valueStr := parts[2]
		
		var addr uint16
		isAnalog := false
		if pds, found := m.state.Config.PointsByAcronym[strings.ToUpper(target)]; found && len(pds) > 0 {
			addr = pds[0].Address
			if pds[0].Type == "analog" {
				isAnalog = true
			}
		} else {
			addr64, err := strconv.ParseUint(target, 10, 16)
			if err != nil {
				m.state.SetStatus(fmt.Sprintf("Error: Target '%s' not found.", target))
				return
			}
			addr = uint16(addr64)
			if pds, found := m.state.Config.PointsByAddress[addr]; found && len(pds) > 0 {
				if pds[0].Type == "analog" {
					isAnalog = true
				}
			}
		}
		
		if isAnalog && strings.Contains(valueStr, ".") {
			valFloat, err := strconv.ParseFloat(valueStr, 64)
			if err != nil {
				m.state.SetStatus(fmt.Sprintf("Error: Invalid float value '%s'.", valueStr))
				return
			}
			m.state.SendCommand(poller.WriteEngCmd{Addr: addr, EngVal: valFloat})
			m.state.SetStatus(fmt.Sprintf("Queued eng write to %s.", target))
		} else {
			valInt, err := strconv.ParseUint(valueStr, 10, 16)
			if err != nil {
				m.state.SetStatus(fmt.Sprintf("Error: Invalid integer value '%s'.", valueStr))
				return
			}
			m.state.SendCommand(poller.WriteRawCmd{Addr: addr, Value: uint16(valInt)})
			m.state.SetStatus(fmt.Sprintf("Queued raw write to %s.", target))
		}
	default:
		m.state.SetStatus(fmt.Sprintf("Error: Unknown command '%s'.", command))
	}
}

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
	paneWidth := m.viewport.Width / 2
	return baseStyle.Width(paneWidth).Height(8).Render(content.String())
}

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
	leftPaneWidth := m.viewport.Width / 2
	rightPaneWidth := m.viewport.Width - leftPaneWidth - 3
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
	datastore, prevData, lastChange, _, _, _, _, _ := m.state.GetSnapshot()

	maxNameLen := 0
	maxAcronymLen := 0
	for _, points := range m.state.Config.PointsByAddress {
		for _, p := range points {
			nameLen := len(p.PointName)
			if p.Type == "bitmap" {
				nameLen += 2
			}
			if nameLen > maxNameLen {
				maxNameLen = nameLen
			}
			if len(p.Acronym) > maxAcronymLen {
				maxAcronymLen = len(p.Acronym)
			}
		}
	}
	for addr, points := range m.state.Config.PointsByAddress {
		if len(points) > 0 && points[0].Type == "bitmap" {
			regHeaderLen := len(fmt.Sprintf("Reg %d (Bitmap)", addr))
			if regHeaderLen > maxNameLen {
				maxNameLen = regHeaderLen
			}
		}
	}
	nameWidth, acronymWidth := maxNameLen, maxAcronymLen
	rtuAddrWidth, modbusAddrWidth, rawValWidth, valWidth, unitWidth := 10, 10, 12, 22, 10

	var sortedAddresses []uint16
	for addr := range m.state.Config.PointsByAddress {
		sortedAddresses = append(sortedAddresses, addr)
	}
	sort.Slice(sortedAddresses, func(i, j int) bool { return sortedAddresses[i] < sortedAddresses[j] })

	excludeHighlight := map[string]bool{"PW1-HBEAT": true, "SCADA Clock": true}
	var content strings.Builder
	header := lipgloss.JoinHorizontal(lipgloss.Left,
		pointNameStyle.Width(nameWidth).Render("Point Name"),
		pointAcronymStyle.Width(acronymWidth).Render("Acronym"),
		rtuAddressStyle.Width(rtuAddrWidth).Render("RTU Addr"),
		pointAddressStyle.Width(modbusAddrWidth).Render("Modbus Addr"),
		pointRawStyle.Width(rawValWidth).Render("Raw Value"),
		pointValueStyle.Width(valWidth).Render("Value"),
		pointUnitStyle.Width(unitWidth).Render("Unit"),
	)
	content.WriteString(titleStyle.Width(m.viewport.Width).Render(header) + "\n")

	for _, addr := range sortedAddresses {
		if addr == 40009 {
			continue
		}
		pointDefs, _ := m.state.Config.PointsByAddress[addr]
		pd := pointDefs[0]
		currentVal := datastore[addr]

		style := lipgloss.NewStyle()
		if !excludeHighlight[pd.Acronym] && !excludeHighlight[pd.PointName] && time.Since(lastChange[addr]) < 3*time.Second {
			style = changedStyle
		}

		if pd.Type == "bitmap" {
			line := lipgloss.JoinHorizontal(lipgloss.Left,
				pointNameStyle.Width(nameWidth).Render(fmt.Sprintf("Reg %d (Bitmap)", addr)),
				pointAcronymStyle.Width(acronymWidth).Render(""),
				rtuAddressStyle.Width(rtuAddrWidth).Render(""),
				pointAddressStyle.Width(modbusAddrWidth).Render(fmt.Sprintf("%d", addr)),
				pointRawStyle.Width(rawValWidth).Render(fmt.Sprintf("%d", currentVal)),
				pointValueStyle.Width(valWidth).Render(fmt.Sprintf("(%016b)", currentVal)),
				pointUnitStyle.Width(unitWidth).Render(""),
			)
			content.WriteString(style.Render(line) + "\n")

			sort.Slice(pointDefs, func(i, j int) bool { return *pointDefs[i].Bit < *pointDefs[j].Bit })
			for _, bitPd := range pointDefs {
				prevRegisterVal := prevData[addr]
				currentBitVal := (currentVal >> *bitPd.Bit) & 1
				prevBitVal := (prevRegisterVal >> *bitPd.Bit) & 1

				isSet := currentBitVal == 1
				stateText := bitPd.StateOff
				if isSet {
					stateText = bitPd.StateOn
				}

				bitStyle := lipgloss.NewStyle()
				if !excludeHighlight[bitPd.Acronym] && currentBitVal != prevBitVal {
					bitStyle = changedStyle
				}

				bitLine := lipgloss.JoinHorizontal(lipgloss.Left,
					pointNameStyle.Width(nameWidth).Render("  "+bitPd.PointName),
					pointAcronymStyle.Width(acronymWidth).Render(bitPd.Acronym),
					rtuAddressStyle.Width(rtuAddrWidth).Render(fmt.Sprintf("%s %d", bitPd.IOType, bitPd.IONumber)),
					pointAddressStyle.Width(modbusAddrWidth).Render(fmt.Sprintf(".../%d", *bitPd.Bit)),
					pointRawStyle.Width(rawValWidth).Render(fmt.Sprintf("%d", currentBitVal)),
					pointValueStyle.Width(valWidth).Render(stateText),
					pointUnitStyle.Width(unitWidth).Render(""),
				)
				content.WriteString(bitStyle.Render(bitLine) + "\n")
			}
		} else { // analog
			var valStr, addrStr, rawStr string
			if addr == 40008 {
				highWord := uint32(datastore[pd.Address])
				lowWord := uint32(datastore[pd.Address+1])
				unixTime := (highWord << 16) | lowWord
				t := time.Unix(int64(unixTime), 0)
				valStr = t.Local().Format("2006-01-02 15:04:05")
				addrStr = "40008-9"
				rawStr = fmt.Sprintf("%d", unixTime)
			} else {
				if pd.DataType == "signed" {
					rawStr = fmt.Sprintf("%d", int16(currentVal))
				} else {
					rawStr = fmt.Sprintf("%d", currentVal)
				}
				scaledVal := poller.ScaleValue(currentVal, pd)
				valStr = fmt.Sprintf("%.2f", scaledVal)
				addrStr = fmt.Sprintf("%d", addr)
			}
			line := lipgloss.JoinHorizontal(lipgloss.Left,
				pointNameStyle.Width(nameWidth).Render(pd.PointName),
				pointAcronymStyle.Width(acronymWidth).Render(pd.Acronym),
				rtuAddressStyle.Width(rtuAddrWidth).Render(fmt.Sprintf("%s %d", pd.IOType, pd.IONumber)),
				pointAddressStyle.Width(modbusAddrWidth).Render(addrStr),
				pointRawStyle.Width(rawValWidth).Render(rawStr),
				pointValueStyle.Width(valWidth).Render(valStr),
				pointUnitStyle.Width(unitWidth).Render(pd.Unit),
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