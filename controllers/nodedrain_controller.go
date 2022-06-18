/*
Copyright 2022.

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

package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/drain"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/hashicorp/go-version"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gezbv1 "gezb/node-upgrader/api/v1"
)

type InvalidNodeVersionError struct {
}

func (e InvalidNodeVersionError) Error() string {
	return "Error processing expectedK8sVersion"
}

// NodeDrainReconciler reconciles a NodeDrain object
type NodeDrainReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	logger  logr.Logger
	drainer *drain.Helper
}

//+kubebuilder:rbac:groups=gezb,resources=nodedrains,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gezb,resources=nodedrains/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gezb,resources=nodedrains/finalizers,verbs=update

//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;update;patch;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NodeDrain object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
func (r *NodeDrainReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	r.logger = log.FromContext(ctx)

	// Fetch the NodeDrain instance
	instance := &gezbv1.NodeDrain{}
	err := r.Client.Get(context.TODO(), req.NamespacedName, instance)
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.logger.Info("Error reading the request object, requeuing.")
		return ctrl.Result{}, err
	}
	nodeName := instance.Spec.NodeName
	version, err := version.NewSemver(instance.Spec.ExpectedK8sVersion)
	if err != nil {
		r.logger.Info(fmt.Sprintf("Failed to parse Expected Node Sematic version from `%s` - Aborting Reconcile", instance.Spec.ExpectedK8sVersion))
		return ctrl.Result{}, nil
	}
	if instance.ObjectMeta.DeletionTimestamp.IsZero() {
		if !ContainsString(instance.ObjectMeta.Finalizers, gezbv1.Finalizer) {
			instance.ObjectMeta.Finalizers = append(instance.ObjectMeta.Finalizers, gezbv1.Finalizer)
			if err := r.Client.Update(context.TODO(), instance); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if ContainsString(instance.ObjectMeta.Finalizers, gezbv1.Finalizer) || ContainsString(instance.ObjectMeta.Finalizers, metav1.FinalizerOrphanDependents) {

			// uncorden node

			node, err := r.fetchNode(nodeName, *version)
			if err != nil {
				return r.handleFetchNodeError(instance, err)
			}
			if instance.Spec.Active {
				if node.Spec.Unschedulable {
					r.logger.Info(fmt.Sprintf("Uncordening node %s", nodeName))
					err := drain.RunCordonOrUncordon(r.drainer, node, false)
					if err != nil {
						return ctrl.Result{}, err
					}
				}
			} else {
				r.logger.Info(fmt.Sprintf("Uncordening node %s - dry-run", nodeName))
			}

			// Remove our finalizer from the list and update it.
			instance.ObjectMeta.Finalizers = RemoveString(instance.ObjectMeta.Finalizers, gezbv1.Finalizer)
			if err := r.Client.Update(context.Background(), instance); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	err = r.initNodeDrainerStatus(instance, nodeName)
	if err != nil {
		r.logger.Error(err, "Failed to update NodeDrain status")
		return r.onReconcileError(instance, err)
	}

	node, err := r.fetchNode(nodeName, *version)
	if err != nil {
		return r.handleFetchNodeError(instance, err)
	}

	// Add finalizer when object is created

	if instance.Spec.Active {
		r.setOwnerRefToNode(instance, node)
		if !node.Spec.Unschedulable {
			r.logger.Info(fmt.Sprintf("Cordoning node %s", nodeName))
			err := drain.RunCordonOrUncordon(r.drainer, node, true)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		if instance.Spec.Drain {
			err = drain.RunNodeDrain(r.drainer, nodeName)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if !node.Spec.Unschedulable {
			r.logger.Info(fmt.Sprintf("Cordoning node %s - Dry-run", nodeName))
		}
		if instance.Spec.Drain {
			r.logger.Info(fmt.Sprintf("Draining  node %s - Dry-run", nodeName))
		}
	}

	instance.Status.Phase = gezbv1.MaintenanceSucceeded
	if !instance.Spec.Active {
		instance.Status.Phase = gezbv1.MaintenanceDryRun
	}
	instance.Status.PendingPods = nil
	err = r.Client.Status().Update(context.TODO(), instance)
	if err != nil {
		r.logger.Error(err, "Failed to update NodeDrain status")
		return r.onReconcileError(instance, err)
	}
	r.logger.Info("Reconcile completed", "nodeName", nodeName)

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeDrainReconciler) SetupWithManager(mgr ctrl.Manager) error {
	err := initDrainer(r, mgr.GetConfig())
	if err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&gezbv1.NodeDrain{}).
		Complete(r)
}

func (r *NodeDrainReconciler) initNodeDrainerStatus(nodeDrain *gezbv1.NodeDrain, nodeName string) error {
	// init Status field
	if nodeDrain.Status.Phase == "" || (nodeDrain.Status.Phase == gezbv1.MaintenanceDryRun && nodeDrain.Spec.Active) {
		nodeDrain.Status.NodeName = nodeName
		if !nodeDrain.Spec.Active {
			nodeDrain.Status.Phase = gezbv1.MaintenanceDryRun
		} else {
			nodeDrain.Status.Phase = gezbv1.MaintenanceRunning
		}
		if nodeDrain.Spec.Drain {
			pendingList, errorList := r.drainer.GetPodsForDeletion(nodeName)
			if errorList != nil {
				return fmt.Errorf("failed to get pods for eviction while initializing status")
			}
			if pendingList != nil {
				nodeDrain.Status.PendingPods = GetPodNameList(pendingList.Pods())
			}
			nodeDrain.Status.EvictionPods = len(nodeDrain.Status.PendingPods)

			podList, err := r.drainer.Client.CoreV1().Pods(metav1.NamespaceAll).List(
				context.Background(),
				metav1.ListOptions{
					FieldSelector: fields.SelectorFromSet(fields.Set{"spec.nodeName": nodeName}).String(),
				})
			if err != nil {
				return err
			}
			nodeDrain.Status.RunningPods = GetPodNameList(podList.Items)
			nodeDrain.Status.TotalPods = len(podList.Items)
		}

		err := r.Client.Status().Update(context.TODO(), nodeDrain)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *NodeDrainReconciler) fetchNode(nodeName string, expectedK8sVersion version.Version) (*corev1.Node, error) {
	node, err := r.drainer.Client.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil && k8sErrors.IsNotFound(err) {
		r.logger.Info(fmt.Sprintf("ERROR - Node cannot be found '%s'", nodeName))
		return nil, err
	} else if err != nil {
		r.logger.Info(fmt.Sprintf("ERROR - Failed to get node '%s", nodeName))
		return nil, err
	}
	nodeVersion, err := version.NewSemver(node.Status.NodeInfo.KubeletVersion)
	if err != nil {
		r.logger.Info(fmt.Sprintf("ERROR - Failed to Parse node KubeletVersion from '%s'- Aborting Reconcile", node.Status.NodeInfo.KubeletVersion))
		return nil, InvalidNodeVersionError{}
	}
	if !expectedK8sVersion.Core().Equal(nodeVersion) {
		r.logger.Info(fmt.Sprintf("ERROR - Node version %s not equal to expected version %s for node '%s' - Aborting Reconcile", nodeVersion.String(), expectedK8sVersion.String(), nodeName))
		return nil, InvalidNodeVersionError{}
	}
	return node, nil
}

func (r *NodeDrainReconciler) handleFetchNodeError(instance *gezbv1.NodeDrain, err error) (reconcile.Result, error) {
	switch err.(type) {
	case InvalidNodeVersionError:
		if instance.Status.Phase != gezbv1.MaintenanceInvalid {
			instance.Status.Phase = gezbv1.MaintenanceInvalid
			instance.Status.PhaseReason = gezbv1.FailureReasonInvalidNodeVersion
		}
	default:
		if instance.Status.Phase != gezbv1.MaintenanceFailed {
			instance.Status.Phase = gezbv1.MaintenanceFailed
			instance.Status.PhaseReason = gezbv1.FailureReasonNodeNotFound
		}
	}
	instance.Status.LastError = err.Error()
	err = r.Client.Status().Update(context.TODO(), instance)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func initDrainer(r *NodeDrainReconciler, config *rest.Config) error {

	r.drainer = &drain.Helper{}

	//Continue even if there are pods not managed by a ReplicationController, ReplicaSet, Job, DaemonSet or StatefulSet.
	//This is required because VirtualMachineInstance pods are not owned by a ReplicaSet or DaemonSet controller.
	//This means that the drain operation can’t guarantee that the pods being terminated on the target node will get
	//re-scheduled replacements placed else where in the cluster after the pods are evicted.
	//medik8s has its own controllers which manage the underlying VirtualMachineInstance pods.
	//Each controller behaves differently to a VirtualMachineInstance being evicted.
	r.drainer.Force = true

	//Continue even if there are pods using emptyDir (local data that will be deleted when the node is drained).
	//This is necessary for removing any pod that utilizes an emptyDir volume.
	//The VirtualMachineInstance Pod does use emptryDir volumes,
	//however the data in those volumes are ephemeral which means it is safe to delete after termination.
	r.drainer.DeleteEmptyDirData = true

	//Ignore DaemonSet-managed pods.
	//This is required because every node running a VirtualMachineInstance will also be running our helper DaemonSet called virt-handler.
	//This flag indicates that it is safe to proceed with the eviction and to just ignore DaemonSets.
	r.drainer.IgnoreAllDaemonSets = true

	//Period of time in seconds given to each pod to terminate gracefully. If negative, the default value specified in the pod will be used.
	r.drainer.GracePeriodSeconds = -1

	// TODO - add logical value or attach from the maintenance CR
	//The length of time to wait before giving up, zero means infinite
	r.drainer.Timeout = 30 * time.Second

	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	r.drainer.Client = cs
	r.drainer.DryRunStrategy = util.DryRunNone
	r.drainer.Ctx = context.Background()

	r.drainer.Out = writer{klog.Info}
	r.drainer.ErrOut = writer{klog.Error}
	r.drainer.OnPodDeletedOrEvicted = onPodDeletedOrEvicted
	return nil
}

func onPodDeletedOrEvicted(pod *corev1.Pod, usingEviction bool) {
	var verbString string
	if usingEviction {
		verbString = "Evicted"
	} else {
		verbString = "Deleted"
	}
	msg := fmt.Sprintf("pod: %s:%s %s from node: %s", pod.ObjectMeta.Namespace, pod.ObjectMeta.Name, verbString, pod.Spec.NodeName)
	klog.Info(msg)
}

func (r *NodeDrainReconciler) setOwnerRefToNode(instance *gezbv1.NodeDrain, node *corev1.Node) {

	for _, ref := range instance.ObjectMeta.GetOwnerReferences() {
		if ref.APIVersion == node.TypeMeta.APIVersion && ref.Kind == node.TypeMeta.Kind && ref.Name == node.ObjectMeta.GetName() && ref.UID == node.ObjectMeta.GetUID() {
			return
		}
	}

	r.logger.Info("setting owner ref to node")

	nodeMeta := node.TypeMeta
	ref := metav1.OwnerReference{
		APIVersion:         nodeMeta.APIVersion,
		Kind:               nodeMeta.Kind,
		Name:               node.ObjectMeta.GetName(),
		UID:                node.ObjectMeta.GetUID(),
		BlockOwnerDeletion: pointer.Bool(false),
		Controller:         pointer.Bool(false),
	}

	instance.ObjectMeta.SetOwnerReferences(append(instance.ObjectMeta.GetOwnerReferences(), ref))
}

func (r *NodeDrainReconciler) onReconcileErrorWithRequeue(nd *gezbv1.NodeDrain, err error, duration *time.Duration) (ctrl.Result, error) {
	if nd.Spec.Active {
		nd.Status.LastError = err.Error()

		if nd.Status.NodeName != "" {
			pendingList, _ := r.drainer.GetPodsForDeletion(nd.Spec.NodeName)
			if pendingList != nil {
				nd.Status.PendingPods = GetPodNameList(pendingList.Pods())
			}
		}

		updateErr := r.Client.Status().Update(context.TODO(), nd)
		if updateErr != nil {
			r.logger.Error(updateErr, "Failed to update NodeDrain with \"Failed\" status")
		}
		if duration != nil {
			r.logger.Info("Reconciling with fixed duration")
			return ctrl.Result{RequeueAfter: *duration}, nil
		}
		r.logger.Info("Reconciling with exponential duration")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, err
}

func (r *NodeDrainReconciler) onReconcileError(nd *gezbv1.NodeDrain, err error) (ctrl.Result, error) {
	return r.onReconcileErrorWithRequeue(nd, err, nil)

}

// writer implements io.Writer interface as a pass-through for klog.
type writer struct {
	logFunc func(args ...interface{})
}

// Write passes string(p) into writer's logFunc and always returns len(p)
func (w writer) Write(p []byte) (n int, err error) {
	w.logFunc(string(p))
	return len(p), nil
}
