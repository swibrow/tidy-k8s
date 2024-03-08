package main

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// ResourceController watches the Kubernetes API for changes to specific resources and deletes them based on annotations.
type ResourceController struct {
	clientset          *kubernetes.Clientset
	queue              workqueue.RateLimitingInterface
	informer           cache.SharedIndexInformer
	resourceType       runtime.Object
	resourceGVR        schema.GroupVersionResource
	deletionAnnotation string // Annotation to check for deletion logic
}

// NewResourceController creates a new ResourceController.
func NewResourceController(clientset *kubernetes.Clientset, resourceType runtime.Object, resourceGVR schema.GroupVersionResource, deletionAnnotation string) *ResourceController {
	rc := &ResourceController{
		clientset:          clientset,
		queue:              workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		resourceType:       resourceType,
		resourceGVR:        resourceGVR,
		deletionAnnotation: deletionAnnotation,
	}

	rc.informer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				return clientset.Discovery().ServerResourcesForGroupVersion(resourceGVR.GroupVersion().String())
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				return clientset.Discovery().ServerPreferredResources()
			},
		},
		resourceType,
		time.Minute*10, // Resync period
		cache.Indexers{},
	)

	rc.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    rc.handleAdd,
		UpdateFunc: rc.handleUpdate,
		DeleteFunc: rc.handleDelete,
	})

	return rc
}

// Run starts the ResourceController.
func (rc *ResourceController) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer rc.queue.ShutDown()

	go rc.informer.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, rc.informer.HasSynced) {
		utilruntime.HandleError(fmt.Errorf("Error syncing cache"))
		return
	}

	wait.Until(rc.runWorker, time.Second, stopCh)
}

func (rc *ResourceController) runWorker() {
	for rc.processNextItem() {
	}
}

func (rc *ResourceController) processNextItem() bool {
	key, quit := rc.queue.Get()
	if quit {
		return false
	}
	defer rc.queue.Done(key)

	// Process the key, e.g., parse and check for your conditions here.
	err := rc.processItem(key.(string))
	if err == nil {
		// No error, tell the queue to stop tracking history for this key.
		rc.queue.Forget(key)
	} else if rc.queue.NumRequeues(key) < 5 {
		// Retry logic.
		rc.queue.AddRateLimited(key)
	} else {
		// Too many retries.
		rc.queue.Forget(key)
		utilruntime.HandleError(err)
	}

	return true
}

func (rc *ResourceController) processItem(key string) error {
	// Implement your logic to check the annotation and delete if necessary.
	// This involves fetching the resource, checking annotations, and potentially calling delete on the resource.
	return nil
}
