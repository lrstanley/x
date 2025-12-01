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

	return lipgloss.NewCompositor(layer).Render()
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

	comp := lipgloss.NewCompositor(layer)

	if view.MouseMode != tea.MouseModeNone {
		view.OnMouse = func(msg tea.MouseMsg) tea.Cmd {
			if hit := comp.Hit(msg.Mouse().X, msg.Mouse().Y); !hit.Empty() {
				x := msg.Mouse().X - hit.Bounds().Min.X
				y := msg.Mouse().Y - hit.Bounds().Min.Y

				return func() tea.Msg {
					var nmsg tea.MouseMsg
					switch msg := msg.(type) {
					case tea.MouseClickMsg:
						msg.X = x
						msg.Y = y
						nmsg = msg
					case tea.MouseReleaseMsg:
						msg.X = x
						msg.Y = y
						nmsg = msg
					case tea.MouseMotionMsg:
						msg.X = x
						msg.Y = y
						nmsg = msg
					case tea.MouseWheelMsg:
						msg.X = x
						msg.Y = y
						nmsg = msg
					default:
						// We don't know what to do, return the original message with
						// absolute coordinates.
						return LayerMouseMsg{
							MouseMsg: msg,
							LayerID:  hit.ID(),
						}
					}

					return LayerMouseMsg{
						MouseMsg: nmsg,
						LayerID:  hit.ID(),
					}
				}
			}
			return nil
		}
	}

	// printLayer(layer)
	view.SetContent(comp.Render())
}
