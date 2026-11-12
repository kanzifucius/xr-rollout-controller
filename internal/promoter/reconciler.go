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

package promoter

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/metrics"
)

const requeueInterval = 30 * time.Second

// PromoterReconciler opens the next cluster's Start gate. It does not
// reconcile composite resources.
type PromoterReconciler struct {
	Local    client.Client
	Provider interface {
		Get(ctx context.Context, clusterName string) (cluster.Cluster, error)
		Names() []string
	}
	Writer               TriggerWriter
	MetricsAggregateOnly bool
}

type hopSource struct {
	rollouts     []string
	compositions map[string][]string
}

type hopSourceKey struct{}

type writeNote struct {
	from        string
	to          string
	composition string
	revision    string
}

type writeNoteKey struct{}

// withHopSource supplies rollout and composition names without listing.
// Production leaves this unset and lists each engaged cluster.
func withHopSource(ctx context.Context, source hopSource) context.Context {
	return context.WithValue(ctx, hopSourceKey{}, source)
}

func hopSourceFrom(ctx context.Context) (hopSource, bool) {
	source, ok := ctx.Value(hopSourceKey{}).(hopSource)
	return source, ok
}

// Reconcile walks one PromotionPipeline and appends a Start when the
// previous cluster has completed that revision.
func (r *PromoterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	pipeline := &rolloutsv1alpha1.PromotionPipeline{}
	if err := r.Local.Get(ctx, req.NamespacedName, pipeline); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	notes := []writeNote{}
	ctx = context.WithValue(ctx, writeNoteKey{}, &notes)
	read := func(clusterName, rolloutName, composition, wantRevision string) (ClusterView, error) {
		return r.readView(ctx, clusterName, rolloutName, composition, wantRevision)
	}
	hops, err := reconcilePipeline(ctx, pipeline, read, r.Writer, func(clusterName string) (client.Client, error) {
		return r.clientFor(ctx, clusterName)
	})
	r.recordMetrics(pipeline.Name, hops, notes)
	if statusErr := r.writeStatus(ctx, pipeline, hops); statusErr != nil && err == nil {
		return ctrl.Result{}, statusErr
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueInterval}, nil
}

// SetupWithManager registers the promoter on the local manager.
func (r *PromoterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("promotionpipeline").
		For(&rolloutsv1alpha1.PromotionPipeline{}).
		Complete(r)
}

func (r *PromoterReconciler) readView(ctx context.Context, clusterName, rolloutName, composition, wantRevision string) (ClusterView, error) {
	cl, err := r.clientFor(ctx, clusterName)
	if err != nil {
		if errors.Is(err, multicluster.ErrClusterNotFound) {
			return ClusterView{Engaged: false}, nil
		}
		return ClusterView{}, err
	}
	return viewComposition(ctx, cl, rolloutName, composition, wantRevision)
}

func (r *PromoterReconciler) clientFor(ctx context.Context, clusterName string) (client.Client, error) {
	if r.Provider == nil {
		return nil, fmt.Errorf("cluster provider is not configured")
	}
	cl, err := r.Provider.Get(ctx, clusterName)
	if err != nil {
		return nil, err
	}
	return cl.GetClient(), nil
}

func (r *PromoterReconciler) recordMetrics(pipeline string, hops []rolloutsv1alpha1.PromotionHopStatus, notes []writeNote) {
	observed := make([]metrics.PromotionHop, 0, len(hops))
	for _, hop := range hops {
		observed = append(observed, metrics.PromotionHop{
			From:        hop.From,
			To:          hop.To,
			Composition: hop.Composition,
			Action:      hop.Action,
			Reason:      hop.Reason,
		})
	}
	metrics.SetPromotionHops(pipeline, observed, r.MetricsAggregateOnly)
	for _, note := range notes {
		metrics.IncPromotionWrite(pipeline, note.from, note.to, note.composition, ActionRecorded, ReasonAlreadyWritten, r.MetricsAggregateOnly)
	}
}

func (r *PromoterReconciler) writeStatus(ctx context.Context, pipeline *rolloutsv1alpha1.PromotionPipeline, hops []rolloutsv1alpha1.PromotionHopStatus) error {
	engaged := []string(nil)
	if r.Provider != nil {
		engaged = r.Provider.Names()
	}
	applyPipelineStatus(pipeline, hops, engaged)
	if err := r.Local.Status().Update(ctx, pipeline); err != nil {
		return fmt.Errorf("update PromotionPipeline status: %w", err)
	}
	return nil
}

