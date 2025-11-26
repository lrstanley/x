package main

import (
	"fmt"
	"image/color"
	"log/slog"
	"os"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"
	"github.com/lrstanley/x/charm/layout"
)

type model struct {
	width  int
	height int
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case layout.LayerMouseMsg:
		slog.Debug("Layer hit", "layer", msg.LayerID, "mouse", msg.Mouse, "type", fmt.Sprintf("%T", msg.Mouse)) //nolint:sloglint
		if mouse, ok := msg.MouseMsg.(tea.MouseClickMsg); ok {
			return m, tea.Printf("Layer hit at %d, %d", mouse.X, mouse.Y)
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	var view tea.View

	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion

	footer := lipgloss.NewStyle().
		Foreground(charmtone.Oyster).
		AlignVertical(lipgloss.Bottom).
		Render("Press any key to swap the cards, or q to quit.")

	layout.RenderView(
		&view,
		m.width,
		m.height,
		layout.Stack(
			layout.Vertical(
				layout.Horizontal(
					layout.Stack(
						newCard("card_1", "Hello", charmtone.Charple),
						newCard("card_2", "left horizontal", charmtone.Charple).X(4).Y(2),
					),
					layout.Space(),
					layout.Stack(
						newCard("card_3", "Hello", charmtone.Charple),
						newCard("card_4", "right horizontal", charmtone.Charple).X(4).Y(2),
					),
				),
				layout.Frame(
					lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(charmtone.Charple),
					"framed text",
				),
				layout.Space(),
				footer,
			),
			layout.Center(
				newCard("card_5", "Centered Card", charmtone.Charple),
			),
		),
	)

	return view
}

func newCard(id, str string, border color.Color) layout.Layer {
	return layout.NewLayer(
		id,
		lipgloss.NewStyle().
			Width(20).
			Height(10).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Align(lipgloss.Center, lipgloss.Center).
			Render(str),
	)
}

func main() {
	// log slog to file, using json.
	f, err := os.OpenFile("debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	slog.SetDefault(slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	_, err = tea.NewProgram(model{}).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Urgh:", err)
		os.Exit(1)
	}
}
