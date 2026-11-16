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
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

func TestPollInterval(t *testing.T) {
	t.Parallel()
	if PollInterval != 30*time.Second {
		t.Fatalf("PollInterval = %s, want 30s", PollInterval)
	}
}

func TestTriggerRevisionIsRequired(t *testing.T) {
	t.Parallel()
	cl := fakeClient(t, baseRollout("team-payments"))
	cmd := NewRoot(Options{Client: cl})
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"approve", "team-payments", "--composition", "platformdb.example.org"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("approve without --revision succeeded")
	}
}

func TestTableAndJSONOutput(t *testing.T) {
	t.Parallel()
	rollout := baseRollout("team-payments")
	rollout.Status.Rollouts = []rolloutsv1alpha1.CompositionRolloutState{{
		Composition:    "platformdb.example.org",
		Phase:          rolloutsv1alpha1.PhasePaused,
		TargetRevision: rolloutsv1alpha1.RevisionInfo{Name: "platformdb.example.org-aaa", Revision: 3},
		PauseReason:    &rolloutsv1alpha1.PauseReason{Reason: rolloutsv1alpha1.PauseReasonCanaryUnhealthy, Message: "not Ready"},
		Canary: rolloutsv1alpha1.CanaryStatus{
			Selected:  []rolloutsv1alpha1.XRRef{{Namespace: "payments", Name: "db-1"}},
			Unhealthy: 1,
		},
	}}
	pipeline := basePipeline("environments")
	pipeline.Status.Hops = []rolloutsv1alpha1.PromotionHopStatus{{
		Rollout:     "team-payments",
		Composition: "platformdb.example.org",
		Revision:    "platformdb.example.org-aaa",
		From:        "dev",
		To:          "test",
		Action:      "Wait",
		Reason:      "InterClusterSoak",
	}}
	cl := fakeClient(t, rollout, pipeline)

	table := runCmd(t, cl, "list")
	for _, want := range []string{"NAME", "team-payments", "platformdb.example.org", "prod", "Paused", "platformdb.example.org-aaa", "CanaryUnhealthy"} {
		if !strings.Contains(table, want) {
			t.Fatalf("list table missing %q:\n%s", want, table)
		}
	}
	if strings.HasPrefix(strings.TrimSpace(table), "{") {
		t.Fatalf("list table is JSON:\n%s", table)
	}

	raw := runCmd(t, cl, "list", "-o", "json")
	var listed rolloutsv1alpha1.CompositionRolloutList
	if err := json.Unmarshal([]byte(raw), &listed); err != nil {
		t.Fatalf("list json: %v\n%s", err, raw)
	}
	if len(listed.Items) != 1 || listed.Items[0].Name != "team-payments" {
		t.Fatalf("list json items = %#v", listed.Items)
	}
	if listed.Items[0].Status.Rollouts[0].Phase != rolloutsv1alpha1.PhasePaused {
		t.Fatalf("list json phase = %s", listed.Items[0].Status.Rollouts[0].Phase)
	}

	gotTable := runCmd(t, cl, "get", "team-payments")
	if !strings.Contains(gotTable, "CanaryUnhealthy") || strings.HasPrefix(strings.TrimSpace(gotTable), "{") {
		t.Fatalf("get table = %s", gotTable)
	}
	gotRaw := runCmd(t, cl, "get", "team-payments", "-o", "json")
	var one rolloutsv1alpha1.CompositionRollout
	if err := json.Unmarshal([]byte(gotRaw), &one); err != nil {
		t.Fatalf("get json: %v\n%s", err, gotRaw)
	}
	if one.Spec.Cluster != "prod" || one.Status.Rollouts[0].Canary.Selected[0].Name != "db-1" {
		t.Fatalf("get json = %#v", one)
	}

	pipeTable := runCmd(t, cl, "pipeline", "status")
	for _, want := range []string{"environments", "dev", "test", "InterClusterSoak", "team-payments"} {
		if !strings.Contains(pipeTable, want) {
			t.Fatalf("pipeline table missing %q:\n%s", want, pipeTable)
		}
	}
	if strings.HasPrefix(strings.TrimSpace(pipeTable), "{") {
		t.Fatalf("pipeline table is JSON:\n%s", pipeTable)
	}
	pipeRaw := runCmd(t, cl, "pipeline", "status", "-o", "json")
	var pipes rolloutsv1alpha1.PromotionPipelineList
	if err := json.Unmarshal([]byte(pipeRaw), &pipes); err != nil {
		t.Fatalf("pipeline json: %v\n%s", err, pipeRaw)
	}
	if len(pipes.Items) != 1 || pipes.Items[0].Status.Hops[0].Reason != "InterClusterSoak" {
		t.Fatalf("pipeline json = %#v", pipes.Items)
	}

	onePipe := runCmd(t, cl, "pipeline", "status", "environments", "-o", "json")
	var pipe rolloutsv1alpha1.PromotionPipeline
	if err := json.Unmarshal([]byte(onePipe), &pipe); err != nil {
		t.Fatalf("pipeline get json: %v\n%s", err, onePipe)
	}
	if pipe.Name != "environments" {
		t.Fatalf("pipeline name = %s", pipe.Name)
	}
}

func fakeClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := rolloutsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func runCmd(t *testing.T, cl client.Client, args ...string) string {
	t.Helper()
	cmd := NewRoot(Options{Client: cl})
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, buf.String())
	}
	return buf.String()
}
