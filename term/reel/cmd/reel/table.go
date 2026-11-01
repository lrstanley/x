// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/term"
)

// printTable prints rows under headers: on a terminal, as a table, and
// otherwise as tab-separated rows without the headers, for scripts. With no
// rows, a terminal gets empty instead, which says why, and hint, a command
// to try next; scripts get nothing.
func printTable(headers []string, rows [][]string, empty, hint string) error {
	tty := term.IsTerminal(os.Stdout.Fd())
	if tty && len(rows) == 0 {
		faint := lipgloss.NewStyle().Faint(true)
		_, err := lipgloss.Println(faint.Render(empty) + "\n" + faint.Render("Try: ") + lipgloss.NewStyle().Bold(true).Render(hint))
		return err
	}
	if !tty {
		for _, row := range rows {
			if _, err := fmt.Fprintln(os.Stdout, strings.Join(row, "\t")); err != nil {
				return err
			}
		}
		return nil
	}
	cell := lipgloss.NewStyle().Padding(0, 1)
	header := cell.Bold(true)
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Faint(true)).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return header
			}
			return cell
		})
	_, err := lipgloss.Println(t)
	return err
}
