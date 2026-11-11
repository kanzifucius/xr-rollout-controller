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

package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/test/utils"
)

const (
	xrGroup                = "e2e.platform.example.org"
	xrVersion              = "v1alpha1"
	xrKind                 = "XNop"
	xrResource             = "xnops.e2e.platform.example.org"
	scenarioLabel          = "e2e.platform.example.org/scenario"
	canaryLabel            = "e2e.platform.example.org/canary"
	canaryLabelValue       = "yes"
	controllerFieldManager = "xr-rollout-controller"
	crossplaneNamespace    = "crossplane-system"
	crossplaneChartVersion = "2.4.3"
	helmReleaseVersion     = "v3.18.6"
	functionPackage        = "xpkg.upbound.io/crossplane-contrib/function-patch-and-transform:v0.11.2"
	controllerDeploy       = "xr-rollout-controller-controller-manager"
	rolloutResource        = "compositionrollouts.rollouts.platform.example.org"
	compositionResource    = "compositions.apiextensions.crossplane.io"
	revisionResource       = "compositionrevisions.apiextensions.crossplane.io"

	allowedRevisionField = "spec.crossplane.compositionRevisionRef.name"
)

type xrSpec struct {
	name   string
	canary bool
}

type seededFleet struct {
	scenario    string
	ns          string
	composition string
	rollout     string
	source      string
	target      string
	xrs         []string
}

type rolloutSpec struct {
	strategy      string
	count         int32
	percent       int32
	matchNone     bool
	soak          string
	stabilisation string
	waveSoak      string
	dev           string
	prod          string
	defaultPolicy string
	suspend       bool
	rolloutName   string
}

var activeFleet *seededFleet

