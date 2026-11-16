/*
Copyright 2026 Ricardo Pinto.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package tui is the xr-rollout terminal UI.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

// PollInterval is how often the view reloads rollouts.
const PollInterval = 30 * time.Second

// Writer appends one trigger. Implementations must not patch composite resources.
type Writer interface {
	AppendRollout(ctx context.Context, name string, trigger rolloutsv1alpha1.RolloutTrigger) error
	AppendPipeline(ctx context.Context, name string, trigger rolloutsv1alpha1.PromotionTrigger) error
}

// Loader reads the current context.
type Loader func(context.Context) (Snapshot, error)

// Row is one composition on a CompositionRollout.
type Row struct {
	Rollout         string
	Composition     string
	Cluster         string
	Phase           string
	Revision        string
	PauseReason     string
	PauseMessage    string
	Canary          []string
	CanaryHealthy   int32
	CanaryUnhealthy int32
}

// Pipeline is a PromotionPipeline the Skip prompt can target.
type Pipeline struct {
	Name     string
	Clusters []string
}

// Snapshot is the rollouts and pipelines on this context.
type Snapshot struct {
	Rows      []Row
	Pipelines []Pipeline
}

type screen int

const (
	screenList screen = iota
	screenDetail
)

type pending struct {
	action     string
	pipeline   string
	pipelines  []string
	pipelineAt int
}

type model struct {
	writer      Writer
	loader      Loader
	ctx         context.Context
	contextName string
	snap        Snapshot
	cursor      int
	screen      screen
	confirm     *pending
	status      string
}

type (
	tickMsg   struct{}
	loadedMsg struct {
		snap Snapshot
		err  error
	}
	wroteMsg struct {
		action string
		err    error
	}
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("63")).Padding(0, 1)
	selected    = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("86"))
	muted       = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	confirmBox  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).BorderForeground(lipgloss.Color("220"))
)

// Run opens the terminal UI. It polls until the context is cancelled or the user quits.
func Run(ctx context.Context, w Writer, load Loader, kubeContext string) error {
	program := tea.NewProgram(newModel(ctx, w, load, kubeContext), tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := program.Run()
	if err != nil {
		return fmt.Errorf("terminal UI: %w", err)
	}
	return nil
}

func newModel(ctx context.Context, w Writer, load Loader, kubeContext string) model {
	if kubeContext == "" {
		kubeContext = "current"
	}
	return model{
		writer:      w,
		loader:      load,
		ctx:         ctx,
		contextName: kubeContext,
		screen:      screenList,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.loadCmd(), m.tick())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.onKey(msg)
	case tickMsg:
		return m, tea.Batch(m.loadCmd(), m.tick())
	case loadedMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		m.snap = msg.snap
		m.clamp()
		return m, nil
	case wroteMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		m.status = "Appended " + msg.action
		return m, m.loadCmd()
	default:
		return m, nil
	}
}

func (m model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.onEsc(), nil
	case tea.KeyEnter:
		return m.onEnter(), nil
	case tea.KeyUp:
		return m.move(-1), nil
	case tea.KeyDown:
		return m.move(1), nil
	case tea.KeyLeft:
		return m.cyclePipeline(-1), nil
	case tea.KeyRight:
		return m.cyclePipeline(1), nil
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			return m.onRune(msg.Runes[0])
		}
		return m, nil
	default:
		return m, nil
	}
}

func (m model) onRune(r rune) (tea.Model, tea.Cmd) {
	if m.confirm != nil {
		switch r {
		case 'y', 'Y':
			item := *m.confirm
			m.confirm = nil
			return m, m.write(item)
		case 'n', 'N', 'q':
			if r == 'q' {
				return m, tea.Quit
			}
			m.confirm = nil
			return m, nil
		default:
			return m, nil
		}
	}
	if m.screen == screenList {
		switch r {
		case 'q':
			return m, tea.Quit
		case 'j':
			return m.move(1), nil
		case 'k':
			return m.move(-1), nil
		default:
			return m, nil
		}
	}
	switch r {
	case 'q':
		return m, tea.Quit
	case 's':
		return m.arm(string(rolloutsv1alpha1.TriggerStart)), nil
	case 'a':
		return m.arm(string(rolloutsv1alpha1.TriggerApprove)), nil
	case 'r':
		return m.arm(string(rolloutsv1alpha1.TriggerResume)), nil
	case 'x':
		return m.arm(string(rolloutsv1alpha1.TriggerAbort)), nil
	case 'k':
		return m.armSkip(), nil
	default:
		return m, nil
	}
}

func (m model) onEsc() model {
	if m.confirm != nil {
		m.confirm = nil
		return m
	}
	if m.screen == screenDetail {
		m.screen = screenList
	}
	return m
}

func (m model) onEnter() model {
	if m.screen == screenList && len(m.snap.Rows) > 0 {
		m.screen = screenDetail
	}
	return m
}

func (m model) arm(action string) model {
	row, ok := m.selected()
	if !ok {
		m.status = "no rollout selected"
		return m
	}
	if row.Composition == "" || row.Revision == "" {
		m.status = "revision is required"
		return m
	}
	m.confirm = &pending{action: action}
	return m
}

func (m model) armSkip() model {
	row, ok := m.selected()
	if !ok {
		m.status = "no rollout selected"
		return m
	}
	if row.Cluster == "" || row.Revision == "" {
		m.status = "cluster and revision are required"
		return m
	}
	names := pipelinesFor(m.snap.Pipelines, row.Cluster)
	if len(names) == 0 {
		m.status = "no PromotionPipeline on this context"
		return m
	}
	m.confirm = &pending{action: string(rolloutsv1alpha1.PromotionTriggerSkip), pipeline: names[0], pipelines: names}
	return m
}

func (m model) write(item pending) tea.Cmd {
	row, ok := m.selected()
	if !ok {
		return nil
	}
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	writer := m.writer
	return func() tea.Msg {
		var err error
		switch item.action {
		case string(rolloutsv1alpha1.TriggerStart), string(rolloutsv1alpha1.TriggerApprove), string(rolloutsv1alpha1.TriggerResume), string(rolloutsv1alpha1.TriggerAbort):
			err = writer.AppendRollout(ctx, row.Rollout, rolloutsv1alpha1.RolloutTrigger{
				Composition: row.Composition,
				Action:      rolloutsv1alpha1.TriggerAction(item.action),
				Revision:    row.Revision,
			})
		case string(rolloutsv1alpha1.PromotionTriggerSkip):
			err = writer.AppendPipeline(ctx, item.pipeline, rolloutsv1alpha1.PromotionTrigger{
				Cluster:     row.Cluster,
				Action:      rolloutsv1alpha1.PromotionTriggerSkip,
				Revision:    row.Revision,
				Rollout:     row.Rollout,
				Composition: row.Composition,
			})
		default:
			err = fmt.Errorf("unknown action %s", item.action)
		}
		return wroteMsg{action: item.action, err: err}
	}
}

func (m model) move(delta int) model {
	if m.screen != screenList || m.confirm != nil {
		return m
	}
	m.cursor += delta
	m.clamp()
	return m
}

func (m model) cyclePipeline(delta int) model {
	if m.confirm == nil || len(m.confirm.pipelines) < 2 {
		return m
	}
	count := len(m.confirm.pipelines)
	m.confirm.pipelineAt = (m.confirm.pipelineAt + delta + count) % count
	m.confirm.pipeline = m.confirm.pipelines[m.confirm.pipelineAt]
	return m
}

func (m *model) clamp() {
	if len(m.snap.Rows) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.snap.Rows) {
		m.cursor = len(m.snap.Rows) - 1
	}
}

func (m model) selected() (Row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.snap.Rows) {
		return Row{}, false
	}
	return m.snap.Rows[m.cursor], true
}

func (m model) tick() tea.Cmd {
	return tea.Tick(PollInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m model) loadCmd() tea.Cmd {
	if m.loader == nil {
		return nil
	}
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	load := m.loader
	return func() tea.Msg {
		snap, err := load(ctx)
		return loadedMsg{snap: snap, err: err}
	}
}

func (m model) View() string {
	var b strings.Builder
	fmt.Fprintln(&b, headerStyle.Render("xr-rollout  "+m.contextName))
	fmt.Fprintln(&b)
	if m.screen == screenDetail {
		b.WriteString(m.detailView())
	} else {
		b.WriteString(m.listView())
	}
	if m.confirm != nil {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, confirmBox.Render(m.confirmView()))
	}
	if m.status != "" {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, muted.Render(m.status))
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, muted.Render(m.help()))
	return b.String()
}

func (m model) listView() string {
	var b strings.Builder
	fmt.Fprintln(&b, muted.Render(fmt.Sprintf("%-18s %-28s %-10s %-16s %-32s %s", "NAME", "COMPOSITION", "CLUSTER", "PHASE", "REVISION", "PAUSE")))
	if len(m.snap.Rows) == 0 {
		fmt.Fprintln(&b, "No CompositionRollout objects on this context.")
		return b.String()
	}
	for i := range m.snap.Rows {
		row := &m.snap.Rows[i]
		line := fmt.Sprintf("%-18s %-28s %-10s %-16s %-32s %s", row.Rollout, dash(row.Composition), dash(row.Cluster), dash(row.Phase), dash(row.Revision), dash(row.PauseReason))
		if i == m.cursor {
			line = selected.Render(line)
		}
		fmt.Fprintln(&b, line)
	}
	return b.String()
}

func (m model) detailView() string {
	row, ok := m.selected()
	if !ok {
		return "No rollout selected.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s / %s\n\n", row.Rollout, dash(row.Composition))
	writeField(&b, "Phase", dash(row.Phase))
	writeField(&b, "Pause reason", dash(row.PauseReason))
	if row.PauseMessage != "" {
		fmt.Fprintf(&b, "%s%s\n", strings.Repeat(" ", 16), row.PauseMessage)
	}
	writeField(&b, "Cluster", dash(row.Cluster))
	writeField(&b, "Revision", dash(row.Revision))
	canary := dash(strings.Join(row.Canary, ", "))
	writeField(&b, "Canary", canary)
	fmt.Fprintf(&b, "%shealthy %d  unhealthy %d\n", strings.Repeat(" ", 16), row.CanaryHealthy, row.CanaryUnhealthy)
	return b.String()
}

func (m model) confirmView() string {
	row, ok := m.selected()
	if !ok {
		return "Nothing selected."
	}
	if m.confirm.action == string(rolloutsv1alpha1.PromotionTriggerSkip) {
		return fmt.Sprintf("Append Skip on %s?\ncluster %s\nrevision %s\nrollout %s\n\ny writes this trigger. n cancels.",
			m.confirm.pipeline, row.Cluster, row.Revision, row.Rollout)
	}
	return fmt.Sprintf("Append %s on %s?\ncomposition %s\nrevision %s\n\ny writes this trigger. n cancels.",
		m.confirm.action, row.Rollout, row.Composition, row.Revision)
}

func (m model) help() string {
	if m.confirm != nil {
		extra := ""
		if len(m.confirm.pipelines) > 1 {
			extra = "  left/right pipeline"
		}
		return "y write   n cancel   esc cancel" + extra
	}
	if m.screen == screenDetail {
		return "s start   a approve   r resume   x abort   k skip   esc back   q quit"
	}
	return "j/k move   enter detail   q quit   refreshes every 30s"
}

func writeField(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "%-14s  %s\n", label, value)
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func pipelinesFor(pipelines []Pipeline, cluster string) []string {
	matched := make([]string, 0, len(pipelines))
	all := make([]string, 0, len(pipelines))
	for i := range pipelines {
		all = append(all, pipelines[i].Name)
		for _, name := range pipelines[i].Clusters {
			if name == cluster {
				matched = append(matched, pipelines[i].Name)
				break
			}
		}
	}
	if len(matched) > 0 {
		return matched
	}
	return all
}
