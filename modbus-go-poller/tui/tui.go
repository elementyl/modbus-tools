package tui

import (
	"fmt"
	"log"
	"modbus-go-poller/poller"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tickMsg time.Time
type Model struct {
	state          *poller.PollerState
	log            *log.Logger
	textInput      textinput.Model
	status         string
	datastore      map[uint16]uint16
	prevData       map[uint16]uint16
	activeAlarms   map[string]poller.ActiveAlarm
	lastTx, lastRx poller.TxRxInfo
	timing         poller.TimingInfo
}

func doTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
func NewModel(ps *poller.PollerState, logger *log.Logger) Model {
	ti := textinput.New()
	ti.Placeholder = "e.g., set \"Motor Running\" | write \"Tank Level SP\" 25.5"
	ti.Focus()
	return Model{
		state:     ps,
		log:       logger,
		textInput: ti,
		datastore: make(map[uint16]uint16),
		prevData:  make(map[uint16]uint16),
	}
}
func (m Model) Init() tea.Cmd {
	return doTick()
}
func (m *Model) handleCommand() {
	input := strings.TrimSpace(m.textInput.Value())
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
			m.status = "Error: command requires a point name."
			return
		}
		pointName := parts[1]
		pointDef, found := m.state.Config.PointsByName[pointName]
		if !found || pointDef.Type != "bitmap" {
			m.status = fmt.Sprintf("Error: Bitmap point '%s' not found.", pointName)
			return
		}
		isSet := (command == "set" || command == "s")
		m.state.SendCommand(poller.SetBitCmd{Addr: pointDef.Address, Bit: *pointDef.Bit, Val: isSet})
		m.status = fmt.Sprintf("Queued %s \"%s\"", command, pointName)
	case "write", "w":
		if len(parts) < 3 {
			m.status = "Error: 'write' requires point name and value."
			return
		}
		pointName := parts[1]
		valStr := parts[2]
		pointDef, found := m.state.Config.PointsByName[pointName]
		if !found || pointDef.Type != "analog" {
			m.status = fmt.Sprintf("Error: Analog point '%s' not found.", pointName)
			return
		}
		valFloat, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			m.status = fmt.Sprintf("Error: Invalid value '%s'.", valStr)
			return
		}
		m.state.SendCommand(poller.WriteEngCmd{Addr: pointDef.Address, EngVal: valFloat})
		m.status = fmt.Sprintf("Queued write %.2f to %s.", valFloat, pointName)
	default:
		m.status = fmt.Sprintf("Error: Unknown command '%s'.", command)
	}
	m.textInput.SetValue("")
}
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			m.handleCommand()
			return m, nil
		}
	case tickMsg:
		m.datastore, m.prevData, m.activeAlarms, m.lastTx, m.lastRx, m.timing, m.status = m.state.GetSnapshot()
		return m, doTick()
	case tea.WindowSizeMsg:
		return m, nil
	}
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}
func (m Model) View() string {
	var b strings.Builder
	changedStyle := lipgloss.NewStyle().Reverse(true)
	alarmCritStyle := lipgloss.NewStyle().Background(lipgloss.Color("9")).Foreground(lipgloss.Color("15"))
	alarmWarnStyle := lipgloss.NewStyle().Background(lipgloss.Color("11")).Foreground(lipgloss.Color("0"))

	b.WriteString("--- Active Alarms ---\n")
	if len(m.activeAlarms) == 0 {
		b.WriteString("No active alarms.\n")
	} else {
		var keys []string
		for k := range m.activeAlarms {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			alarm := m.activeAlarms[k]
			if alarm.Severity == "CRITICAL" {
				b.WriteString(alarmCritStyle.Render(fmt.Sprintf("[%s] %s", alarm.Severity, alarm.Message)))
			} else {
				b.WriteString(alarmWarnStyle.Render(fmt.Sprintf("[%s] %s", alarm.Severity, alarm.Message)))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString("--- Interpreted Register Values ---\n")

	var sortedAddresses []uint16
	for addr := range m.state.Config.PointsByAddress {
		sortedAddresses = append(sortedAddresses, addr)
	}
	sort.Slice(sortedAddresses, func(i, j int) bool { return sortedAddresses[i] < sortedAddresses[j] })

	for _, addr := range sortedAddresses {
		pointDefs, ok := m.state.Config.PointsByAddress[addr]
		if !ok || len(pointDefs) == 0 {
			continue
		}

		currentVal := m.datastore[addr]
		prevVal, hasPrev := m.prevData[addr]
		isChanged := hasPrev && currentVal != prevVal

		var line string

		if pointDefs[0].Type == "analog" {
			analogPoint := pointDefs[0]
			var baseLine string

			var rawDisplayValue interface{}
			if analogPoint.DataType == "signed" {
				rawDisplayValue = int16(currentVal)
			} else {
				rawDisplayValue = currentVal
			}

			if addr == 40008 {
				highWord := m.datastore[40008]
				lowWord := m.datastore[40009]
				unixTime := (uint32(highWord) << 16) | uint32(lowWord)
				t := time.Unix(int64(unixTime), 0)
				baseLine = fmt.Sprintf("Reg %d (%-25s): Raw: %-5d -> %s", addr, analogPoint.PointName, rawDisplayValue, t.UTC().Format("2006-01-02 15:04:05 UTC"))
			} else if addr == 40009 {
				baseLine = fmt.Sprintf("Reg %d (%-25s): Raw: %-5d -> (see 40008)", addr, analogPoint.PointName, rawDisplayValue)
			} else {
				scaledVal := poller.ScaleValue(currentVal, analogPoint)
				baseLine = fmt.Sprintf("Reg %d (%-25s): Raw: %-5d Scaled: %7.2f %s", addr, analogPoint.PointName, rawDisplayValue, scaledVal, analogPoint.Unit)
			}
			line = baseLine + "\n"

		} else {
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Reg %d (Bitmap Register          ): %-6d (%016b)\n", addr, currentVal, currentVal))

			sort.Slice(pointDefs, func(i, j int) bool { return *pointDefs[i].Bit < *pointDefs[j].Bit })

			for _, pointDef := range pointDefs {
				isSet := (currentVal>>*pointDef.Bit)&1 == 1
				stateText := pointDef.StateOff
				if isSet {
					stateText = pointDef.StateOn
				}

				isAbnormal := false
				if pointDef.NormalState != nil {
					isAbnormal = (isSet && *pointDef.NormalState == 0) || (!isSet && *pointDef.NormalState == 1)
				}
				stateStyle := lipgloss.NewStyle()
				if isAbnormal {
					stateStyle = stateStyle.Bold(true).Foreground(lipgloss.Color("11"))
				}

				sb.WriteString(fmt.Sprintf("  Bit %2d: %-28s: %s\n", *pointDef.Bit, pointDef.PointName, stateStyle.Render(stateText)))
			}
			line = sb.String()
		}

		if isChanged {
			b.WriteString(changedStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	b.WriteString("--- Timing Analysis ---\n")
	b.WriteString(fmt.Sprintf("  Round Trip Time: %.2f ms\n\n", m.timing.RoundTripTimeMs))
	b.WriteString("--- Last Transaction (Hex) ---\n")
	b.WriteString(fmt.Sprintf("  TX [%d]: [%s] %s\n", m.lastTx.Count, m.lastTx.Timestamp, m.lastTx.Hex))
	b.WriteString(fmt.Sprintf("  RX [%d]: [%s] %s\n", m.lastRx.Count, m.lastRx.Timestamp, m.lastRx.Hex))
	footer := fmt.Sprintf("\n\n%s\n%s", m.textInput.View(), m.status)
	return b.String() + footer
}