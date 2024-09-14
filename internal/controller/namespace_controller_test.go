/*
Copyright 2024.

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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var _ = Describe("Namespace Controller", func() {
	Context("When reconciling a resource", func() {

		It("should successfully reconcile the resource", func() {

			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})

func TestNamespaceReconciler(t *testing.T) {
	g := NewWithT(t)

	// Setup test environment
	testEnv := &envtest.Environment{}
	cfg, err := testEnv.Start()
	g.Expect(err).NotTo(HaveOccurred())
	defer testEnv.Stop()

	// Setup scheme
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)

	// Create client
	k8sClient, err := client.New(cfg, client.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	// Create NamespaceReconciler
	reconciler := &NamespaceReconciler{
		Client:      k8sClient,
		Scheme:      s,
		DryRun:      true,
		LabelFilter: "app=true",
	}

	// Create a test namespace
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-namespace",
		},
	}
	g.Expect(k8sClient.Create(context.Background(), namespace)).To(Succeed())

	// Reconcile
	_, err = reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-namespace"},
	})
	g.Expect(err).NotTo(HaveOccurred())

	// Verify annotation is set
	updatedNs := &corev1.Namespace{}
	g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: "test-namespace"}, updatedNs)).To(Succeed())
	g.Expect(updatedNs.Annotations[DeletionAnnotationKey]).ToNot(BeEmpty())

	// Add a resource to the namespace
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx",
			Namespace: "test-namespace",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "nginx",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "nginx",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx",
						},
					},
				},
			},
		},
	}
	g.Expect(k8sClient.Create(context.Background(), deployment)).To(Succeed())

	// Reconcile again
	_, err = reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-namespace"},
	})
	g.Expect(err).NotTo(HaveOccurred())

	// Verify annotation is removed
	g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: "test-namespace"}, updatedNs)).To(Succeed())
	g.Expect(updatedNs.Annotations[DeletionAnnotationKey]).To(BeEmpty())
}

func int32Ptr(i int32) *int32 {
	return &i
}
