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
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/cli/tui"
)

// PollInterval is how often list, get, pipeline status, and the terminal UI refresh.
const PollInterval = tui.PollInterval

// Options configures the CLI. A set Client skips kubeconfig loading.
type Options struct {
	// Client is the caller's cluster. Tests set this. The CLI does not load spoke kubeconfigs.
	Client client.Client
}

// NewRoot builds the xr-rollout command. With no subcommand it opens the terminal UI.
func NewRoot(o Options) *cobra.Command {
	var kubeconfig, kubeContext, output string
	root := &cobra.Command{
		Use:           "xr-rollout",
		Short:         "Read rollouts and append triggers",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `xr-rollout reads CompositionRollout and PromotionPipeline objects in the current kube context.

With no subcommand it opens the terminal UI. Subcommands do not prompt.

A trigger appends one revision-bound entry and leaves every other spec.triggers entry in place. The CLI does not patch composite resources.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, name, err := o.cluster(kubeconfig, kubeContext)
			if err != nil {
				return err
			}
			return tui.Run(cmd.Context(), triggerWriter{cl}, func(ctx context.Context) (tui.Snapshot, error) {
				return loadSnapshot(ctx, cl)
			}, name)
		},
	}
	root.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Path to the kubeconfig. Defaults to the current kubeconfig")
	root.PersistentFlags().StringVar(&kubeContext, "context", "", "Kube context. Defaults to the current context")
	root.PersistentFlags().StringVarP(&output, "output", "o", outputTable, "Output format: table or json")

	cluster := func() (client.Client, error) {
		cl, _, err := o.cluster(kubeconfig, kubeContext)
		return cl, err
	}

	root.AddCommand(
		newListCommand(cluster, &output),
		newGetCommand(cluster, &output),
		newPipelineCommand(cluster, &output),
		newRolloutTriggerCommand(cluster, &output, "start", rolloutsv1alpha1.TriggerStart, "Append a Start trigger"),
		newRolloutTriggerCommand(cluster, &output, "approve", rolloutsv1alpha1.TriggerApprove, "Append an Approve trigger"),
		newRolloutTriggerCommand(cluster, &output, "resume", rolloutsv1alpha1.TriggerResume, "Append a Resume trigger"),
		newRolloutTriggerCommand(cluster, &output, "abort", rolloutsv1alpha1.TriggerAbort, "Append an Abort trigger"),
		newSkipCommand(cluster, &output),
	)
	return root
}

func (o Options) cluster(kubeconfig, kubeContext string) (client.Client, string, error) {
	if o.Client != nil {
		if kubeContext == "" {
			kubeContext = "current"
		}
		return o.Client, kubeContext, nil
	}
	return newKubeClient(kubeconfig, kubeContext)
}

func newListCommand(cluster func() (client.Client, error), output *string) *cobra.Command {
	var watch bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List CompositionRollout objects on this context",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWatch(cmd, watch, func() error {
				cl, err := cluster()
				if err != nil {
					return err
				}
				var list rolloutsv1alpha1.CompositionRolloutList
				if err := cl.List(cmd.Context(), &list); err != nil {
					return fmt.Errorf("list CompositionRollout: %w", err)
				}
				return writeRollouts(cmd.OutOrStdout(), *output, &list)
			})
		},
	}
	cmd.Flags().BoolVar(&watch, "watch", false, "Poll every 30 seconds")
	return cmd
}

func newGetCommand(cluster func() (client.Client, error), output *string) *cobra.Command {
	var watch bool
	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Get one CompositionRollout",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWatch(cmd, watch, func() error {
				cl, err := cluster()
				if err != nil {
					return err
				}
				obj := &rolloutsv1alpha1.CompositionRollout{}
				if err := cl.Get(cmd.Context(), client.ObjectKey{Name: args[0]}, obj); err != nil {
					return fmt.Errorf("get CompositionRollout %s: %w", args[0], err)
				}
				return writeRollout(cmd.OutOrStdout(), *output, obj)
			})
		},
	}
	cmd.Flags().BoolVar(&watch, "watch", false, "Poll every 30 seconds")
	return cmd
}

func newPipelineCommand(cluster func() (client.Client, error), output *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline [name]",
		Short: "Show PromotionPipeline status",
		Args:  cobra.MaximumNArgs(1),
		RunE:  pipelineRun(cluster, output),
	}
	cmd.Flags().Bool("watch", false, "Poll every 30 seconds")
	status := &cobra.Command{
		Use:   "status [name]",
		Short: "Show PromotionPipeline status",
		Args:  cobra.MaximumNArgs(1),
		RunE:  pipelineRun(cluster, output),
	}
	status.Flags().Bool("watch", false, "Poll every 30 seconds")
	cmd.AddCommand(status)
	return cmd
}

func pipelineRun(cluster func() (client.Client, error), output *string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		watch, err := cmd.Flags().GetBool("watch")
		if err != nil {
			return err
		}
		return runWatch(cmd, watch, func() error {
			cl, err := cluster()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				obj := &rolloutsv1alpha1.PromotionPipeline{}
				if err := cl.Get(cmd.Context(), client.ObjectKey{Name: args[0]}, obj); err != nil {
					return fmt.Errorf("get PromotionPipeline %s: %w", args[0], err)
				}
				return writePipeline(cmd.OutOrStdout(), *output, obj)
			}
			var list rolloutsv1alpha1.PromotionPipelineList
			if err := cl.List(cmd.Context(), &list); err != nil {
				return fmt.Errorf("list PromotionPipeline: %w", err)
			}
			return writePipelines(cmd.OutOrStdout(), *output, &list)
		})
	}
}

func newRolloutTriggerCommand(cluster func() (client.Client, error), output *string, use string, action rolloutsv1alpha1.TriggerAction, short string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use + " NAME",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			trigger, err := rolloutTriggerFromFlags(cmd, action)
			if err != nil {
				return err
			}
			cl, err := cluster()
			if err != nil {
				return err
			}
			if err := AppendRolloutTrigger(cmd.Context(), cl, args[0], trigger); err != nil {
				return err
			}
			return writeRolloutTrigger(cmd.OutOrStdout(), *output, args[0], trigger)
		},
	}
	cmd.Flags().String("composition", "", "Composition the trigger applies to")
	cmd.Flags().String("revision", "", "CompositionRevision name")
	cmd.Flags().String("actor", "", "Free-text actor recorded on the trigger")
	mustMarkRequired(cmd, "composition")
	mustMarkRequired(cmd, "revision")
	return cmd
}

func newSkipCommand(cluster func() (client.Client, error), output *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skip NAME",
		Short: "Append a Skip trigger on a PromotionPipeline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			trigger, err := pipelineTriggerFromFlags(cmd)
			if err != nil {
				return err
			}
			cl, err := cluster()
			if err != nil {
				return err
			}
			if err := AppendPipelineTrigger(cmd.Context(), cl, args[0], trigger); err != nil {
				return err
			}
			return writePipelineTrigger(cmd.OutOrStdout(), *output, args[0], trigger)
		},
	}
	cmd.Flags().String("cluster", "", "Pipeline cluster this Skip applies to")
	cmd.Flags().String("revision", "", "CompositionRevision name")
	cmd.Flags().String("rollout", "", "CompositionRollout name. Empty matches every rollout on the cluster")
	cmd.Flags().String("composition", "", "Composition name. Empty matches every composition")
	cmd.Flags().String("actor", "", "Free-text actor recorded on the trigger")
	mustMarkRequired(cmd, "cluster")
	mustMarkRequired(cmd, "revision")
	return cmd
}

func rolloutTriggerFromFlags(cmd *cobra.Command, action rolloutsv1alpha1.TriggerAction) (rolloutsv1alpha1.RolloutTrigger, error) {
	composition, err := cmd.Flags().GetString("composition")
	if err != nil {
		return rolloutsv1alpha1.RolloutTrigger{}, err
	}
	revision, err := cmd.Flags().GetString("revision")
	if err != nil {
		return rolloutsv1alpha1.RolloutTrigger{}, err
	}
	actor, err := cmd.Flags().GetString("actor")
	if err != nil {
		return rolloutsv1alpha1.RolloutTrigger{}, err
	}
	if revision == "" {
		return rolloutsv1alpha1.RolloutTrigger{}, fmt.Errorf("revision is required")
	}
	return rolloutsv1alpha1.RolloutTrigger{
		Composition: composition,
		Action:      action,
		Revision:    revision,
		Actor:       actor,
	}, nil
}

func pipelineTriggerFromFlags(cmd *cobra.Command) (rolloutsv1alpha1.PromotionTrigger, error) {
	clusterName, err := cmd.Flags().GetString("cluster")
	if err != nil {
		return rolloutsv1alpha1.PromotionTrigger{}, err
	}
	revision, err := cmd.Flags().GetString("revision")
	if err != nil {
		return rolloutsv1alpha1.PromotionTrigger{}, err
	}
	rolloutName, err := cmd.Flags().GetString("rollout")
	if err != nil {
		return rolloutsv1alpha1.PromotionTrigger{}, err
	}
	composition, err := cmd.Flags().GetString("composition")
	if err != nil {
		return rolloutsv1alpha1.PromotionTrigger{}, err
	}
	actor, err := cmd.Flags().GetString("actor")
	if err != nil {
		return rolloutsv1alpha1.PromotionTrigger{}, err
	}
	if revision == "" {
		return rolloutsv1alpha1.PromotionTrigger{}, fmt.Errorf("revision is required")
	}
	return rolloutsv1alpha1.PromotionTrigger{
		Cluster:     clusterName,
		Action:      rolloutsv1alpha1.PromotionTriggerSkip,
		Revision:    revision,
		Rollout:     rolloutName,
		Composition: composition,
		Actor:       actor,
	}, nil
}

func writeRolloutTrigger(w interface{ Write([]byte) (int, error) }, format, name string, trigger rolloutsv1alpha1.RolloutTrigger) error {
	switch format {
	case "", outputTable:
		_, err := fmt.Fprintf(w, "Appended %s on %s composition %s revision %s\n", trigger.Action, name, trigger.Composition, trigger.Revision)
		return err
	case outputJSON:
		return writeJSON(w, trigger)
	default:
		return fmt.Errorf("output %q must be table or json", format)
	}
}

func writePipelineTrigger(w interface{ Write([]byte) (int, error) }, format, name string, trigger rolloutsv1alpha1.PromotionTrigger) error {
	switch format {
	case "", outputTable:
		_, err := fmt.Fprintf(w, "Appended %s on %s cluster %s revision %s\n", trigger.Action, name, trigger.Cluster, trigger.Revision)
		return err
	case outputJSON:
		return writeJSON(w, trigger)
	default:
		return fmt.Errorf("output %q must be table or json", format)
	}
}

func runWatch(cmd *cobra.Command, watch bool, render func() error) error {
	if err := render(); err != nil {
		return err
	}
	if !watch {
		return nil
	}
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-cmd.Context().Done():
			return nil
		case <-ticker.C:
			if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
				return err
			}
			if err := render(); err != nil {
				return err
			}
		}
	}
}

func mustMarkRequired(cmd *cobra.Command, name string) {
	if err := cmd.MarkFlagRequired(name); err != nil {
		panic(err)
	}
}
