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
	"github.com/prometheus/client_golang/prometheus"
	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	StateManaged   = "managed"
	StateUpdated   = "updated"
	StateHealthy   = "healthy"
	StateUnmanaged = "unmanaged"
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
)

func init() {
	crmetrics.Registry.MustRegister(
		rolloutPhase,
		rolloutXRs,
		canaryUnhealthy,
		updatesTotal,
		updateErrorsTotal,
		pausesTotal,
	)
}

func labels(rollout, composition string, aggregateOnly bool) (rolloutLabel, compositionLabel string) {
	if aggregateOnly {
		return "_all", "_all"
	}
	return rollout, composition
}

// SetPhase records the active phase gauge (exactly one phase label set to 1).
func SetPhase(rollout, composition string, phase rolloutsv1alpha1.RolloutPhase, aggregateOnly bool) {
	r, c := labels(rollout, composition, aggregateOnly)
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
func SetXRCounts(rollout, composition string, managed, updated, healthy, unmanaged int32, aggregateOnly bool) {
	r, c := labels(rollout, composition, aggregateOnly)
	rolloutXRs.WithLabelValues(r, c, StateManaged).Set(float64(managed))
	rolloutXRs.WithLabelValues(r, c, StateUpdated).Set(float64(updated))
	rolloutXRs.WithLabelValues(r, c, StateHealthy).Set(float64(healthy))
	rolloutXRs.WithLabelValues(r, c, StateUnmanaged).Set(float64(unmanaged))
}

// SetCanaryUnhealthy sets the canary unhealthy gauge.
func SetCanaryUnhealthy(rollout, composition string, count int32, aggregateOnly bool) {
	r, c := labels(rollout, composition, aggregateOnly)
	canaryUnhealthy.WithLabelValues(r, c).Set(float64(count))
}

// IncUpdates increments patch attempt counter.
func IncUpdates(rollout, composition string, aggregateOnly bool) {
	r, c := labels(rollout, composition, aggregateOnly)
	updatesTotal.WithLabelValues(r, c).Inc()
}

// IncUpdateErrors increments patch failure counter.
func IncUpdateErrors(rollout, composition string, aggregateOnly bool) {
	r, c := labels(rollout, composition, aggregateOnly)
	updateErrorsTotal.WithLabelValues(r, c).Inc()
}

// IncPause increments pause counter when a rollout enters Paused.
func IncPause(rollout, composition, reason string, aggregateOnly bool) {
	r, c := labels(rollout, composition, aggregateOnly)
	pausesTotal.WithLabelValues(r, c, reason).Inc()
}
