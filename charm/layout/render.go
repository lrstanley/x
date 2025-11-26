// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package layout

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type LayerMouseMsg struct {
	tea.MouseMsg
	LayerID string
}

// RenderString renders the provided child/layout/etc into a string.
func RenderString(width, height int, child any) string {
	if child == nil || width == 0 || height == 0 {
		return ""
	}

	layer := resolveLayer(child, width, height)
	if layer == nil {
		return ""
	}

	return lipgloss.NewCanvas(width, height).Compose(layer).Render()
}

// RenderView renders the provided child/layout/etc onto an existing [tea.View],
// including applying a callback to the view to handle mouse events, which will
// send a downstream [LayerMouseMsg] to the model.
func RenderView(view *tea.View, width, height int, child any) {
	if child == nil || width == 0 || height == 0 {
		return
	}

	layer := resolveLayer(child, width, height)
	if layer == nil {
		return
	}

	canvas := lipgloss.NewCanvas(width, height).Compose(layer)

	if view.MouseMode != tea.MouseModeNone {
		view.OnMouse = func(msg tea.MouseMsg) tea.Cmd {
			if id := layer.Hit(msg.Mouse().X, msg.Mouse().Y); id != "" {
				return func() tea.Msg {
					return LayerMouseMsg{
						MouseMsg: msg,
						LayerID:  id,
					}
				}
			}
			return nil
		}
	}

	printLayer(layer)

	view.SetContent(canvas.Render())
}
