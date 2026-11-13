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

package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

const (
	StateManaged   = "managed"
	StateUpdated   = "updated"
	StateHealthy   = "healthy"
	StateUnmanaged = "unmanaged"

	aggregateAll = "_all"
)

var (
	rolloutPhase = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xr_rollout_phase",
		Help: "Current rollout phase (1 for active phase label).",
	}, []string{"rollout", "composition", "phase"})

	rolloutXRs = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xr_rollout_xrs",
		Help: "XR counts by state for a composition rollout.",
	}, []string{"rollout", "composition", "state"})

	canaryUnhealthy = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xr_rollout_canary_unhealthy",
		Help: "Number of unhealthy canary XRs.",
	}, []string{"rollout", "composition"})

	updatesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "xr_rollout_updates_total",
		Help: "XR composition revision reference patches attempted.",
	}, []string{"rollout", "composition"})

	updateErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "xr_rollout_update_errors_total",
		Help: "XR composition revision reference patch failures.",
	}, []string{"rollout", "composition"})

	pausesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "xr_rollout_pauses_total",
		Help: "Rollout pause events by reason.",
	}, []string{"rollout", "composition", "reason"})

	promotionHops = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xr_rollout_promotion_hops",
		Help: "Cross-cluster promotion hop. 1 for the current action and reason.",
	}, []string{"pipeline", "from", "to", "composition", "action", "reason"})

	promotionWrites = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "xr_rollout_promotion_writes_total",
		Help: "Start triggers the promoter appended on the next cluster.",
	}, []string{"pipeline", "from", "to", "composition", "action", "reason"})

	promotionHopMu     sync.Mutex
	promotionHopSeries = map[string][]promotionSeries{}
)

// PromotionHop is one hop observation for the promotion gauge.
type PromotionHop struct {
	From        string
	To          string
	Composition string
	Action      string
	Reason      string
}

type promotionSeries struct {
	pipeline    string
	from        string
	to          string
	composition string
	action      string
	reason      string
}

func init() {
	crmetrics.Registry.MustRegister(
		rolloutPhase,
		rolloutXRs,
		canaryUnhealthy,
		updatesTotal,
		updateErrorsTotal,
		pausesTotal,
		promotionHops,
		promotionWrites,
	)
}

func labels(clusterName, rollout, composition string, aggregateOnly bool) (rolloutLabel, compositionLabel string) {
	if clusterName == "" {
		if aggregateOnly {
			return aggregateAll, aggregateAll
		}
		return rollout, composition
	}
	if aggregateOnly {
		return clusterName + "/" + aggregateAll, aggregateAll
	}
	return clusterName + "/" + rollout, composition
}

// SetPhase records the active phase gauge (exactly one phase label set to 1).
func SetPhase(clusterName, rollout, composition string, phase rolloutsv1alpha1.RolloutPhase, aggregateOnly bool) {
	r, c := labels(clusterName, rollout, composition, aggregateOnly)
	phases := []rolloutsv1alpha1.RolloutPhase{
		rolloutsv1alpha1.PhaseIdle,
		rolloutsv1alpha1.PhaseCanarying,
		rolloutsv1alpha1.PhaseSoaking,
		rolloutsv1alpha1.PhaseAwaitingApproval,
		rolloutsv1alpha1.PhasePromoting,
		rolloutsv1alpha1.PhasePaused,
		rolloutsv1alpha1.PhaseCompleted,
		rolloutsv1alpha1.PhaseAborted,
	}
	for _, p := range phases {
		v := 0.0
		if p == phase {
			v = 1
		}
		rolloutPhase.WithLabelValues(r, c, string(p)).Set(v)
	}
}

// SetXRCounts updates fleet accounting gauges.
func SetXRCounts(clusterName, rollout, composition string, managed, updated, healthy, unmanaged int32, aggregateOnly bool) {
	r, c := labels(clusterName, rollout, composition, aggregateOnly)
	rolloutXRs.WithLabelValues(r, c, StateManaged).Set(float64(managed))
	rolloutXRs.WithLabelValues(r, c, StateUpdated).Set(float64(updated))
	rolloutXRs.WithLabelValues(r, c, StateHealthy).Set(float64(healthy))
	rolloutXRs.WithLabelValues(r, c, StateUnmanaged).Set(float64(unmanaged))
}

// SetCanaryUnhealthy sets the canary unhealthy gauge.
func SetCanaryUnhealthy(clusterName, rollout, composition string, count int32, aggregateOnly bool) {
	r, c := labels(clusterName, rollout, composition, aggregateOnly)
	canaryUnhealthy.WithLabelValues(r, c).Set(float64(count))
}

// IncUpdates increments patch attempt counter.
func IncUpdates(clusterName, rollout, composition string, aggregateOnly bool) {
	r, c := labels(clusterName, rollout, composition, aggregateOnly)
	updatesTotal.WithLabelValues(r, c).Inc()
}

// IncUpdateErrors increments patch failure counter.
func IncUpdateErrors(clusterName, rollout, composition string, aggregateOnly bool) {
	r, c := labels(clusterName, rollout, composition, aggregateOnly)
	updateErrorsTotal.WithLabelValues(r, c).Inc()
}

// IncPause increments pause counter when a rollout enters Paused.
func IncPause(clusterName, rollout, composition, reason string, aggregateOnly bool) {
	r, c := labels(clusterName, rollout, composition, aggregateOnly)
	pausesTotal.WithLabelValues(r, c, reason).Inc()
}

// SetPromotionHops sets the current hop gauges and zeroes series from the
// previous reconcile of this pipeline that are no longer present.
// MetricsAggregateOnly keeps the composition label and sets it to _all.
func SetPromotionHops(pipeline string, hops []PromotionHop, aggregateOnly bool) {
	next := make([]promotionSeries, 0, len(hops))
	for _, hop := range hops {
		next = append(next, promotionSeries{
			pipeline:    pipeline,
			from:        hop.From,
			to:          hop.To,
			composition: compositionLabel(hop.Composition, aggregateOnly),
			action:      hop.Action,
			reason:      hop.Reason,
		})
	}

	promotionHopMu.Lock()
	defer promotionHopMu.Unlock()
	for _, old := range promotionHopSeries[pipeline] {
		if !seriesCurrent(next, old) {
			promotionHops.WithLabelValues(old.pipeline, old.from, old.to, old.composition, old.action, old.reason).Set(0)
		}
	}
	for _, cur := range next {
		promotionHops.WithLabelValues(cur.pipeline, cur.from, cur.to, cur.composition, cur.action, cur.reason).Set(1)
	}
	promotionHopSeries[pipeline] = next
}

// IncPromotionWrite counts one Start append that succeeded.
func IncPromotionWrite(pipeline, from, to, composition, action, reason string, aggregateOnly bool) {
	promotionWrites.WithLabelValues(
		pipeline,
		from,
		to,
		compositionLabel(composition, aggregateOnly),
		action,
		reason,
	).Inc()
}

func compositionLabel(composition string, aggregateOnly bool) string {
	if aggregateOnly {
		return aggregateAll
	}
	return composition
}

func seriesCurrent(series []promotionSeries, candidate promotionSeries) bool {
	for _, item := range series {
		if item == candidate {
			return true
		}
	}
	return false
}
