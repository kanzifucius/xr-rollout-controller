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
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	authzv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/xp"
)

const (
	xrGroup      = "example.org"
	xrVersion    = "v1"
	xrAPIVersion = xrGroup + "/" + xrVersion
	xrKind       = "XDatabase"
	xrPlural     = "xdatabases"
	xpGroup      = "apiextensions.crossplane.io"
	xpVersion    = "v1"
	xpAPIVersion = xpGroup + "/" + xpVersion
	confPrefix   = "conf-"
)

var fixtureOnce sync.Once

var _ = Describe("Plan conformance", func() {
	BeforeEach(func() {
		if !envtestOK {
			Skip("envtest not available")
		}
		fixtureOnce.Do(installFixtureCRDs)
	})

	AfterEach(func() {
		if !envtestOK {
			return
		}
		deletePrefixedRollouts()
		deletePrefixed(schema.GroupVersionKind{Group: xrGroup, Version: xrVersion, Kind: xrKind}, confPrefix)
		deletePrefixed(schema.GroupVersionKind{Group: xpGroup, Version: xpVersion, Kind: "Composition"}, confPrefix)
		deletePrefixed(schema.GroupVersionKind{Group: xpGroup, Version: xpVersion, Kind: "CompositionRevision"}, confPrefix)
		deleteNamed(schema.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingAdmissionPolicy"}, "compositionrollout-guards")
		deleteNamed(schema.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingAdmissionPolicy"}, "compositionrollout-overlap")
		deleteNamed(schema.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingAdmissionPolicyBinding"}, "compositionrollout-guards")
		deleteNamed(schema.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingAdmissionPolicyBinding"}, "compositionrollout-overlap")
		deleteNamed(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"}, "xr-access-rolebinding")
		deleteNamed(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}, "xr-rollout-controller-xr-access")
		deleteNamed(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}, "xr-rollout-example-org")
	})

	It("applies only spec.crossplane.compositionRevisionRef.name", func() {
		const (
			composition = confPrefix + "comp-own"
			revision    = confPrefix + "comp-own-2"
			xrName      = confPrefix + "xr-own"
			rolloutName = confPrefix + "rollout-own"
		)
		createComposition(composition)
		createRevision(composition, revision, 2)
		applyXR(xrName, composition, "", "gitops", false)

		reconcileRollout(newTestReconciler(false), rolloutName, composition)

		got := getXR(xrName)
		Expect(xp.GetCompositionRevisionRef(got)).To(Equal(revision))
		team, _, _ := unstructured.NestedString(got.Object, "spec", "teamField")
		Expect(team).To(Equal("owned-by-gitops"))
		Expect(got.GetLabels()).To(HaveKeyWithValue("team", "payments"))

		owned := managedFieldPaths(got, defaultFieldManager)
		Expect(owned).To(ContainElement(compositionRevisionRefField))
		for _, path := range owned {
			switch path {
			case compositionRevisionRefField, "metadata.name", "metadata.namespace":
			default:
				Fail("controller field manager co-owns " + path)
			}
		}
		gitops := managedFieldPaths(got, "gitops")
		Expect(gitops).To(ContainElement("spec.teamField"))
		Expect(gitops).To(ContainElement("metadata.labels.team"))
		Expect(gitops).NotTo(ContainElement(compositionRevisionRefField))

		st := rolloutState(getRollout(rolloutName), composition)
		Expect(st.Phase).To(Equal(rolloutsv1alpha1.PhaseCanarying))
		Expect(st.PauseReason).To(BeNil())
	})

	It("pauses the canary path on a persistent SSA conflict", func() {
		const (
			composition = confPrefix + "comp-conflict"
			revision    = confPrefix + "comp-conflict-2"
			xrName      = confPrefix + "xr-conflict"
			rolloutName = confPrefix + "rollout-conflict"
			oldRev      = confPrefix + "old-rev"
		)
		createComposition(composition)
		createRevision(composition, revision, 2)
		applyXR(xrName, composition, oldRev, "argocd", true)

		r := newTestReconciler(false)
		reconcileRollout(r, rolloutName, composition)

		st := rolloutState(getRollout(rolloutName), composition)
		Expect(st.Phase).To(Equal(rolloutsv1alpha1.PhasePaused))
		Expect(st.PhaseBeforePause).To(Equal(rolloutsv1alpha1.PhaseCanarying))
		Expect(st.PauseReason).NotTo(BeNil())
		Expect(st.PauseReason.Reason).To(Equal(rolloutsv1alpha1.PauseReasonPatchFailed))
		Expect(st.PauseReason.Message).To(ContainSubstring("argocd"))
		Expect(st.PauseReason.Message).To(ContainSubstring(compositionRevisionRefField))

		cr := getRollout(rolloutName)
		paused := findCondition(cr, rolloutsv1alpha1.ConditionPaused)
		Expect(paused.Status).To(Equal(metav1.ConditionTrue))
		Expect(paused.Reason).To(Equal(rolloutsv1alpha1.PauseReasonPatchFailed))
		Expect(xp.GetCompositionRevisionRef(getXR(xrName))).To(Equal(oldRev))

		reconcileRollout(r, rolloutName, composition)
		Expect(xp.GetCompositionRevisionRef(getXR(xrName))).To(Equal(oldRev))
		again := rolloutState(getRollout(rolloutName), composition)
		Expect(again.Phase).To(Equal(rolloutsv1alpha1.PhasePaused))
		Expect(again.PauseReason.Reason).To(Equal(rolloutsv1alpha1.PauseReasonPatchFailed))
	})

	It("lets force-ownership take the revision ref without pausing", func() {
		const (
			composition = confPrefix + "comp-force"
			revision    = confPrefix + "comp-force-2"
			xrName      = confPrefix + "xr-force"
			rolloutName = confPrefix + "rollout-force"
		)
		createComposition(composition)
		createRevision(composition, revision, 2)
		applyXR(xrName, composition, confPrefix+"old-force", "argocd", true)

		reconcileRollout(newTestReconciler(true), rolloutName, composition)

		got := getXR(xrName)
		Expect(xp.GetCompositionRevisionRef(got)).To(Equal(revision))
		team, _, _ := unstructured.NestedString(got.Object, "spec", "teamField")
		Expect(team).To(Equal("owned-by-argocd"))
		st := rolloutState(getRollout(rolloutName), composition)
		Expect(st.Phase).NotTo(Equal(rolloutsv1alpha1.PhasePaused))
		for _, path := range managedFieldPaths(got, defaultFieldManager) {
			switch path {
			case compositionRevisionRefField, "metadata.name", "metadata.namespace":
			default:
				Fail("force-ownership co-owns " + path)
			}
		}
	})

	It("rejects a second CompositionRollout that claims an already-claimed composition", func() {
		installShippedPolicies()
		const shared = confPrefix + "shared-composition"
		Expect(k8sClient.Create(ctx, newConfRollout(confPrefix+"vap-owner", shared))).To(Succeed())

		fresh := getRollout(confPrefix + "vap-owner")
		fresh.Spec.Suspend = true
		Expect(k8sClient.Update(ctx, fresh)).To(Succeed())

		Eventually(func() string {
			dup := newConfRollout(confPrefix+"vap-dup", shared)
			err := k8sClient.Create(ctx, dup)
			if err == nil {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, dup))).To(Succeed())
				return "admitted overlapping rollout; policy status " + policyStatus("compositionrollout-overlap")
			}
			return err.Error()
		}, 20*time.Second, time.Second).Should(ContainSubstring("already claimed"))

		otherName := confPrefix + "vap-other"
		Expect(k8sClient.Create(ctx, newConfRollout(otherName, confPrefix+"other-composition"))).To(Succeed())
		other := getRollout(otherName)
		other.Spec.Compositions = append(other.Spec.Compositions, rolloutsv1alpha1.CompositionTarget{Name: shared})
		Expect(k8sClient.Update(ctx, other)).To(MatchError(ContainSubstring("already claimed")))
	})

	It("grants the controller ServiceAccount the aggregated XR verbs", func() {
		leaf := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata": map[string]interface{}{
				"name": "xr-rollout-example-org",
				"labels": map[string]interface{}{
					aggregateToControllerLabel: "true",
				},
			},
			"rules": []interface{}{xrRule()},
		}}
		Expect(k8sClient.Create(ctx, leaf)).To(Succeed())

		// envtest does not run the cluster-role aggregation controller, so the
		// shipped role is installed with the rules that controller would copy
		// from the labelled leaf role above.
		role := mustUnstructuredFile(filepath.Join("..", "..", "config", "rbac", "xr_aggregate_role.yaml"))
		Expect(unstructured.SetNestedSlice(role.Object, []interface{}{xrRule()}, "rules")).To(Succeed())
		Expect(k8sClient.Create(ctx, role)).To(Succeed())

		const user = "system:serviceaccount:system:controller-manager"
		Expect(subjectAllows(user, "patch")).To(BeFalse())

		binding := mustUnstructuredFile(filepath.Join("..", "..", "config", "rbac", "xr_aggregate_role_binding.yaml"))
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())

		Expect(subjectAllows(user, "get")).To(BeTrue())
		Expect(subjectAllows(user, "list")).To(BeTrue())
		Expect(subjectAllows(user, "watch")).To(BeTrue())
		Expect(subjectAllows(user, "patch")).To(BeTrue())
		Expect(subjectAllows(user, "delete")).To(BeFalse())
		Expect(subjectAllows("system:serviceaccount:system:someone-else", "patch")).To(BeFalse())
	})
})

