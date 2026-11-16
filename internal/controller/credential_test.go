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

package controller

import (
	"context"
	"errors"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

const (
	controllerNamespace = "xr-rollout"
	paymentsSecret      = "acc-payments"
	platformSecret      = "acc-platform"
)

func TestKubeconfigRef(t *testing.T) {
	t.Parallel()
	cr := rolloutForCluster("acc")
	name, ns := kubeconfigRef(cr, controllerNamespace)
	if name != "acc" || ns != controllerNamespace {
		t.Fatalf("default = %s/%s", ns, name)
	}
	cr.Spec.KubeconfigSecretRef = &rolloutsv1alpha1.KubeconfigSecretReference{Name: paymentsSecret}
	name, ns = kubeconfigRef(cr, controllerNamespace)
	if name != paymentsSecret || ns != controllerNamespace {
		t.Fatalf("named = %s/%s", ns, name)
	}
	cr.Spec.KubeconfigSecretRef.Namespace = "team-secrets"
	name, ns = kubeconfigRef(cr, controllerNamespace)
	if name != paymentsSecret || ns != "team-secrets" {
		t.Fatalf("namespaced = %s/%s", ns, name)
	}
	indexed := kubeconfigNameIndex(cr)
	if len(indexed) != 1 || indexed[0] != paymentsSecret {
		t.Fatalf("index = %v", indexed)
	}
}

func TestClientsForSecretName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ref  string
		want string
	}{
		{name: "default", want: "acc"},
		{name: "explicit", ref: paymentsSecret, want: paymentsSecret},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubClusters{engaged: true, env: "acceptance"}
			r := &RemoteReconciler{Clusters: stub, Namespace: controllerNamespace}
			cr := rolloutForCluster("acc")
			if tc.ref != "" {
				cr.Spec.KubeconfigSecretRef = &rolloutsv1alpha1.KubeconfigSecretReference{Name: tc.ref}
			}
			_, err := r.clientsFor(context.Background(), cr)
			if stub.last != tc.want {
				t.Fatalf("client = %q, err = %v", stub.last, err)
			}
		})
	}
}

func TestClientsForRejectsAnotherNamespace(t *testing.T) {
	t.Parallel()
	stub := &stubClusters{engaged: true, env: "acceptance"}
	r := &RemoteReconciler{Clusters: stub, Namespace: controllerNamespace}
	cr := rolloutForCluster("acc")
	cr.Spec.KubeconfigSecretRef = &rolloutsv1alpha1.KubeconfigSecretReference{
		Name:      paymentsSecret,
		Namespace: "other",
	}
	_, err := r.clientsFor(context.Background(), cr)
	var cred *credentialUnavailableError
	if !errors.As(err, &cred) {
		t.Fatalf("err = %v", err)
	}
	if stub.gets != 0 {
		t.Fatalf("Get calls = %d", stub.gets)
	}
}

func TestMissingSecretPausesOnlyThatRollout(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	if err := rolloutsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	on := true
	payments := rolloutForCluster("acc")
	payments.Name = "payments"
	payments.Spec.DryRun = &on
	payments.Spec.KubeconfigSecretRef = &rolloutsv1alpha1.KubeconfigSecretReference{Name: paymentsSecret}
	platform := rolloutForCluster("acc")
	platform.Name = "platform"
	platform.Spec.KubeconfigSecretRef = &rolloutsv1alpha1.KubeconfigSecretReference{Name: platformSecret}
	platform.Spec.Compositions = []rolloutsv1alpha1.CompositionTarget{{Name: "platform.example.org"}}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&rolloutsv1alpha1.CompositionRollout{}).
		WithObjects(payments, platform).
		Build()
	stub := &stubClusters{
		engaged: true,
		env:     "acceptance",
		failures: map[string]string{
			paymentsSecret: "kubeconfig Secret xr-rollout/acc-payments will not parse: bad",
		},
	}
	inner := &CompositionRolloutReconciler{Client: cl, Recorder: record.NewFakeRecorder(4)}
	inner.remote = &RemoteReconciler{Inner: inner, Clusters: stub, Hub: cl, Namespace: controllerNamespace}
	if _, err := inner.reconcileHub(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "payments"}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if stub.gets != 0 {
		t.Fatalf("Get calls = %d", stub.gets)
	}
	got := &rolloutsv1alpha1.CompositionRollout{}
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "payments"}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Status.Rollouts) != 1 {
		t.Fatalf("rollouts = %#v", got.Status.Rollouts)
	}
	st := got.Status.Rollouts[0]
	if st.Phase != rolloutsv1alpha1.PhasePaused || st.PauseReason == nil || st.PauseReason.Reason != rolloutsv1alpha1.PauseReasonCredentialUnavailable {
		t.Fatalf("state = %#v", st)
	}
	if st.DryRun != nil || len(st.Simulation()) != 0 {
		t.Fatalf("wouldUpdate = %#v", st.DryRun)
	}
	other := &rolloutsv1alpha1.CompositionRollout{}
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "platform"}, other); err != nil {
		t.Fatalf("sibling: %v", err)
	}
	if len(other.Status.Rollouts) != 0 || other.Status.ObservedGeneration != 0 {
		t.Fatalf("sibling status = %#v", other.Status)
	}
}

func TestMissingEnvironmentDoesNotWriteStatus(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	if err := rolloutsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	cr := rolloutForCluster("acc")
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&rolloutsv1alpha1.CompositionRollout{}).
		WithObjects(cr).
		Build()
	stub := &stubClusters{engaged: true}
	inner := &CompositionRolloutReconciler{Client: cl, Recorder: record.NewFakeRecorder(2)}
	inner.remote = &RemoteReconciler{Inner: inner, Clusters: stub, Hub: cl, Namespace: controllerNamespace}
	if _, err := inner.reconcileHub(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: cr.Name}}); err == nil {
		t.Fatal("expected error")
	}
	if stub.gets != 0 {
		t.Fatalf("Get calls = %d", stub.gets)
	}
	got := &rolloutsv1alpha1.CompositionRollout{}
	if err := cl.Get(context.Background(), client.ObjectKey{Name: cr.Name}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Status.Rollouts) != 0 || got.Status.ObservedGeneration != 0 {
		t.Fatalf("status = %#v", got.Status)
	}
}

func TestPauseReasonForPatch(t *testing.T) {
	t.Parallel()
	denied := apierrors.NewForbidden(schema.GroupResource{Group: "payments.example.org", Resource: "databases"}, "db", fmt.Errorf("denied"))
	if got := pauseReasonForPatch(denied); got != rolloutsv1alpha1.PauseReasonCredentialDenied {
		t.Fatalf("reason = %s", got)
	}
	if got := pauseReasonForPatch(fmt.Errorf("conflict")); got != rolloutsv1alpha1.PauseReasonPatchFailed {
		t.Fatalf("reason = %s", got)
	}
}
