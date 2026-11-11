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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
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
			controllerReconciler := &CompositionRolloutReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

func int32Ptr(v int32) *int32 {
	return &v
}