func installFixtureCRDs() {
	installCRD("compositions."+xpGroup, xpGroup, "Composition", "compositions", "Cluster", objectSchema(map[string]interface{}{
		"compositeTypeRef": refSchema(),
	}))
	installCRD("compositionrevisions."+xpGroup, xpGroup, "CompositionRevision", "compositionrevisions", "Cluster", objectSchema(map[string]interface{}{
		"revision":         map[string]interface{}{"type": "integer"},
		"compositeTypeRef": refSchema(),
	}))
	installCRD(xrPlural+"."+xrGroup, xrGroup, xrKind, xrPlural, "Namespaced", objectSchema(map[string]interface{}{
		"teamField": map[string]interface{}{"type": "string"},
		"crossplane": objectSchema(map[string]interface{}{
			"compositionUpdatePolicy": map[string]interface{}{"type": "string"},
			"compositionRef":          objectSchema(map[string]interface{}{"name": map[string]interface{}{"type": "string"}}),
			"compositionRevisionRef":  objectSchema(map[string]interface{}{"name": map[string]interface{}{"type": "string"}}),
		}),
	}))
	waitForResource(schema.GroupVersionKind{Group: xrGroup, Version: xrVersion, Kind: xrKind})
	waitForResource(schema.GroupVersionKind{Group: xpGroup, Version: xpVersion, Kind: "Composition"})
	waitForResource(schema.GroupVersionKind{Group: xpGroup, Version: xpVersion, Kind: "CompositionRevision"})
}