func installScenarioCluster() {
	By("installing Crossplane")
	if !resourceExists("crd", "compositions.apiextensions.crossplane.io") {
		helm := ensureHelm()
		runOrFail(helm, "repo", "add", "crossplane-stable", "https://charts.crossplane.io/stable", "--force-update")
		runOrFail(helm, "repo", "update", "crossplane-stable")
		runOrFail(helm, "upgrade", "--install", "crossplane", "crossplane-stable/crossplane",
			"--namespace", crossplaneNamespace, "--create-namespace",
			"--version", crossplaneChartVersion, "--wait", "--timeout", "10m")
	}

	By("installing function-patch-and-transform")
	applyYAML(functionYAML())
	Eventually(func(g Gomega) {
		out, err := run("kubectl", "get", "functions.pkg.crossplane.io", "function-patch-and-transform", "-o", "json")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(conditionTrue(out, "Healthy")).To(BeTrue())
	}).WithTimeout(8 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

	By("installing the scenario XRD")
	applyYAML(xrdYAML())
	runOrFail("kubectl", "wait", "--for=condition=Established", "crd/"+xrResource, "--timeout=180s")
	Eventually(func(g Gomega) {
		raw, err := run("kubectl", "get", "crd", xrResource, "-o", "json")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(raw).To(ContainSubstring("compositionRevisionRef"))
	}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())

	By("deploying the rollout controller")
	makeInRepo("install")
	makeInRepo("deploy", "IMG="+projectImage)
	applyYAML(xrRBACYAML())
	setControllerEnvironment("dev")
	waitControllerReady()
}

func teardownScenarioCluster() {
	if helm, err := exec.LookPath("helm"); err == nil {
		_, _ = run(helm, "uninstall", "crossplane", "--namespace", crossplaneNamespace)
	}
	_, _ = run("kubectl", "delete", "functions.pkg.crossplane.io", "function-patch-and-transform", "--wait=false")
	_, _ = run("kubectl", "delete", "compositeresourcedefinitions.apiextensions.crossplane.io", xrResource, "--wait=false")
	_, _ = run("kubectl", "delete", "clusterrolebinding", "xr-rollout-e2e-xnops", "--wait=false")
	_, _ = run("kubectl", "delete", "clusterrole", "xr-rollout-e2e-xnops", "--wait=false")
	makeInRepo("undeploy", "ignore-not-found=true")
}

func seedFleet(scenario string, specs []xrSpec) seededFleet {
	f := seededFleet{
		scenario:    scenario,
		ns:          "e2e-" + scenario,
		composition: scenario,
		rollout:     scenario,
	}
	activeFleet = &f
	_, err := run("kubectl", "create", "ns", f.ns)
	if err != nil && !strings.Contains(err.Error(), "AlreadyExists") {
		Expect(err).NotTo(HaveOccurred())
	}
	runOrFail("kubectl", "label", "ns", f.ns, scenarioLabel+"="+scenario, "--overwrite")

	applyYAML(compositionYAML(f.composition, "1"))
	f.source = waitRevision(f.composition, 1)
	for _, xr := range specs {
		applyYAML(xrYAML(f, xr))
		f.xrs = append(f.xrs, xr.name)
	}
	for _, name := range f.xrs {
		waitRevisionRef(f.ns, name, f.source)
		waitReady(f.ns, name)
	}
	applyYAML(compositionYAML(f.composition, "2"))
	f.target = waitRevision(f.composition, 2)
	Consistently(func(g Gomega) {
		for _, name := range f.xrs {
			g.Expect(revisionRef(mustXR(f.ns, name))).To(Equal(f.source))
		}
	}).WithTimeout(15 * time.Second).WithPolling(time.Second).Should(Succeed())
	return f
}

func cleanupActiveFleet() {
	if activeFleet == nil {
		return
	}
	f := activeFleet
	activeFleet = nil
	_, _ = run("kubectl", "delete", "ns", f.ns, "--wait=false")
	_, _ = run("kubectl", "delete", rolloutResource, "-l", scenarioLabel+"="+f.scenario, "--wait=false")
	_, _ = run("kubectl", "delete", compositionResource, f.composition, "--wait=false")
	_, _ = run(
		"kubectl", "delete", revisionResource,
		"-l", "crossplane.io/composition-name="+f.composition, "--wait=false",
	)
}

func applyRollout(f seededFleet, spec rolloutSpec) {
	if spec.rolloutName == "" {
		spec.rolloutName = f.rollout
	}
	applyYAML(rolloutYAML(f, spec))
}

func rolloutYAML(f seededFleet, spec rolloutSpec) string {
	if spec.soak == "" {
		spec.soak = "3s"
	}
	if spec.waveSoak == "" {
		spec.waveSoak = "2s"
	}
	if spec.stabilisation == "" {
		spec.stabilisation = "5m"
	}
	if spec.defaultPolicy == "" {
		spec.defaultPolicy = string(rolloutsv1alpha1.PromotionManualApproval)
	}
	if spec.strategy == "" {
		spec.strategy = string(rolloutsv1alpha1.CanaryStrategyCount)
	}
	name := spec.rolloutName
	if name == "" {
		name = f.rollout
	}

	var b strings.Builder
	_, _ = fmt.Fprintf(&b, `apiVersion: rollouts.platform.example.org/v1alpha1
kind: CompositionRollout
metadata:
  name: %s
  labels:
    %s: %s
spec:
  compositions:
    - name: %s
`, name, scenarioLabel, f.scenario, f.composition)
	_, _ = fmt.Fprintf(&b, "  canary:\n    strategy: %s\n", spec.strategy)
	switch spec.strategy {
	case string(rolloutsv1alpha1.CanaryStrategyCount):
		_, _ = fmt.Fprintf(&b, "    count: %d\n", spec.count)
	case string(rolloutsv1alpha1.CanaryStrategyPercent):
		_, _ = fmt.Fprintf(&b, "    percent: %d\n", spec.percent)
	default:
		value := canaryLabelValue
		if spec.matchNone {
			value = "missing"
		}
		_, _ = fmt.Fprintf(&b, "    labelSelector:\n      matchLabels:\n        %s: %q\n", canaryLabel, value)
	}
	_, _ = fmt.Fprintf(&b, "  autoStart: true\n  onNewRevision: Queue\n")
	if spec.suspend {
		b.WriteString("  suspend: true\n")
	}
	_, _ = fmt.Fprintf(&b, `  soak:
    duration: %s
    maxUnhealthy: 0
    stabilisationTimeout: %s
  promotion:
    defaultPolicy: %s
`, spec.soak, spec.stabilisation, spec.defaultPolicy)
	if spec.dev != "" || spec.prod != "" {
		b.WriteString("    environments:\n")
		if spec.dev != "" {
			_, _ = fmt.Fprintf(&b, "      dev: %s\n", spec.dev)
		}
		if spec.prod != "" {
			_, _ = fmt.Fprintf(&b, "      prod: %s\n", spec.prod)
		}
	}
	_, _ = fmt.Fprintf(&b, `  waves:
    batchSize: "100%%"
    maxConcurrentUpdates: 10
    waveSoak: %s
    maxUnhealthy: 0
`, spec.waveSoak)
	return b.String()
}

func waitRevision(composition string, number int64) string {
	var found string
	Eventually(func(g Gomega) {
		raw := mustGet(revisionResource, "-l", "crossplane.io/composition-name="+composition)
		var list struct {
			Items []struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
				Spec struct {
					Revision int64 `json:"revision"`
				} `json:"spec"`
			} `json:"items"`
		}
		g.Expect(json.Unmarshal([]byte(raw), &list)).To(Succeed())
		found = ""
		for _, item := range list.Items {
			if item.Spec.Revision == number {
				found = item.Metadata.Name
			}
		}
		g.Expect(found).NotTo(BeEmpty(), "revision %d of %s missing in %s", number, composition, raw)
	}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
	return found
}

func waitRevisionRef(ns, name, rev string) {
	Eventually(func(g Gomega) {
		raw, err := getXR(ns, name)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(revisionRef(raw)).To(Equal(rev))
	}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
}

func waitReady(ns, name string) {
	Eventually(func(g Gomega) {
		raw, err := getXR(ns, name)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(conditionTrue(string(raw), "Ready")).To(BeTrue())
		g.Expect(conditionTrue(string(raw), "Synced")).To(BeTrue())
	}).WithTimeout(4 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
}

func waitState(
	f seededFleet,
	timeout time.Duration,
	match func(rolloutsv1alpha1.CompositionRolloutState) bool,
) rolloutsv1alpha1.CompositionRolloutState {
	var got rolloutsv1alpha1.CompositionRolloutState
	Eventually(func(g Gomega) {
		cr, err := getRollout(f.rollout)
		g.Expect(err).NotTo(HaveOccurred())
		st, ok := findState(cr, f.composition)
		g.Expect(ok).To(BeTrue(), "no status for %s", f.composition)
		g.Expect(match(st)).To(BeTrue(), stateDetail(st))
		got = st
	}).WithTimeout(timeout).WithPolling(2 * time.Second).Should(Succeed())
	return got
}

func stateDetail(st rolloutsv1alpha1.CompositionRolloutState) string {
	return fmt.Sprintf(
		"phase=%s reason=%s before=%s target=%s selected=%v aborted=%s total=%d unmanaged=%d",
		st.Phase, pauseReason(st), st.PhaseBeforePause, st.TargetRevision.Name,
		selectedKeys(st), st.AbortedRevision, st.TotalXRs, st.UnmanagedXRs,
	)
}

func waitPhase(f seededFleet, phase rolloutsv1alpha1.RolloutPhase) rolloutsv1alpha1.CompositionRolloutState {
	return waitState(f, 3*time.Minute, func(st rolloutsv1alpha1.CompositionRolloutState) bool {
		return st.Phase == phase
	})
}

func waitPaused(f seededFleet, reason string) rolloutsv1alpha1.CompositionRolloutState {
	return waitState(f, 3*time.Minute, func(st rolloutsv1alpha1.CompositionRolloutState) bool {
		return st.Phase == rolloutsv1alpha1.PhasePaused && pauseReason(st) == reason
	})
}

func waitFinished(f seededFleet) {
	Eventually(func(g Gomega) {
		for _, name := range f.xrs {
			raw, err := getXR(f.ns, name)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(revisionRef(raw)).To(Equal(f.target), name)
		}
		cr, err := getRollout(f.rollout)
		g.Expect(err).NotTo(HaveOccurred())
		st, ok := findState(cr, f.composition)
		g.Expect(ok).To(BeTrue())
		g.Expect(st.Phase).To(BeElementOf(rolloutsv1alpha1.PhaseIdle, rolloutsv1alpha1.PhaseCompleted))
		g.Expect(pauseReason(st)).To(BeEmpty())
	}).WithTimeout(4 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
}

func snapshotFleet(f seededFleet) map[string][]byte {
	out := make(map[string][]byte, len(f.xrs))
	for _, name := range f.xrs {
		out[name] = mustXR(f.ns, name)
	}
	return out
}

func assertRevisionWrite(f seededFleet, name, rev string, before map[string][]byte) {
	after := mustXR(f.ns, name)
	Expect(revisionRef(after)).To(Equal(rev), name)
	Expect(specWithoutRevisionName(before[name])).To(Equal(specWithoutRevisionName(after)), name)
	Expect(labelsOf(before[name])).To(Equal(labelsOf(after)), name)
	assertControllerFields(after, revisionRef(before[name]) != rev)
}

func assertUnchangedXR(f seededFleet, name string, before map[string][]byte) {
	after := mustXR(f.ns, name)
	Expect(canonicalSpec(after)).To(Equal(canonicalSpec(before[name])), name)
	Expect(labelsOf(before[name])).To(Equal(labelsOf(after)), name)
	assertControllerFields(after, false)
}

func assertControllerFields(raw []byte, wrote bool) {
	paths := controllerFieldPaths(raw)
	if !wrote {
		Expect(paths).To(BeEmpty(), "controller field manager owned %v", paths)
		return
	}
	// Crossplane updates the XR after the rollout patch and can take the field
	// back. An empty manager entry is fine. Anything still owned must be the
	// revision name or the object identity.
	for _, path := range paths {
		switch path {
		case allowedRevisionField, "metadata.name", "metadata.namespace", "apiVersion", "kind":
		default:
			Expect(path).To(BeEmpty(), "controller owned %s; fields %v", path, paths)
		}
	}
}

func addTrigger(rollout, composition, action, revision string) {
	cr, err := getRollout(rollout)
	Expect(err).NotTo(HaveOccurred())
	triggers := append(cr.Spec.Triggers, rolloutsv1alpha1.RolloutTrigger{
		Composition: composition,
		Action:      rolloutsv1alpha1.TriggerAction(action),
		Revision:    revision,
		Actor:       "e2e",
	})
	raw, err := json.Marshal(triggers)
	Expect(err).NotTo(HaveOccurred())
	op := "add"
	if len(cr.Spec.Triggers) > 0 {
		op = "replace"
	}
	runOrFail("kubectl", "patch", rolloutResource, rollout, "--type=json",
		"-p", fmt.Sprintf(`[{"op":"%s","path":"/spec/triggers","value":%s}]`, op, raw))
}

func setXRConditions(ns, name, ready string) {
	when := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	body := fmt.Sprintf(`{"status":{"conditions":[`+
		`{"type":"Ready","status":%q,"reason":"E2E","message":"e2e","lastTransitionTime":%q},`+
		`{"type":"Synced","status":"True","reason":"E2E","message":"e2e","lastTransitionTime":%q}`+
		`]}}`, ready, when, when)
	runOrFail("kubectl", "patch", xrResource, name, "-n", ns, "--subresource=status", "--type=merge", "-p", body)
}

func ownRevision(ns, name, composition, rev string) {
	// Keep Manual and the composition ref in the apply. Omitting them lets
	// CRD defaulting turn the policy into Automatic and drop the XR from the fleet.
	//
	// Keep every live label too. kubectl apply --server-side migrates
	// client-side ownership onto this field manager, then deletes migrated
	// fields the manifest omits. Dropping the canary label makes the selector
	// match nobody, so the rollout pauses with CanarySetEmpty and never
	// attempts the conflicting revision patch.
	doc := fmt.Sprintf(`apiVersion: %s/%s
kind: %s
metadata:
  name: %s
  namespace: %s
%s
spec:
  marker: keep-%s
  crossplane:
    compositionUpdatePolicy: Manual
    compositionRef:
      name: %s
    compositionRevisionRef:
      name: %s
`, xrGroup, xrVersion, xrKind, name, ns, labelBlock(labelsOf(mustXR(ns, name))), name, composition, rev)
	path := writeTemp(doc)
	defer func() { _ = os.Remove(path) }()
	runOrFail("kubectl", "apply", "--server-side", "--field-manager=gitops", "--force-conflicts", "-f", path)
}

func labelBlock(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("  labels:\n")
	for _, key := range keys {
		_, _ = fmt.Fprintf(&b, "    %s: %q\n", key, labels[key])
	}
	return b.String()
}

func scaleCrossplane(replicas int) {
	runOrFail("kubectl", "scale", "deployment/crossplane", "-n", crossplaneNamespace, "--replicas="+fmt.Sprint(replicas))
	Eventually(func(g Gomega) {
		out, err := run(
			"kubectl", "get", "deploy", "crossplane", "-n", crossplaneNamespace,
			"-o", "jsonpath={.status.readyReplicas}",
		)
		g.Expect(err).NotTo(HaveOccurred())
		out = strings.TrimSpace(out)
		if replicas == 0 {
			g.Expect(out == "" || out == "0").To(BeTrue(), out)
			return
		}
		g.Expect(out).To(Equal(fmt.Sprint(replicas)))
	}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
}

func setControllerEnvironment(env string) {
	raw := mustGet("deploy", controllerDeploy, "-n", namespace)
	var dep struct {
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct {
						Args []string `json:"args"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}
	Expect(json.Unmarshal([]byte(raw), &dep)).To(Succeed())
	Expect(dep.Spec.Template.Spec.Containers).NotTo(BeEmpty())
	args := dep.Spec.Template.Spec.Containers[0].Args
	next := make([]string, 0, len(args)+1)
	for _, arg := range args {
		if strings.HasPrefix(arg, "--environment=") {
			continue
		}
		next = append(next, arg)
	}
	next = append(next, "--environment="+env)
	if strings.Join(args, "\n") == strings.Join(next, "\n") {
		return
	}
	encoded, err := json.Marshal(next)
	Expect(err).NotTo(HaveOccurred())
	runOrFail("kubectl", "patch", "deploy", controllerDeploy, "-n", namespace, "--type=json",
		"-p", fmt.Sprintf(`[{"op":"replace","path":"/spec/template/spec/containers/0/args","value":%s}]`, encoded))
	runOrFail("kubectl", "rollout", "status", "deploy/"+controllerDeploy, "-n", namespace, "--timeout=180s")
	waitControllerReady()
}

func restartController() {
	runOrFail("kubectl", "delete", "pod", "-n", namespace, "-l", "control-plane=controller-manager", "--wait=true")
	waitControllerReady()
}

func waitControllerReady() {
	Eventually(func(g Gomega) {
		raw := mustGet("pods", "-n", namespace, "-l", "control-plane=controller-manager")
		var list struct {
			Items []struct {
				Metadata struct {
					DeletionTimestamp *string `json:"deletionTimestamp"`
				} `json:"metadata"`
				Status struct {
					Phase string `json:"phase"`
				} `json:"status"`
			} `json:"items"`
		}
		g.Expect(json.Unmarshal([]byte(raw), &list)).To(Succeed())
		ready := false
		for _, item := range list.Items {
			if item.Metadata.DeletionTimestamp == nil && item.Status.Phase == "Running" {
				ready = true
			}
		}
		g.Expect(ready).To(BeTrue(), raw)
	}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
}

func deleteRevision(name string) {
	_, _ = run("kubectl", "delete", revisionResource, name, "--wait=false")
	_, _ = run("kubectl", "patch", revisionResource, name, "--type=merge", "-p", `{"metadata":{"finalizers":[]}}`)
	Eventually(func(g Gomega) {
		_, err := run("kubectl", "get", revisionResource, name)
		g.Expect(err).To(HaveOccurred())
	}).WithTimeout(time.Minute).WithPolling(2 * time.Second).Should(Succeed())
}

func createPinnedXR(f seededFleet, name, rev string) {
	xr := xrSpec{name: name}
	doc := xrYAML(f, xr)
	doc = strings.Replace(doc, "compositionRef:\n      name: "+f.composition+"\n",
		"compositionRef:\n      name: "+f.composition+"\n    compositionRevisionRef:\n      name: "+rev+"\n", 1)
	path := writeTemp(doc)
	defer func() { _ = os.Remove(path) }()
	// Field manager crossplane matches the owner the controller is allowed to
	// take. A client-side apply would conflict as kubectl-client-side-apply.
	runOrFail("kubectl", "apply", "--server-side", "--field-manager=crossplane", "--force-conflicts", "-f", path)
	setXRConditions(f.ns, name, "True")
}

func dumpScenarioFailure() {
	if activeFleet == nil {
		return
	}
	_, _ = fmt.Fprintf(GinkgoWriter, "controller logs:\n")
	out, _ := run("kubectl", "logs", "-n", namespace, "-l", "control-plane=controller-manager", "--tail=200")
	_, _ = fmt.Fprintf(GinkgoWriter, "%s\n", out)
	out, _ = run("kubectl", "get", rolloutResource, "-l", scenarioLabel+"="+activeFleet.scenario, "-o", "yaml")
	_, _ = fmt.Fprintf(GinkgoWriter, "rollouts:\n%s\n", out)
	out, _ = run("kubectl", "get", xrResource, "-n", activeFleet.ns, "-o", "yaml")
	_, _ = fmt.Fprintf(GinkgoWriter, "xrs:\n%s\n", out)
}

func selectedKeys(st rolloutsv1alpha1.CompositionRolloutState) []string {
	out := make([]string, 0, len(st.Canary.Selected))
	for _, ref := range st.Canary.Selected {
		out = append(out, ref.Namespace+"/"+ref.Name)
	}
	sort.Strings(out)
	return out
}

func xrKeys(ns string, names ...string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = ns + "/" + name
	}
	sort.Strings(out)
	return out
}

func pauseReason(st rolloutsv1alpha1.CompositionRolloutState) string {
	if st.PauseReason == nil {
		return ""
	}
	return st.PauseReason.Reason
}

func findState(
	cr rolloutsv1alpha1.CompositionRollout,
	composition string,
) (rolloutsv1alpha1.CompositionRolloutState, bool) {
	for i := range cr.Status.Rollouts {
		if cr.Status.Rollouts[i].Composition == composition {
			return cr.Status.Rollouts[i], true
		}
	}
	return rolloutsv1alpha1.CompositionRolloutState{}, false
}

func getRollout(name string) (rolloutsv1alpha1.CompositionRollout, error) {
	out, err := run("kubectl", "get", rolloutResource, name, "-o", "json")
	if err != nil {
		return rolloutsv1alpha1.CompositionRollout{}, err
	}
	var cr rolloutsv1alpha1.CompositionRollout
	if err := json.Unmarshal([]byte(out), &cr); err != nil {
		return cr, err
	}
	return cr, nil
}

func getXR(ns, name string) ([]byte, error) {
	out, err := run("kubectl", "get", xrResource, name, "-n", ns, "-o", "json")
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

func mustXR(ns, name string) []byte {
	raw, err := getXR(ns, name)
	Expect(err).NotTo(HaveOccurred())
	return raw
}

func mustGet(args ...string) string {
	cmd := append([]string{"get"}, args...)
	if !strings.Contains(strings.Join(args, " "), "-o") {
		cmd = append(cmd, "-o", "json")
	}
	return kubectl(cmd...)
}

func revisionRef(raw []byte) string {
	var obj map[string]any
	Expect(json.Unmarshal(raw, &obj)).To(Succeed())
	spec, _ := obj["spec"].(map[string]any)
	cp, _ := spec["crossplane"].(map[string]any)
	ref, _ := cp["compositionRevisionRef"].(map[string]any)
	name, _ := ref["name"].(string)
	return name
}

func canonicalSpec(raw []byte) string {
	return string(mustJSON(specMap(raw)))
}

func specWithoutRevisionName(raw []byte) string {
	spec := specMap(raw)
	if cp, ok := spec["crossplane"].(map[string]any); ok {
		if ref, ok := cp["compositionRevisionRef"].(map[string]any); ok {
			delete(ref, "name")
		}
	}
	return string(mustJSON(spec))
}

func specMap(raw []byte) map[string]any {
	var obj map[string]any
	Expect(json.Unmarshal(raw, &obj)).To(Succeed())
	spec, _ := obj["spec"].(map[string]any)
	if spec == nil {
		return map[string]any{}
	}
	return spec
}

func labelsOf(raw []byte) map[string]string {
	var obj struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	}
	Expect(json.Unmarshal(raw, &obj)).To(Succeed())
	if obj.Metadata.Labels == nil {
		return map[string]string{}
	}
	return obj.Metadata.Labels
}

func controllerFieldPaths(raw []byte) []string {
	var obj struct {
		Metadata struct {
			ManagedFields []struct {
				Manager  string         `json:"manager"`
				FieldsV1 map[string]any `json:"fieldsV1"`
			} `json:"managedFields"`
		} `json:"metadata"`
	}
	Expect(json.Unmarshal(raw, &obj)).To(Succeed())
	var paths []string
	for _, field := range obj.Metadata.ManagedFields {
		if field.Manager != controllerFieldManager {
			continue
		}
		walkFields(field.FieldsV1, "", &paths)
	}
	sort.Strings(paths)
	return paths
}

func walkFields(v any, prefix string, out *[]string) {
	children, ok := v.(map[string]any)
	if !ok {
		if prefix != "" {
			*out = append(*out, prefix)
		}
		return
	}
	found := false
	for key, child := range children {
		if !strings.HasPrefix(key, "f:") {
			continue
		}
		found = true
		name := strings.TrimPrefix(key, "f:")
		next := name
		if prefix != "" {
			next = prefix + "." + name
		}
		walkFields(child, next, out)
	}
	if !found && prefix != "" {
		*out = append(*out, prefix)
	}
}

func conditionTrue(raw, condType string) bool {
	var obj struct {
		Status struct {
			Conditions []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
			} `json:"conditions"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return false
	}
	for _, cond := range obj.Status.Conditions {
		if cond.Type == condType && cond.Status == "True" {
			return true
		}
	}
	return false
}

func policyOf(raw []byte) string {
	spec := specMap(raw)
	cp, _ := spec["crossplane"].(map[string]any)
	policy, _ := cp["compositionUpdatePolicy"].(string)
	return policy
}

func compositionYAML(name, generation string) string {
	return fmt.Sprintf(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: %s
  labels:
    %s: %s
spec:
  compositeTypeRef:
    apiVersion: %s/%s
    kind: %s
  mode: Pipeline
  pipeline:
  - step: render
    functionRef:
      name: function-patch-and-transform
    input:
      apiVersion: pt.fn.crossplane.io/v1beta1
      kind: Resources
      resources:
      - name: config
        base:
          apiVersion: v1
          kind: ConfigMap
          data:
            generation: %q
        patches:
        - type: FromCompositeFieldPath
          fromFieldPath: metadata.name
          toFieldPath: metadata.name
        readinessChecks:
        - type: None
`, name, scenarioLabel, name, xrGroup, xrVersion, xrKind, generation)
}

func xrYAML(f seededFleet, xr xrSpec) string {
	labels := fmt.Sprintf("    %s: %s\n", scenarioLabel, f.scenario)
	if xr.canary {
		labels += fmt.Sprintf("    %s: %q\n", canaryLabel, canaryLabelValue)
	}
	return fmt.Sprintf(`apiVersion: %s/%s
kind: %s
metadata:
  name: %s
  namespace: %s
  labels:
%s
spec:
  marker: keep-%s
  crossplane:
    compositionUpdatePolicy: Manual
    compositionRef:
      name: %s
`, xrGroup, xrVersion, xrKind, xr.name, f.ns, labels, xr.name, f.composition)
}

func xrdYAML() string {
	return fmt.Sprintf(`apiVersion: apiextensions.crossplane.io/v2
kind: CompositeResourceDefinition
metadata:
  name: %s
spec:
  scope: Namespaced
  group: %s
  names:
    kind: %s
    plural: xnops
  versions:
  - name: %s
    served: true
    referenceable: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            properties:
              marker:
                type: string
`, xrResource, xrGroup, xrKind, xrVersion)
}

func functionYAML() string {
	return fmt.Sprintf(`apiVersion: pkg.crossplane.io/v1
kind: Function
metadata:
  name: function-patch-and-transform
spec:
  package: %s
`, functionPackage)
}

func xrRBACYAML() string {
	return fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: xr-rollout-e2e-xnops
rules:
- apiGroups: ["%s"]
  resources: ["xnops"]
  verbs: ["get", "list", "watch", "patch", "update"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: xr-rollout-e2e-xnops
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: xr-rollout-e2e-xnops
subjects:
- kind: ServiceAccount
  name: %s
  namespace: %s
`, xrGroup, serviceAccountName, namespace)
}

func applyYAML(doc string) {
	path := writeTemp(doc)
	defer func() { _ = os.Remove(path) }()
	runOrFail("kubectl", "apply", "-f", path)
}

func writeTemp(doc string) string {
	f, err := os.CreateTemp("", "xre2e-*.yaml")
	Expect(err).NotTo(HaveOccurred())
	_, err = f.WriteString(doc)
	Expect(err).NotTo(HaveOccurred())
	Expect(f.Close()).To(Succeed())
	return f.Name()
}

func resourceExists(kind, name string) bool {
	_, err := run("kubectl", "get", kind, name)
	return err == nil
}

func ensureHelm() string {
	if path, err := exec.LookPath("helm"); err == nil {
		return path
	}
	return downloadHelm(runtime.GOARCH)
}

func downloadHelm(arch string) string {
	url := fmt.Sprintf("https://get.helm.sh/helm-%s-linux-%s.tar.gz", helmReleaseVersion, arch)
	resp, err := http.Get(url) //nolint:gosec // pinned Helm release archive
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = resp.Body.Close() }()
	Expect(resp.StatusCode).To(Equal(http.StatusOK))
	gz, err := gzip.NewReader(resp.Body)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	dest := filepath.Join(os.TempDir(), "e2e-helm-"+helmReleaseVersion+"-"+arch)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		Expect(err).NotTo(HaveOccurred())
		if filepath.Base(hdr.Name) != "helm" || hdr.Typeflag != tar.TypeReg {
			continue
		}
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		Expect(err).NotTo(HaveOccurred())
		_, err = io.Copy(f, tr)
		Expect(err).NotTo(HaveOccurred())
		Expect(f.Close()).To(Succeed())
		return dest
	}
	Fail(fmt.Sprintf("helm binary missing from %s", url))
	return ""
}

func makeInRepo(target string, extra ...string) {
	root, err := utils.GetProjectDir()
	Expect(err).NotTo(HaveOccurred())
	cmd := exec.Command("make", append([]string{target}, extra...)...)
	cmd.Dir = root
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	Expect(cmd.Run()).To(Succeed())
}

func kubectl(args ...string) string {
	out, err := run("kubectl", args...)
	Expect(err).NotTo(HaveOccurred())
	return out
}

func runOrFail(bin string, args ...string) {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	Expect(cmd.Run()).To(Succeed())
}

func run(bin string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%s %s: %w\n%s", bin, strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String(), nil
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	return raw
}
