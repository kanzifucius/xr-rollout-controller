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

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

const (
	outputTable = "table"
	outputJSON  = "json"
)

type rolloutRow struct {
	name        string
	composition string
	cluster     string
	phase       string
	revision    string
	pause       string
	canary      string
}

func writeRollouts(w io.Writer, format string, list *rolloutsv1alpha1.CompositionRolloutList) error {
	switch format {
	case "", outputTable:
		return writeRolloutTable(w, rowsFromRollouts(list.Items))
	case outputJSON:
		list.SetGroupVersionKind(rolloutsv1alpha1.GroupVersion.WithKind("CompositionRolloutList"))
		return writeJSON(w, list)
	default:
		return fmt.Errorf("output %q must be table or json", format)
	}
}

func writeRollout(w io.Writer, format string, obj *rolloutsv1alpha1.CompositionRollout) error {
	switch format {
	case "", outputTable:
		rows := rowsFromRollouts([]rolloutsv1alpha1.CompositionRollout{*obj})
		if err := writeRolloutTable(w, rows); err != nil {
			return err
		}
		for i := range rows {
			if rows[i].canary == "" {
				continue
			}
			if _, err := fmt.Fprintf(w, "Canary %s %s\n", rows[i].composition, rows[i].canary); err != nil {
				return err
			}
		}
		return nil
	case outputJSON:
		obj.SetGroupVersionKind(rolloutsv1alpha1.GroupVersion.WithKind("CompositionRollout"))
		return writeJSON(w, obj)
	default:
		return fmt.Errorf("output %q must be table or json", format)
	}
}

func writePipelines(w io.Writer, format string, list *rolloutsv1alpha1.PromotionPipelineList) error {
	switch format {
	case "", outputTable:
		return writePipelineTable(w, list.Items)
	case outputJSON:
		list.SetGroupVersionKind(rolloutsv1alpha1.GroupVersion.WithKind("PromotionPipelineList"))
		return writeJSON(w, list)
	default:
		return fmt.Errorf("output %q must be table or json", format)
	}
}

func writePipeline(w io.Writer, format string, obj *rolloutsv1alpha1.PromotionPipeline) error {
	switch format {
	case "", outputTable:
		return writePipelineTable(w, []rolloutsv1alpha1.PromotionPipeline{*obj})
	case outputJSON:
		obj.SetGroupVersionKind(rolloutsv1alpha1.GroupVersion.WithKind("PromotionPipeline"))
		return writeJSON(w, obj)
	default:
		return fmt.Errorf("output %q must be table or json", format)
	}
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}

func writeRolloutTable(w io.Writer, rows []rolloutRow) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "NAME\tCOMPOSITION\tCLUSTER\tPHASE\tREVISION\tPAUSE"); err != nil {
		return err
	}
	for i := range rows {
		row := &rows[i]
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", row.name, dash(row.composition), dash(row.cluster), dash(row.phase), dash(row.revision), dash(row.pause)); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write table: %w", err)
	}
	return nil
}

func writePipelineTable(w io.Writer, items []rolloutsv1alpha1.PromotionPipeline) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "PIPELINE\tFROM\tTO\tROLLOUT\tCOMPOSITION\tREVISION\tACTION\tREASON"); err != nil {
		return err
	}
	sorted := append([]rolloutsv1alpha1.PromotionPipeline(nil), items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for i := range sorted {
		item := &sorted[i]
		if len(item.Status.Hops) == 0 {
			if _, err := fmt.Fprintf(tw, "%s\t-\t-\t-\t-\t-\t-\t%s\n", item.Name, dash(clusterList(item))); err != nil {
				return err
			}
			continue
		}
		for j := range item.Status.Hops {
			hop := &item.Status.Hops[j]
			if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				item.Name, dash(hop.From), dash(hop.To), dash(hop.Rollout), dash(hop.Composition), dash(hop.Revision), dash(hop.Action), dash(hop.Reason),
			); err != nil {
				return err
			}
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write table: %w", err)
	}
	return nil
}

func rowsFromRollouts(items []rolloutsv1alpha1.CompositionRollout) []rolloutRow {
	rows := make([]rolloutRow, 0, len(items))
	for i := range items {
		item := &items[i]
		if len(item.Status.Rollouts) == 0 {
			rows = append(rows, rolloutRow{
				name:        item.Name,
				composition: onlyComposition(item),
				cluster:     item.Spec.Cluster,
				phase:       "-",
				revision:    "-",
				pause:       "-",
			})
			continue
		}
		for j := range item.Status.Rollouts {
			state := &item.Status.Rollouts[j]
			pause := ""
			if state.PauseReason != nil {
				pause = state.PauseReason.Reason
			}
			rows = append(rows, rolloutRow{
				name:        item.Name,
				composition: state.Composition,
				cluster:     item.Spec.Cluster,
				phase:       string(state.Phase),
				revision:    state.TargetRevision.Name,
				pause:       pause,
				canary:      canarySummary(&state.Canary),
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].name != rows[j].name {
			return rows[i].name < rows[j].name
		}
		return rows[i].composition < rows[j].composition
	})
	return rows
}

func canarySummary(canary *rolloutsv1alpha1.CanaryStatus) string {
	if canary == nil || (len(canary.Selected) == 0 && canary.Healthy == 0 && canary.Unhealthy == 0) {
		return ""
	}
	names := make([]string, 0, len(canary.Selected))
	for i := range canary.Selected {
		names = append(names, canary.Selected[i].String())
	}
	return fmt.Sprintf("%s healthy=%d unhealthy=%d", joinComma(names), canary.Healthy, canary.Unhealthy)
}

func onlyComposition(obj *rolloutsv1alpha1.CompositionRollout) string {
	if len(obj.Spec.Compositions) == 1 {
		return obj.Spec.Compositions[0].Name
	}
	return ""
}

func clusterList(obj *rolloutsv1alpha1.PromotionPipeline) string {
	names := make([]string, 0, len(obj.Spec.Clusters))
	for i := range obj.Spec.Clusters {
		names = append(names, obj.Spec.Clusters[i].Name)
	}
	return joinComma(names)
}

func joinComma(parts []string) string {
	out := ""
	for i := range parts {
		if i > 0 {
			out += ", "
		}
		out += parts[i]
	}
	return out
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