func refSchema() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"apiVersion": map[string]interface{}{"type": "string"},
		"kind":       map[string]interface{}{"type": "string"},
	})
}

func objectSchema(props map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": props}
}

func installCRD(name, group, kind, plural, scope string, specSchema map[string]interface{}) {
	crd := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinition",
		"metadata":   map[string]interface{}{"name": name},
		"spec": map[string]interface{}{
			"group": group,
			"scope": scope,
			"names": map[string]interface{}{
				"plural":   plural,
				"singular": strings.ToLower(kind),
				"kind":     kind,
				"listKind": kind + "List",
			},
			"versions": []interface{}{map[string]interface{}{
				"name":    "v1",
				"served":  true,
				"storage": true,
				"schema": map[string]interface{}{
					"openAPIV3Schema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"spec": specSchema,
						},
					},
				},
			}},
		},
	}}
	err := k8sClient.Create(ctx, crd)
	if apierrors.IsAlreadyExists(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	Eventually(func(g Gomega) {
		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"})
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, got)).To(Succeed())
		conds, found, err := unstructured.NestedSlice(got.Object, "status", "conditions")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(found).To(BeTrue())
		established := false
		for _, cond := range conds {
			m, ok := cond.(map[string]interface{})
			if !ok {
				continue
			}
			t, _, _ := unstructured.NestedString(m, "type")
			s, _, _ := unstructured.NestedString(m, "status")
			if t == "Established" && s == "True" {
				established = true
			}
		}
		g.Expect(established).To(BeTrue())
	}, 20*time.Second, 200*time.Millisecond).Should(Succeed())
}

func waitForResource(gvk schema.GroupVersionKind) {
	Eventually(func(g Gomega) {
		resetRESTMapper()
		_, err := k8sClient.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
		g.Expect(err).NotTo(HaveOccurred())
	}, 20*time.Second, 200*time.Millisecond).Should(Succeed())
}

