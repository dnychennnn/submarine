/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	clientset "submarine-cloud-v2/pkg/generated/clientset/versioned"
	submarinescheme "submarine-cloud-v2/pkg/generated/clientset/versioned/scheme"
	"submarine-cloud-v2/pkg/helm"
	informers "submarine-cloud-v2/pkg/generated/informers/externalversions/submarine/v1alpha1"
	listers "submarine-cloud-v2/pkg/generated/listers/submarine/v1alpha1"
	"time"
)

const controllerAgentName = "submarine-controller"

// Controller is the controller implementation for Foo resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// sampleclientset is a clientset for our own API group
	submarineclientset clientset.Interface

	submarinesLister listers.SubmarineLister
	submarinesSynced cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

// NewController returns a new sample controller
func NewController(
	kubeclientset kubernetes.Interface,
	submarineclientset clientset.Interface,
	deploymentInformer appsinformers.DeploymentInformer,
	submarineInformer informers.SubmarineInformer) *Controller {

	// TODO: Create event broadcaster
	// Add Submarine types to the default Kubernetes Scheme so Events can be
	// logged for Submarine types.
	utilruntime.Must(submarinescheme.AddToScheme(scheme.Scheme))
	klog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	// Initialize controller
	controller := &Controller{
		kubeclientset:      kubeclientset,
		submarineclientset: submarineclientset,
		submarinesLister:   submarineInformer.Lister(),
		submarinesSynced:   submarineInformer.Informer().HasSynced,
		workqueue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Submarines"),
		recorder:           recorder,
	}

	// Setting up event handler for Submarine
	klog.Info("Setting up event handlers")
	submarineInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueSubmarine,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueSubmarine(new)
		},
	})

	// TODO: Setting up event handler for other resources. E.g. namespace

	return controller
}

func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	klog.Info("Starting Submarine controller")

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.submarinesSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	// Example: HelmInstall (can be removed in the future):
	// This is equal to:
	// 		helm repo add k8s-as-helm https://ameijer.github.io/k8s-as-helm/
	// .	helm repo update
	//  	helm install helm-install-example-release k8s-as-helm/svc --set ports[0].protocol=TCP,ports[0].port=80,ports[0].targetPort=9376
	// Useful Links:
	//   (1) https://github.com/PrasadG193/helm-clientgo-example
	// . (2) https://github.com/ameijer/k8s-as-helm/tree/master/charts/svc
	klog.Info("[Helm example] Install")
	helmActionConfig := helm.HelmInstall(
		"https://ameijer.github.io/k8s-as-helm/",
		"k8s-as-helm",
		"svc",
		"helm-install-example-release",
		"default",
		map[string]string {
			"set": "ports[0].protocol=TCP,ports[0].port=80,ports[0].targetPort=9376",
		},
	)

	klog.Info("[Helm example] Sleep 60 seconds")
	time.Sleep(time.Duration(60) * time.Second)

	klog.Info("[Helm example] Uninstall")
	helm.HelmUninstall("helm-install-example-release", helmActionConfig)



	klog.Info("Starting workers")
	// Launch two workers to process Submarine resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// TODO: Maintain workqueue
		defer c.workqueue.Done(obj)
		key, _ := obj.(string)
		c.syncHandler(key)
		c.workqueue.Forget(obj)
		klog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Foo resource
// with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	// TODO: business logic

	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Invalid resource key: %s", key))
		return nil
	}

	// Get the Submarine resource with this namespace/name
	submarine, err := c.submarinesLister.Submarines(namespace).Get(name)
	if err != nil {
		// The Submarine resource may no longer exist, in which case we stop
		// processing
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("submarine '%s' in work queue no longer exists", key))
			return nil
		}
	}

	klog.Info("syncHandler: ", key)

	// Print out the spec of the Submarine resource
	b, err := json.MarshalIndent(submarine.Spec, "", "  ")
	fmt.Println(string(b))

	return nil
}

// enqueueFoo takes a Submarine resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than Submarine.
func (c *Controller) enqueueSubmarine(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	// key: [namespace]/[CR name]
	// Example: default/example-submarine
	c.workqueue.Add(key)
}
