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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

var _ = Describe("CompositionRollout Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}
		compositionrollout := &rolloutsv1alpha1.CompositionRollout{}

		BeforeEach(func() {
			if !envtestOK {
				Skip("envtest not available")
			}
			By("creating the custom resource for the Kind CompositionRollout")
			err := k8sClient.Get(ctx, typeNamespacedName, compositionrollout)
			if err != nil && errors.IsNotFound(err) {
				autoStart := true
				resource := &rolloutsv1alpha1.CompositionRollout{
					ObjectMeta: metav1.ObjectMeta{
						Name: resourceName,
					},
					Spec: rolloutsv1alpha1.CompositionRolloutSpec{
						Compositions: []rolloutsv1alpha1.CompositionTarget{{Name: "example-composition"}},
						AutoStart:    &autoStart,
						Canary: rolloutsv1alpha1.CanarySpec{
							Strategy: rolloutsv1alpha1.CanaryStrategyCount,
							Count:    int32Ptr(1),
						},
						Soak: rolloutsv1alpha1.SoakSpec{
							Duration: metav1.Duration{Duration: 0},
						},
						Promotion: rolloutsv1alpha1.PromotionSpec{
							DefaultPolicy: rolloutsv1alpha1.PromotionManualApproval,
						},
						Waves: rolloutsv1alpha1.WaveSpec{
							BatchSize: intstr.FromString("10%"),
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			if !envtestOK {
				return
			}
			resource := &rolloutsv1alpha1.CompositionRollout{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err != nil {
				return
			}

			By("Cleanup the specific resource instance CompositionRollout")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			if !envtestOK {
				Skip("envtest not available")
			}
			By("Reconciling the created resource")
			dyn, err := dynamic.NewForConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			controllerReconciler := &CompositionRolloutReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Dynamic:  dyn,
				Mapper:   k8sClient.RESTMapper(),
				Recorder: record.NewFakeRecorder(32),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

var _ = Describe("Manager startup", func() {
	It("skips a watch when the API is not installed", func() {
		missing := &CompositionRolloutReconciler{Mapper: stubMapper{err: &meta.NoKindMatchError{
			GroupKind:        compositionGVK.GroupKind(),
			SearchedVersions: []string{compositionGVK.Version},
		}}}
		Expect(missing.apiInstalled(compositionGVK)).To(BeFalse())

		present := &CompositionRolloutReconciler{Mapper: stubMapper{}}
		Expect(present.apiInstalled(compositionGVK)).To(BeTrue())

		Expect((&CompositionRolloutReconciler{}).apiInstalled(compositionGVK)).To(BeTrue())
	})

	It("starts when Crossplane CRDs are not installed", func() {
		if !envtestOK {
			Skip("envtest not available")
		}
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 k8sClient.Scheme(),
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
		})
		Expect(err).NotTo(HaveOccurred())

		dyn, err := dynamic.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect((&CompositionRolloutReconciler{
			Client:   mgr.GetClient(),
			Scheme:   mgr.GetScheme(),
			Dynamic:  dyn,
			Recorder: record.NewFakeRecorder(8),
		}).SetupWithManager(mgr)).To(Succeed())

		startCtx, stop := context.WithCancel(context.Background())
		defer stop()
		errCh := make(chan error, 1)
		go func() { errCh <- mgr.Start(startCtx) }()

		synced := make(chan bool, 1)
		go func() { synced <- mgr.GetCache().WaitForCacheSync(startCtx) }()
		select {
		case startErr := <-errCh:
			Expect(startErr).NotTo(HaveOccurred())
		case ok := <-synced:
			Expect(ok).To(BeTrue())
		case <-time.After(20 * time.Second):
			Fail("manager did not start")
		}
		stop()
		Eventually(errCh).Should(Receive(BeNil()))
	})
})

// stubMapper is a RESTMapper with a fixed RESTMapping result.
type stubMapper struct {
	meta.RESTMapper
	err error
}

func (m stubMapper) RESTMapping(schema.GroupKind, ...string) (*meta.RESTMapping, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &meta.RESTMapping{}, nil
}

func int32Ptr(v int32) *int32 {
	return &v
}