func resetRESTMapper() {
	switch mapper := k8sClient.RESTMapper().(type) {
	case interface{ Reset() error }:
		_ = mapper.Reset()
	case interface{ Reset() }:
		mapper.Reset()
	default:
		if reset, ok := k8sClient.RESTMapper().(meta.ResettableRESTMapper); ok {
			reset.Reset()
		}
	}
}

func newTestReconciler(force bool) *CompositionRolloutReconciler {
	resetRESTMapper()
	dyn, err := dynamic.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
	return &CompositionRolloutReconciler{
		Client:         k8sClient,
		Scheme:         k8sClient.Scheme(),
		Dynamic:        dyn,
		Mapper:         k8sClient.RESTMapper(),
		Recorder:       record.NewFakeRecorder(32),
		FieldManager:   defaultFieldManager,
		ForceOwnership: force,
		Environment:    "prod",
	}
}

func createComposition(name string) {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": xpAPIVersion,
		"kind":       "Composition",
		"metadata":   map[string]interface{}{"name": name},
		"spec": map[string]interface{}{
			"compositeTypeRef": map[string]interface{}{"apiVersion": xrAPIVersion, "kind": xrKind},
		},
	}}
	Expect(k8sClient.Create(ctx, u)).To(Succeed())
}

func createRevision(composition, name string, number int64) {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": xpAPIVersion,
		"kind":       "CompositionRevision",
		"metadata": map[string]interface{}{
			"name":   name,
			"labels": map[string]interface{}{"crossplane.io/composition-name": composition},
		},
		"spec": map[string]interface{}{
			"revision":         number,
			"compositeTypeRef": map[string]interface{}{"apiVersion": xrAPIVersion, "kind": xrKind},
		},
	}}
	Expect(k8sClient.Create(ctx, u)).To(Succeed())
}

func applyXR(name, composition, rev, manager string, includeRef bool) {
	crossplane := map[string]interface{}{
		"compositionRef":          map[string]interface{}{"name": composition},
		"compositionUpdatePolicy": "Manual",
	}
	if includeRef {
		crossplane["compositionRevisionRef"] = map[string]interface{}{"name": rev}
	}
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": xrAPIVersion,
		"kind":       xrKind,
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": "default",
			"labels":    map[string]interface{}{"team": "payments"},
		},
		"spec": map[string]interface{}{
			"teamField":  "owned-by-" + manager,
			"crossplane": crossplane,
		},
	}}
	Expect(k8sClient.Patch(ctx, u, client.Apply, client.FieldOwner(manager), client.ForceOwnership)).To(Succeed())
}

func newConfRollout(name, composition string) *rolloutsv1alpha1.CompositionRollout {
	auto := true
	count := int32(1)
	return &rolloutsv1alpha1.CompositionRollout{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: rolloutsv1alpha1.CompositionRolloutSpec{
			Compositions: []rolloutsv1alpha1.CompositionTarget{{Name: composition}},
			AutoStart:    &auto,
			Canary: rolloutsv1alpha1.CanarySpec{
				Strategy: rolloutsv1alpha1.CanaryStrategyCount,
				Count:    &count,
			},
			Soak: rolloutsv1alpha1.SoakSpec{Duration: metav1.Duration{Duration: 30 * time.Minute}},
			Promotion: rolloutsv1alpha1.PromotionSpec{
				DefaultPolicy: rolloutsv1alpha1.PromotionManualApproval,
			},
			Waves: rolloutsv1alpha1.WaveSpec{BatchSize: intstr.FromString("10%")},
		},
	}
}

func reconcileRollout(r *CompositionRolloutReconciler, rolloutName, composition string) {
	err := k8sClient.Create(ctx, newConfRollout(rolloutName, composition))
	if !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: rolloutName}})
	Expect(err).NotTo(HaveOccurred())
}

func getRollout(name string) *rolloutsv1alpha1.CompositionRollout {
	cr := &rolloutsv1alpha1.CompositionRollout{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, cr)).To(Succeed())
	return cr
}

func getXR(name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(xrAPIVersion)
	u.SetKind(xrKind)
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, u)).To(Succeed())
	return u
}