func reconcilePipeline(
	ctx context.Context,
	pipeline *rolloutsv1alpha1.PromotionPipeline,
	read func(clusterName, rolloutName, composition, wantRevision string) (ClusterView, error),
	writer TriggerWriter,
	clFor func(clusterName string) (client.Client, error),
) ([]rolloutsv1alpha1.PromotionHopStatus, error) {
	names, compositions, err := catalogFor(ctx, pipeline, clFor)
	if err != nil {
		return nil, err
	}
	var hops []rolloutsv1alpha1.PromotionHopStatus
	var writeErr error
	for _, rolloutName := range names {
		for _, composition := range compositions[rolloutName] {
			for i := 0; i < len(pipeline.Spec.Clusters)-1; i++ {
				fromName := pipeline.Spec.Clusters[i].Name
				toName := pipeline.Spec.Clusters[i+1].Name
				hop, hopErr := reconcileHop(ctx, fromName, toName, rolloutName, composition, read, writer, clFor, &writeErr)
				if hopErr != nil {
					return hops, hopErr
				}
				hops = append(hops, hop)
			}
		}
	}
	return hops, writeErr
}

func reconcileHop(
	ctx context.Context,
	fromName, toName, rolloutName, composition string,
	read func(clusterName, rolloutName, composition, wantRevision string) (ClusterView, error),
	writer TriggerWriter,
	clFor func(clusterName string) (client.Client, error),
	writeErr *error,
) (rolloutsv1alpha1.PromotionHopStatus, error) {
	from, err := read(fromName, rolloutName, composition, "")
	if err != nil {
		return rolloutsv1alpha1.PromotionHopStatus{}, err
	}
	want := ""
	if rev, ok := completedRevision(from); ok {
		want = rev
	}
	to, err := read(toName, rolloutName, composition, want)
	if err != nil {
		return rolloutsv1alpha1.PromotionHopStatus{}, err
	}
	decision := DecideHop(from, to, composition)
	if decision.Action == ActionWriteStart && *writeErr == nil {
		if err := appendStart(ctx, clFor, writer, toName, rolloutName, fromName, decision); err != nil {
			*writeErr = err
			decision.Action = ActionWait
			decision.Reason = ReasonStartWriteFailed
			decision.Message = err.Error()
		} else {
			decision.Action = ActionRecorded
			decision.Reason = ReasonAlreadyWritten
		}
	}
	return rolloutsv1alpha1.PromotionHopStatus{
		Rollout:     rolloutName,
		Composition: composition,
		Revision:    decision.Revision,
		From:        fromName,
		To:          toName,
		Action:      decision.Action,
		Reason:      decision.Reason,
		Message:     decision.Message,
	}, nil
}

func appendStart(
	ctx context.Context,
	clFor func(clusterName string) (client.Client, error),
	writer TriggerWriter,
	toName, rolloutName, fromName string,
	decision Decision,
) error {
	cl, err := clFor(toName)
	if err != nil {
		return err
	}
	if err := writer.AppendStart(ctx, cl, rolloutName, decision.Trigger); err != nil {
		return err
	}
	noteWrite(ctx, fromName, toName, decision.Trigger.Composition, decision.Revision)
	return nil
}

func noteWrite(ctx context.Context, from, to, composition, revision string) {
	notes, _ := ctx.Value(writeNoteKey{}).(*[]writeNote)
	if notes == nil {
		return
	}
	*notes = append(*notes, writeNote{from: from, to: to, composition: composition, revision: revision})
}

func catalogFor(
	ctx context.Context,
	pipeline *rolloutsv1alpha1.PromotionPipeline,
	clFor func(clusterName string) (client.Client, error),
) ([]string, map[string][]string, error) {
	if source, ok := hopSourceFrom(ctx); ok {
		return append([]string(nil), source.rollouts...), source.compositions, nil
	}
	return listCatalog(ctx, pipeline, clFor)
}

