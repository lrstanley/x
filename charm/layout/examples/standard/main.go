package main

import (
	"fmt"
	"log/slog"
	"os"

	tea "charm.land/bubbletea/v2"
	tint "github.com/lrstanley/bubbletint/v2"
	"github.com/lrstanley/x/charm/layout"
)

var theme = tint.TintGitHubDark

type model struct {
	width  int
	height int

	sidebar   *sidebarModel
	reader    *readerModel
	statusbar *statusbarModel
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.sidebar.Init(),
		m.reader.Init(),
		m.statusbar.Init(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case layout.LayerMouseMsg:
		slog.Debug("Layer hit", "layer", msg.LayerID, "mouse", msg.Mouse(), "type", fmt.Sprintf("%T", msg.MouseMsg)) //nolint:sloglint
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
	return m, tea.Batch(
		m.sidebar.Update(msg),
		m.reader.Update(msg),
	)
}

func (m model) View() tea.View {
	var view tea.View

	view.BackgroundColor = theme.Bg
	view.ForegroundColor = theme.Fg
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion

	layout.RenderView(
		&view,
		m.width,
		m.height,
		layout.Vertical(
			layout.BottomPadding(1, layout.Columns(
				layout.NewCell(m.sidebar).Size(20), // .Percent(0.3).HideSize(20),
				layout.NewCell(m.reader),
			)),
			m.statusbar,
		),
	)

	return view
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

	_, err = tea.NewProgram(model{
		sidebar:   newSidebar(),
		reader:    newReader(),
		statusbar: newStatusbar(),
	}).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Urgh:", err)
		os.Exit(1)
	}
}
