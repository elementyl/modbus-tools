// ===== C:\Projects\modbus-tools\modbus-go-server\tui\tui.go =====
package tui

import (
	"fmt"
	"log"
	// --- THIS IS THE FIX ---
	"modbus-tools/modbus-go-server/config"
	"modbus-tools/modbus-go-server/server"
	// --- END OF FIX ---
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Model struct {
	server    *server.Server
	log       *log.Logger
	datastore map[uint16]uint16
	prevData  map[uint16]uint16
	textInput textinput.Model
	status    string
	width, height int
}
type tickMsg time.Time

func NewModel(s *server.Server, logger *log.Logger) Model {
	ti := textinput.New()
	ti.Placeholder = "e.g., set \"Motor Running\" | write 40002 25.5"
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 80
	return Model{
		server:    s,
		log:       logger,
		datastore: make(map[uint16]uint16),
		prevData:  make(map[uint16]uint16),
		textInput: ti,
		status:    "Press Ctrl+C to quit.",
	}
}

func (m Model) Init() tea.Cmd {
	return doTick
}

func doTick() tea.Msg {
	time.Sleep(500 * time.Millisecond)
	return tickMsg{}
}

func (m *Model) handleCommand() {
	input := strings.TrimSpace(m.textInput.Value())
	if input == "" {
		return
	}
	parts := strings.Fields(input)
	command := strings.ToLower(parts[0])

	switch command {
	case "set", "clear":
		if len(parts) < 2 {
			m.status = "Error: 'set'/'clear' require a point name."
			return
		}
		pointName := strings.Join(parts[1:], " ")
		pointName = strings.Trim(pointName, "\"")
		addr, bit, found := config.FindPointByName(pointName)
		if !found {
			m.status = fmt.Sprintf("Error: Point '%s' not found.", pointName)
			return
		}
		m.server.CommandChan <- server.SetBitCmd{Addr: addr, Bit: bit, Val: command == "set"}
		m.status = fmt.Sprintf("Success: Queued %s \"%s\".", command, pointName)
	case "write", "w":
		if len(parts) < 3 {
			m.status = "Error: 'write' requires address and value."
			return
		}
		addrInt, err := strconv.ParseUint(parts[1], 10, 16)
		if err != nil {
			m.status = fmt.Sprintf("Error: Invalid address '%s'.", parts[1])
			return
		}
		valFloat, err := strconv.ParseFloat(parts[2], 64)
		if err != nil {
			m.status = fmt.Sprintf("Error: Invalid value '%s'.", parts[2])
			return
		}
		m.server.CommandChan <- server.WriteEngCmd{Addr: uint16(addrInt), EngVal: valFloat}
		m.status = fmt.Sprintf("Success: Queued write %s to address %d.", parts[2], addrInt)
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
		m.prevData = m.datastore
		m.datastore = m.server.GetDatastoreSnapshot()
		return m, doTick
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	var b strings.Builder
	changedStyle := lipgloss.NewStyle().Reverse(true)
	b.WriteString("--- Modbus Server (Go Version) ---\n\n")

	var addresses []int
	addrMap := make(map[int]struct{})
	for addr := range m.datastore {
		addrMap[int(addr)] = struct{}{}
	}
	for addrInt := range addrMap {
		addresses = append(addresses, addrInt)
	}
	sort.Ints(addresses)

	for _, addrInt := range addresses {
		addr := uint16(addrInt)
		regDef := config.GetRegisterDefinition(addr)
		if regDef == nil {
			continue
		}
		currentVal := m.datastore[addr]
		prevVal := m.prevData[addr]
		isChanged := currentVal != prevVal && len(m.prevData) > 0
		var line string
		if regDef.Type == "bitmap" {
			var onNames []string
			for i := uint(0); i < 16; i++ {
				if (currentVal>>i)&1 == 1 {
					if name, ok := regDef.Points[i]; ok {
						onNames = append(onNames, name)
					}
				}
			}
			onStr := "(None)"
			if len(onNames) > 0 {
				onStr = strings.Join(onNames, ", ")
			}
			line = fmt.Sprintf("Reg %d (%-25s): %-6d (%016b)\n  ON: %s\n", addr, regDef.Name, currentVal, currentVal, onStr)
		} else {
			scaledVal := config.ScaleValue(currentVal, regDef.Scaling)
			line = fmt.Sprintf("Reg %d (%-25s): Raw: %-5d Scaled: %7.2f %s\n", addr, regDef.Name, currentVal, scaledVal, regDef.Unit)
		}

		if isChanged {
			b.WriteString(changedStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	footer := fmt.Sprintf("\n%s\n%s", m.textInput.View(), m.status)
	viewContent := b.String() + footer
	return viewContent
}