func listCatalog(
	ctx context.Context,
	pipeline *rolloutsv1alpha1.PromotionPipeline,
	clFor func(clusterName string) (client.Client, error),
) ([]string, map[string][]string, error) {
	rollouts := map[string]struct{}{}
	compositions := map[string]map[string]struct{}{}
	for _, cluster := range pipeline.Spec.Clusters {
		cl, err := clFor(cluster.Name)
		if err != nil {
			if errors.Is(err, multicluster.ErrClusterNotFound) {
				continue
			}
			return nil, nil, err
		}
		if cl == nil {
			continue
		}
		if err := collectRollouts(ctx, cl, cluster.Name, rollouts, compositions); err != nil {
			return nil, nil, err
		}
	}
	return sortedSet(rollouts), compositionLists(compositions), nil
}

func collectRollouts(ctx context.Context, cl client.Client, clusterName string, rollouts map[string]struct{}, compositions map[string]map[string]struct{}) error {
	list := &rolloutsv1alpha1.CompositionRolloutList{}
	if err := cl.List(ctx, list); err != nil {
		return fmt.Errorf("list CompositionRollout on %s: %w", clusterName, err)
	}
	for i := range list.Items {
		item := &list.Items[i]
		rollouts[item.Name] = struct{}{}
		names := compositions[item.Name]
		if names == nil {
			names = map[string]struct{}{}
			compositions[item.Name] = names
		}
		for _, composition := range item.Spec.Compositions {
			names[composition.Name] = struct{}{}
		}
	}
	return nil
}

func compositionLists(compositions map[string]map[string]struct{}) map[string][]string {
	out := make(map[string][]string, len(compositions))
	for rollout, names := range compositions {
		out[rollout] = sortedSet(names)
	}
	return out
}

func sortedSet(values map[string]struct{}) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func applyPipelineStatus(pipeline *rolloutsv1alpha1.PromotionPipeline, hops []rolloutsv1alpha1.PromotionHopStatus, engaged []string) {
	pipeline.Status.ObservedGeneration = pipeline.Generation
	pipeline.Status.Hops = hops
	ready, readyReason, readyMessage := readyCondition(pipeline, engaged)
	blocked, blockedReason, blockedMessage := blockedCondition(hops)
	progressing, progressingReason, progressingMessage := progressingCondition(hops)
	setPipelineCondition(pipeline, rolloutsv1alpha1.PipelineReady, ready, readyReason, readyMessage)
	setPipelineCondition(pipeline, rolloutsv1alpha1.PipelineBlocked, blocked, blockedReason, blockedMessage)
	setPipelineCondition(pipeline, rolloutsv1alpha1.PipelineProgressing, progressing, progressingReason, progressingMessage)
}

func readyCondition(pipeline *rolloutsv1alpha1.PromotionPipeline, engaged []string) (metav1.ConditionStatus, string, string) {
	have := map[string]struct{}{}
	for _, name := range engaged {
		have[name] = struct{}{}
	}
	var missing []string
	for _, cluster := range pipeline.Spec.Clusters {
		if _, ok := have[cluster.Name]; !ok {
			missing = append(missing, cluster.Name)
		}
	}
	if len(missing) == 0 {
		return metav1.ConditionTrue, "ClustersEngaged", "Every pipeline cluster is engaged"
	}
	return metav1.ConditionFalse, "ClustersNotEngaged", "Not engaged: " + strings.Join(missing, ", ")
}

func blockedCondition(hops []rolloutsv1alpha1.PromotionHopStatus) (metav1.ConditionStatus, string, string) {
	for _, hop := range hops {
		if hop.Action == ActionBlocked {
			return metav1.ConditionTrue, "HopBlocked", hop.From + " to " + hop.To + " is blocked: " + hop.Reason
		}
	}
	return metav1.ConditionFalse, "NoBlockedHop", "No hop is blocked"
}

func progressingCondition(hops []rolloutsv1alpha1.PromotionHopStatus) (metav1.ConditionStatus, string, string) {
	for _, hop := range hops {
		if hop.Action == ActionWait || (hop.Action == ActionRecorded && hop.Reason == ReasonAlreadyWritten) {
			return metav1.ConditionTrue, "PromotionPending", "A hop is waiting for the next cluster to accept the revision"
		}
	}
	return metav1.ConditionFalse, "PromotionIdle", "No hop is waiting to open"
}

func setPipelineCondition(pipeline *rolloutsv1alpha1.PromotionPipeline, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&pipeline.Status.Conditions, metav1.Condition{
		Type:    condType,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}
