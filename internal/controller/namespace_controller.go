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
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"

	"k8s.io/apimachinery/pkg/api/meta"

	"k8s.io/apimachinery/pkg/runtime"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// NamespaceReconciler reconciles a Namespace object
type NamespaceReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	DryRun      bool
	LabelFilter string
}

const (
	// DeletionAnnotationKey is the annotation key used to mark namespaces for deletion.
	DeletionAnnotationKey = "tidy-k8s.cloudsnacks.dev/deletion-time"

	// DeletionGracePeriod defines the duration to wait before deleting an annotated namespace.
	DeletionGracePeriod = 5 * time.Minute
)

// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=namespaces/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=list;watch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=list;watch
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=list;watch
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=list;watch
// +kubebuilder:rbac:groups=core,resources=pods;services;configmaps;secrets;persistentvolumeclaims,verbs=list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Namespace object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	var namespace corev1.Namespace
	if err := r.Get(ctx, req.NamespacedName, &namespace); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Log.Info("DryRun", "DryRun", r.DryRun)
	log.Log.Info("Watching for label", "label", r.LabelFilter, "namespace", namespace.Name)
	labels := namespace.GetLabels()
	if labels == nil {
		return ctrl.Result{}, nil
	}

	parts := strings.SplitN(r.LabelFilter, "=", 2)
	if len(parts) != 2 {
		return ctrl.Result{}, nil
	}
	key, value := parts[0], parts[1]

	if val, ok := labels[key]; !ok || val != value {
		log.Log.Info("Skipping namespace without watch label", "namespace", namespace.Name, "label", r.LabelFilter)
		return ctrl.Result{}, nil
	}

	if isSystemNamespace(namespace.Name) {
		log.Log.Info("Skipping reconciliation for system namespace", "namespace", namespace.Name)
		return ctrl.Result{}, nil
	}

	// Check if namespace is empty
	empty, err := isNamespaceEmpty(ctx, r.Client, namespace.Name)
	if err != nil {
		log.Log.Error(err, "Failed to check if namespace is empty")
		return ctrl.Result{}, err
	}

	if empty {
		// Check if the deletion annotation is already set
		deletionTime, annotated := namespace.Annotations[DeletionAnnotationKey]
		if !annotated {
			// Set the deletion annotation with the current timestamp
			if namespace.Annotations == nil {
				namespace.Annotations = make(map[string]string)
			}
			namespace.Annotations[DeletionAnnotationKey] = time.Now().Format(time.RFC3339)
			if err := r.Update(ctx, &namespace); err != nil {
				log.Log.Error(err, "Failed to set deletion annotation", "namespace", namespace.Name)
				return ctrl.Result{}, err
			}
			log.Log.Info("Set deletion annotation for namespace", "namespace", namespace.Name)
			// Requeue after the grace period
			return ctrl.Result{RequeueAfter: DeletionGracePeriod}, nil
		}

		// Parse the deletion annotation timestamp
		deletionTimestamp, err := time.Parse(time.RFC3339, deletionTime)
		if err != nil {
			log.Log.Error(err, "Invalid deletion annotation timestamp", "namespace", namespace.Name)
			// Remove the invalid annotation to prevent repeated errors
			delete(namespace.Annotations, DeletionAnnotationKey)
			if err := r.Update(ctx, &namespace); err != nil {
				log.Log.Error(err, "Failed to remove invalid deletion annotation", "namespace", namespace.Name)
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}

		// Check if the grace period has elapsed
		if time.Since(deletionTimestamp) >= DeletionGracePeriod {
			if r.DryRun {
				log.Log.Info("Dry Run: Would delete empty namespace", "namespace", namespace.Name)
			} else {
				log.Log.Info("Deleting empty namespace", "namespace", namespace.Name)
				err := r.Delete(ctx, &namespace, &client.DeleteOptions{})
				if err != nil {
					log.Log.Error(err, "Failed to delete namespace", "namespace", namespace.Name)
					return ctrl.Result{}, err
				}
				log.Log.Info("Successfully deleted namespace", "namespace", namespace.Name)
			}
		} else {
			// Grace period not yet elapsed; requeue after remaining time
			remaining := DeletionGracePeriod - time.Since(deletionTimestamp)
			log.Log.Info("Grace period not yet elapsed; requeuing", "namespace", namespace.Name, "remaining", remaining)
			return ctrl.Result{RequeueAfter: remaining}, nil
		}
	} else {
		// Namespace is not empty; remove the deletion annotation if it exists
		if _, annotated := namespace.Annotations[DeletionAnnotationKey]; annotated {
			delete(namespace.Annotations, DeletionAnnotationKey)
			if err := r.Update(ctx, &namespace); err != nil {
				log.Log.Error(err, "Failed to remove deletion annotation from namespace", "namespace", namespace.Name)
				return ctrl.Result{}, err
			}
			log.Log.Info("Removed deletion annotation from namespace", "namespace", namespace.Name)
		}
		log.Log.Info("Namespace is not empty; no action taken", "namespace", namespace.Name)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}

// isSystemNamespace checks if the namespace is a system namespace
func isSystemNamespace(name string) bool {
	systemNamespaces := []string{
		"kube-system",
		"kube-public",
		"kube-node-lease",
		"default",
	}
	for _, ns := range systemNamespaces {
		if name == ns {
			return true
		}
	}
	return false
}

// isNamespaceEmpty checks if the namespace has any active resources
func isNamespaceEmpty(ctx context.Context, c client.Client, namespace string) (bool, error) {
	resourceChecks := []struct {
		list   client.ObjectList
		plural string
	}{
		{&corev1.PodList{}, "pods"},
		{&corev1.ServiceList{}, "services"},
		{&appsv1.DeploymentList{}, "deployments"},
		{&appsv1.StatefulSetList{}, "statefulsets"},
		{&appsv1.DaemonSetList{}, "daemonsets"},
		{&appsv1.ReplicaSetList{}, "replicasets"},
		{&networkingv1.IngressList{}, "ingresses"},
		{&corev1.ConfigMapList{}, "configmaps"},
		{&corev1.SecretList{}, "secrets"},
		{&corev1.PersistentVolumeClaimList{}, "persistentvolumeclaims"},
	}

	for _, rc := range resourceChecks {
		if err := c.List(ctx, rc.list, client.InNamespace(namespace)); err != nil {
			return false, err
		}
		items, err := meta.ExtractList(rc.list)
		if err != nil {
			return false, err
		}
		if len(items) > 0 {
			// Special case for services: exclude the default kubernetes service
			if rc.plural == "services" && len(items) == 1 {
				if svc, ok := items[0].(*corev1.Service); ok && svc.Name == "kubernetes" {
					continue
				}
			}
			// Special case for configmaps: exclude the default kubernetes configmap
			if rc.plural == "configmaps" && len(items) == 1 {
				if cm, ok := items[0].(*corev1.ConfigMap); ok && cm.Name == "kube-root-ca.crt" {
					continue
				}
			}
			// Namespace is not empty
			return false, nil
		}
	}

	return true, nil
}