func rolloutState(cr *rolloutsv1alpha1.CompositionRollout, composition string) rolloutsv1alpha1.CompositionRolloutState {
	for i := range cr.Status.Rollouts {
		if cr.Status.Rollouts[i].Composition == composition {
			return cr.Status.Rollouts[i]
		}
	}
	Fail("missing rollout state for " + composition)
	return rolloutsv1alpha1.CompositionRolloutState{}
}

func findCondition(cr *rolloutsv1alpha1.CompositionRollout, condType string) metav1.Condition {
	for i := range cr.Status.Conditions {
		if cr.Status.Conditions[i].Type == condType {
			return cr.Status.Conditions[i]
		}
	}
	return metav1.Condition{}
}

func managedFieldPaths(u *unstructured.Unstructured, manager string) []string {
	var paths []string
	for _, entry := range u.GetManagedFields() {
		if entry.Manager != manager || entry.FieldsV1 == nil {
			continue
		}
		var root interface{}
		Expect(json.Unmarshal(entry.FieldsV1.Raw, &root)).To(Succeed())
		paths = append(paths, collectFieldPaths("", root)...)
	}
	sort.Strings(paths)
	return paths
}

func collectFieldPaths(prefix string, node interface{}) []string {
	children, ok := node.(map[string]interface{})
	if !ok || len(children) == 0 {
		if prefix == "" {
			return nil
		}
		return []string{prefix}
	}
	paths := make([]string, 0, len(children))
	for key, child := range children {
		name := strings.TrimPrefix(key, "f:")
		next := name
		if prefix != "" {
			next = prefix + "." + name
		}
		paths = append(paths, collectFieldPaths(next, child)...)
	}
	return paths
}

func installShippedPolicies() {
	raw, err := os.ReadFile(filepath.Join("..", "..", "config", "policy", "approval-vap.yaml"))
	Expect(err).NotTo(HaveOccurred())
	docs, err := decodeYAMLObjects(raw)
	Expect(err).NotTo(HaveOccurred())
	Expect(docs).NotTo(BeEmpty())
	for _, doc := range docs {
		err := k8sClient.Create(ctx, doc)
		if apierrors.IsAlreadyExists(err) {
			continue
		}
		Expect(err).NotTo(HaveOccurred())
	}
}

func policyStatus(name string) string {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingAdmissionPolicy"})
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, u); err != nil {
		return err.Error()
	}
	raw, err := json.Marshal(u.Object["status"])
	if err != nil {
		return err.Error()
	}
	return string(raw)
}

func xrRule() map[string]interface{} {
	return map[string]interface{}{
		"apiGroups": []interface{}{xrGroup},
		"resources": []interface{}{xrPlural},
		"verbs":     []interface{}{"get", "list", "watch", "patch"},
	}
}

func subjectAllows(user, verb string) bool {
	sar := &authzv1.SubjectAccessReview{
		Spec: authzv1.SubjectAccessReviewSpec{
			User: user,
			ResourceAttributes: &authzv1.ResourceAttributes{
				Group:    xrGroup,
				Version:  xrVersion,
				Resource: xrPlural,
				Verb:     verb,
			},
		},
	}
	Expect(k8sClient.Create(ctx, sar)).To(Succeed())
	return sar.Status.Allowed
}

func mustUnstructuredFile(path string) *unstructured.Unstructured {
	raw, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	docs, err := decodeYAMLObjects(raw)
	Expect(err).NotTo(HaveOccurred())
	Expect(docs).To(HaveLen(1))
	return docs[0]
}

func deletePrefixedRollouts() {
	var list rolloutsv1alpha1.CompositionRolloutList
	Expect(k8sClient.List(ctx, &list)).To(Succeed())
	for i := range list.Items {
		if !strings.HasPrefix(list.Items[i].Name, confPrefix) {
			continue
		}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &list.Items[i]))).To(Succeed())
	}
}

func deletePrefixed(gvk schema.GroupVersionKind, prefix string) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind + "List"})
	err := k8sClient.List(ctx, list)
	if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	for i := range list.Items {
		item := &list.Items[i]
		if !strings.HasPrefix(item.GetName(), prefix) {
			continue
		}
		item.SetGroupVersionKind(gvk)
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, item))).To(Succeed())
	}
}

func deleteNamed(gvk schema.GroupVersionKind, name string) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	u.SetName(name)
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, u))).To(Succeed())
}
