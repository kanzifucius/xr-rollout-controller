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

package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

func TestConfirmDoesNotWriteUntilYes(t *testing.T) {
	t.Parallel()
	writer := &recordWriter{}
	m := detailModel(writer)

	view := m.View()
	for _, want := range []string{"Paused", "CanaryUnhealthy", "payments/db-1", "platformdb.example.org-aaa", "prod"} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail missing %q:\n%s", want, view)
		}
	}

	next, cmd := m.Update(key('x'))
	m = next.(model)
	if cmd != nil {
		t.Fatalf("abort key returned a command before confirmation")
	}
	if len(writer.rollouts) != 0 || len(writer.pipelines) != 0 {
		t.Fatalf("writes before confirmation: rollouts=%d pipelines=%d", len(writer.rollouts), len(writer.pipelines))
	}
	if m.confirm == nil || m.confirm.action != string(rolloutsv1alpha1.TriggerAbort) {
		t.Fatalf("confirm = %#v, want Abort", m.confirm)
	}
	if !strings.Contains(m.View(), "Abort") {
		t.Fatalf("confirm view does not name Abort:\n%s", m.View())
	}

	next, cmd = m.Update(key('n'))
	m = next.(model)
	if cmd != nil || m.confirm != nil || len(writer.rollouts) != 0 {
		t.Fatalf("cancel wrote or left the prompt: cmd=%v confirm=%#v writes=%d", cmd, m.confirm, len(writer.rollouts))
	}

	next, cmd = m.Update(key('x'))
	m = next.(model)
	if cmd != nil || len(writer.rollouts) != 0 {
		t.Fatalf("second abort wrote before confirmation: cmd=%v writes=%d", cmd, len(writer.rollouts))
	}
	next, cmd = m.Update(key('y'))
	m = next.(model)
	if m.confirm != nil || len(writer.rollouts) != 0 {
		t.Fatalf("confirm wrote before the command ran: confirm=%#v writes=%#v", m.confirm, writer.rollouts)
	}
	if cmd == nil {
		t.Fatal("confirm did not return a write command")
	}
	_ = cmd()
	if len(writer.rollouts) != 1 {
		t.Fatalf("writes = %#v, want one Abort", writer.rollouts)
	}
	got := writer.rollouts[0]
	if writer.rolloutNames[0] != "team-payments" || got.Action != rolloutsv1alpha1.TriggerAbort || got.Revision != "platformdb.example.org-aaa" || got.Composition != "platformdb.example.org" {
		t.Fatalf("write = %s %#v", writer.rolloutNames[0], got)
	}
}

func TestSkipConfirmDoesNotWriteUntilYes(t *testing.T) {
	t.Parallel()
	writer := &recordWriter{}
	m := detailModel(writer)
	next, cmd := m.Update(key('k'))
	m = next.(model)
	if cmd != nil || len(writer.pipelines) != 0 {
		t.Fatalf("skip wrote before confirmation: cmd=%v writes=%d", cmd, len(writer.pipelines))
	}
	if m.confirm == nil || m.confirm.action != string(rolloutsv1alpha1.PromotionTriggerSkip) || m.confirm.pipeline != "environments" {
		t.Fatalf("confirm = %#v, want Skip on environments", m.confirm)
	}
	next, cmd = m.Update(key('y'))
	m = next.(model)
	if m.confirm != nil || len(writer.pipelines) != 0 {
		t.Fatalf("skip wrote before the command ran: confirm=%#v writes=%d", m.confirm, len(writer.pipelines))
	}
	if cmd == nil {
		t.Fatal("skip confirm did not return a write command")
	}
	_ = cmd()
	if len(writer.pipelines) != 1 {
		t.Fatalf("pipeline writes = %#v", writer.pipelines)
	}
	got := writer.pipelines[0]
	if writer.pipelineNames[0] != "environments" || got.Action != rolloutsv1alpha1.PromotionTriggerSkip || got.Cluster != "prod" || got.Revision != "platformdb.example.org-aaa" {
		t.Fatalf("skip = %s %#v", writer.pipelineNames[0], got)
	}
}

func TestPollInterval(t *testing.T) {
	t.Parallel()
	if PollInterval != 30*time.Second {
		t.Fatalf("PollInterval = %s, want 30s", PollInterval)
	}
}

func TestListIsTheFirstScreen(t *testing.T) {
	t.Parallel()
	m := detailModel(&recordWriter{})
	m.screen = screenList
	view := m.View()
	for _, want := range []string{"team-payments", "Paused", "prod"} {
		if !strings.Contains(view, want) {
			t.Fatalf("list missing %q:\n%s", want, view)
		}
	}
}

func detailModel(writer Writer) model {
	return model{
		writer:      writer,
		contextName: "dev",
		screen:      screenDetail,
		snap: Snapshot{
			Rows: []Row{{
				Rollout:         "team-payments",
				Composition:     "platformdb.example.org",
				Cluster:         "prod",
				Phase:           "Paused",
				Revision:        "platformdb.example.org-aaa",
				PauseReason:     "CanaryUnhealthy",
				PauseMessage:    "not Ready",
				Canary:          []string{"payments/db-1"},
				CanaryUnhealthy: 1,
			}},
			Pipelines: []Pipeline{{
				Name:     "environments",
				Clusters: []string{"dev", "prod"},
			}},
		},
	}
}

func key(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

type recordWriter struct {
	rollouts      []rolloutsv1alpha1.RolloutTrigger
	rolloutNames  []string
	pipelines     []rolloutsv1alpha1.PromotionTrigger
	pipelineNames []string
}

func (w *recordWriter) AppendRollout(_ context.Context, name string, trigger rolloutsv1alpha1.RolloutTrigger) error {
	w.rolloutNames = append(w.rolloutNames, name)
	w.rollouts = append(w.rollouts, trigger)
	return nil
}

func (w *recordWriter) AppendPipeline(_ context.Context, name string, trigger rolloutsv1alpha1.PromotionTrigger) error {
	w.pipelineNames = append(w.pipelineNames, name)
	w.pipelines = append(w.pipelines, trigger)
	return nil
}
