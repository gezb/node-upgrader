package controllers

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gezbv1 "gezb/node-upgrader/api/v1"
)

var _ = Describe("NodeDrainer", func() {

	var r *NodeDrainReconciler
	var testNs *corev1.Namespace
	var ndActive, ndInactive, ndActiveNoDrain, ndInactiveNoDrain *gezbv1.NodeDrain
	var clObjs []client.Object

	reconcileNodeDrain := func(nodeDrain *gezbv1.NodeDrain) {
		// Mock request to simulate Reconcile() being called on an event for a watched resource .
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      nodeDrain.ObjectMeta.Name,
				Namespace: nodeDrain.ObjectMeta.Namespace,
			},
		}
		r.Reconcile(context.Background(), req)
	}

	checkSuccessfulReconcile := func(nodeDrain *gezbv1.NodeDrain, phase gezbv1.MaintenancePhase) *gezbv1.NodeDrain {
		drain := &gezbv1.NodeDrain{}
		err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(nodeDrain), drain)
		Expect(err).NotTo(HaveOccurred())
		Expect(drain.Status.Phase).To(Equal(phase))
		return drain

	}

	checkFailedReconcile := func(nodeDrain *gezbv1.NodeDrain, phase gezbv1.MaintenancePhase, phaseReason gezbv1.PhaseReason) *gezbv1.NodeDrain {
		drain := &gezbv1.NodeDrain{}
		err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(nodeDrain), drain)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(drain.Status.LastError)).NotTo(Equal(0))
		Expect(drain.Status.Phase).To(Equal(phase))
		Expect(drain.Status.PhaseReason).To(Equal(phaseReason))
		return drain
	}

	BeforeEach(func() {

		startTestEnv()

		// Create a ReconcileNodeMaintenance object with the scheme and fake client
		r = &NodeDrainReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
			logger: ctrl.Log.WithName("unit test"),
		}
		initDrainer(r, cfg)

		// in test pods are not evicted, so don't wait forever for them
		r.drainer.SkipWaitForDeleteTimeoutSeconds = 0

		var objs []client.Object
		ndActive = &gezbv1.NodeDrain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "node-maintanance",
				Namespace: "test",
			},
			Spec: gezbv1.NodeDrainSpec{
				Active:             true,
				NodeName:           "node01",
				ExpectedK8sVersion: "1.19.8",
				Drain:              true,
			}}
		ndInactive = &gezbv1.NodeDrain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "node-maintanance-inactive",
				Namespace: "test",
			},
			Spec: gezbv1.NodeDrainSpec{
				Active:             false,
				NodeName:           "node01",
				ExpectedK8sVersion: "1.19.8",
				Drain:              true,
			}}
		ndActiveNoDrain = &gezbv1.NodeDrain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "node-maintanance-no-drain",
				Namespace: "test",
			},
			Spec: gezbv1.NodeDrainSpec{
				Active:             true,
				NodeName:           "node01",
				ExpectedK8sVersion: "1.19.8",
				Drain:              false,
			}}
		ndInactiveNoDrain = &gezbv1.NodeDrain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "node-maintanance-inactive-no-drain",
				Namespace: "test",
			},
			Spec: gezbv1.NodeDrainSpec{
				Active:             false,
				NodeName:           "node01",
				ExpectedK8sVersion: "1.19.8",
				Drain:              false,
			}}

		// create test ns on 1st run
		testNs = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
		}
		if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(testNs), &corev1.Namespace{}); err != nil {
			err := k8sClient.Create(context.Background(), testNs)
			Expect(err).ToNot(HaveOccurred())
		}

		objs = getTestObjects()
		clObjs = append(objs, ndActive, ndInactive, ndActiveNoDrain, ndInactiveNoDrain)

		for _, o := range clObjs {
			err := k8sClient.Create(context.Background(), o)
			Expect(err).ToNot(HaveOccurred())
		}
	})

	AfterEach(func() {
		if err := k8sClient.Delete(context.Background(), testNs); err != nil {
			Expect(err).ToNot(HaveOccurred())
		}

	})

	Context("Node drain controller initialization test", func() {

		It("Node drain should be initialized properly", func() {
			r.initNodeDrainerStatus(ndActive, "node01")
			drain := &gezbv1.NodeDrain{}
			err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(ndActive), drain)
			Expect(err).NotTo(HaveOccurred())
			Expect(drain.Status.Phase).To(Equal(gezbv1.MaintenanceRunning))
			Expect(len(drain.Status.PendingPods)).To(Equal(2))
			Expect(drain.Status.EvictionPods).To(Equal(2))
			Expect(drain.Status.TotalPods).To(Equal(2))
		})

		It("Node drain should be initialized properly with drain not set", func() {
			r.initNodeDrainerStatus(ndActiveNoDrain, "node01")
			drain := &gezbv1.NodeDrain{}
			err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(ndActiveNoDrain), drain)
			Expect(err).NotTo(HaveOccurred())
			Expect(drain.Status.Phase).To(Equal(gezbv1.MaintenanceRunning))
			Expect(len(drain.Status.PendingPods)).To(Equal(0))
			Expect(drain.Status.EvictionPods).To(Equal(0))
			Expect(drain.Status.TotalPods).To(Equal(0))
		})

		It("owner ref should be set properly", func() {
			r.initNodeDrainerStatus(ndActive, "node01")
			drain := &gezbv1.NodeDrain{}
			err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(ndActive), drain)
			Expect(err).NotTo(HaveOccurred())
			node := &corev1.Node{}
			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: "node01"}, node)
			Expect(err).ToNot(HaveOccurred())
			r.setOwnerRefToNode(drain, node)
			Expect(len(drain.ObjectMeta.GetOwnerReferences())).To(Equal(1))
			ref := drain.ObjectMeta.GetOwnerReferences()[0]
			Expect(ref.Name).To(Equal(node.ObjectMeta.Name))
			Expect(ref.UID).To(Equal(node.ObjectMeta.UID))
			Expect(ref.APIVersion).To(Equal(node.TypeMeta.APIVersion))
			Expect(ref.Kind).To(Equal(node.TypeMeta.Kind))
			r.setOwnerRefToNode(drain, node)
			Expect(len(drain.ObjectMeta.GetOwnerReferences())).To(Equal(1))
		})

		It("Should not init Node Drain if already set", func() {
			ndActiveCopy := ndActive.DeepCopy()
			ndActiveCopy.Status.Phase = gezbv1.MaintenanceRunning
			r.initNodeDrainerStatus(ndActiveCopy, "node01")
			nodeDrain := &gezbv1.NodeDrain{}
			err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(ndActive), nodeDrain)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodeDrain.Status.Phase).NotTo(Equal(gezbv1.MaintenanceRunning))
			Expect(len(nodeDrain.Status.PendingPods)).NotTo(Equal(2))
			Expect(nodeDrain.Status.EvictionPods).NotTo(Equal(2))
			Expect(nodeDrain.Status.TotalPods).NotTo(Equal(2))
		})
	})

	Context("Node drain controller initialization dry-run test", func() {

		It("Node drain should be initialized in dry-run", func() {
			r.initNodeDrainerStatus(ndInactive, "node01")
			drain := &gezbv1.NodeDrain{}
			err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(ndInactive), drain)
			Expect(err).NotTo(HaveOccurred())
			Expect(drain.Status.Phase).To(Equal(gezbv1.MaintenanceDryRun))
			Expect(len(drain.Status.PendingPods)).To(Equal(2))
			Expect(drain.Status.EvictionPods).To(Equal(2))
			Expect(drain.Status.TotalPods).To(Equal(2))
		})

		It("Node drain should be initialized properly with drain not set", func() {
			r.initNodeDrainerStatus(ndInactiveNoDrain, "node01")
			drain := &gezbv1.NodeDrain{}
			err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(ndInactiveNoDrain), drain)
			Expect(err).NotTo(HaveOccurred())
			Expect(drain.Status.Phase).To(Equal(gezbv1.MaintenanceDryRun))
			Expect(len(drain.Status.PendingPods)).To(Equal(0))
			Expect(drain.Status.EvictionPods).To(Equal(0))
			Expect(drain.Status.TotalPods).To(Equal(0))
		})

		It("Should not init Node Drain if already set - dry-run", func() {
			ndInactiveCopy := ndInactive.DeepCopy()
			ndInactiveCopy.Status.Phase = gezbv1.MaintenanceDryRun
			r.initNodeDrainerStatus(ndInactiveCopy, "node01")
			nodeDrain := &gezbv1.NodeDrain{}
			err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(ndActive), nodeDrain)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodeDrain.Status.Phase).NotTo(Equal(gezbv1.MaintenanceDryRun))
			Expect(len(nodeDrain.Status.PendingPods)).NotTo(Equal(2))
			Expect(nodeDrain.Status.EvictionPods).NotTo(Equal(2))
			Expect(nodeDrain.Status.TotalPods).NotTo(Equal(2))
		})

	})

	Context("Node Drain controller reconciles a NodeDrain CR for a node in the cluster", func() {

		It("should reconcile once without failing", func() {
			reconcileNodeDrain(ndActive)
			checkSuccessfulReconcile(ndActive, gezbv1.MaintenanceSucceeded)
		})

		It("should reconcile and cordon node", func() {
			reconcileNodeDrain(ndActive)
			checkSuccessfulReconcile(ndActive, gezbv1.MaintenanceSucceeded)
			node := &corev1.Node{}
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: ndActive.Spec.NodeName}, node)
			Expect(err).NotTo(HaveOccurred())
			Expect(node.Spec.Unschedulable).To(Equal(true))
		})

		It("should fail on non existing node", func() {
			ndFail := &gezbv1.NodeDrain{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-maintanance",
					Namespace: "test",
				},
				Spec: gezbv1.NodeDrainSpec{
					Active:             true,
					NodeName:           "non-existing",
					ExpectedK8sVersion: "1.19.8",
					Drain:              false,
				},
			}
			err := k8sClient.Delete(context.TODO(), ndActive)
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), ndFail)
			Expect(err).NotTo(HaveOccurred())
			reconcileNodeDrain(ndFail)
			checkFailedReconcile(ndFail, gezbv1.MaintenanceFailed, gezbv1.FailureReasonNodeNotFound)
		})

	})

	Context("Node Drain controller reconciles a In-active NodeDrain CR for a node in the cluster", func() {

		It("should reconcile once without failing", func() {
			reconcileNodeDrain(ndInactive)
			checkSuccessfulReconcile(ndInactive, gezbv1.MaintenanceDryRun)
		})

		It("should reconcile and not cordon node", func() {
			reconcileNodeDrain(ndInactive)
			checkSuccessfulReconcile(ndInactive, gezbv1.MaintenanceDryRun)
			node := &corev1.Node{}
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: ndActive.Spec.NodeName}, node)
			Expect(err).NotTo(HaveOccurred())
			Expect(node.Spec.Unschedulable).To(Equal(false))
		})

		It("should fail on non existing node", func() {
			ndFail := &gezbv1.NodeDrain{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-maintanance",
					Namespace: "test",
				},
				Spec: gezbv1.NodeDrainSpec{
					Active:             true,
					NodeName:           "non-existing",
					ExpectedK8sVersion: "1.19.8",
					Drain:              false,
				},
			}
			ndFail.Spec.NodeName = ""
			err := k8sClient.Delete(context.TODO(), ndActive)
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), ndFail)
			Expect(err).NotTo(HaveOccurred())
			reconcileNodeDrain(ndFail)
			checkFailedReconcile(ndFail, gezbv1.MaintenanceFailed, gezbv1.FailureReasonNodeNotFound)
		})

	})

	Context("Node Drain controller reconciles a In-active NodeDrain CR for a node in the cluster", func() {

		It("should reconcile once without failing", func() {
			reconcileNodeDrain(ndInactive)
			checkSuccessfulReconcile(ndInactive, gezbv1.MaintenanceDryRun)
		})

		It("should reconcile and not cordon node", func() {
			reconcileNodeDrain(ndInactive)
			checkSuccessfulReconcile(ndInactive, gezbv1.MaintenanceDryRun)
			node := &corev1.Node{}
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: ndActive.Spec.NodeName}, node)
			Expect(err).NotTo(HaveOccurred())
			Expect(node.Spec.Unschedulable).To(Equal(false))
		})

		It("should fail on non existing node", func() {
			ndFail := &gezbv1.NodeDrain{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-maintanance",
					Namespace: "test",
				},
				Spec: gezbv1.NodeDrainSpec{
					Active:             true,
					NodeName:           "non-existing",
					ExpectedK8sVersion: "1.19.8",
					Drain:              false,
				},
			}
			ndFail.Spec.NodeName = ""
			err := k8sClient.Delete(context.TODO(), ndActive)
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), ndFail)
			Expect(err).NotTo(HaveOccurred())
			reconcileNodeDrain(ndFail)
			checkFailedReconcile(ndFail, gezbv1.MaintenanceFailed, gezbv1.FailureReasonNodeNotFound)
		})

	})

	Context("Node Drain controller reconciles NodeDrain for the wrong node version  CR for a node in the cluster", func() {

		It("should reconcile an active CR and and reports InvalidNodeVesion", func() {
			ndWrongVersion := &gezbv1.NodeDrain{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-maintanance",
					Namespace: "test",
				},
				Spec: gezbv1.NodeDrainSpec{
					Active:             true,
					NodeName:           "node01",
					ExpectedK8sVersion: "1.13.0",
					Drain:              false,
				},
			}
			err := k8sClient.Delete(context.TODO(), ndActive)
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), ndWrongVersion)
			Expect(err).NotTo(HaveOccurred())
			reconcileNodeDrain(ndWrongVersion)
			checkFailedReconcile(ndWrongVersion, gezbv1.MaintenanceInvalid, gezbv1.FailureReasonInvalidNodeVersion)
		})

		It("should reconcile an inactive CR and reports InvalidNodeVesion", func() {
			ndWrongVersion := &gezbv1.NodeDrain{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-maintanance",
					Namespace: "test",
				},
				Spec: gezbv1.NodeDrainSpec{
					Active:             false,
					NodeName:           "node01",
					ExpectedK8sVersion: "1.13.0",
					Drain:              false,
				},
			}
			err := k8sClient.Delete(context.TODO(), ndActive)
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), ndWrongVersion)
			Expect(err).NotTo(HaveOccurred())
			reconcileNodeDrain(ndWrongVersion)
			checkFailedReconcile(ndWrongVersion, gezbv1.MaintenanceInvalid, gezbv1.FailureReasonInvalidNodeVersion)
		})

	})

	Context("Node Drain Controller uncordens node on removal of CR", func() {

		It("should reconcile a deleting active CR should uncordens node", func() {
			ndUncorden := &gezbv1.NodeDrain{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "node-maintanance",
					Namespace:  "test",
					Finalizers: []string{gezbv1.Finalizer},
				},
				Spec: gezbv1.NodeDrainSpec{
					Active:             true,
					NodeName:           "node02",
					ExpectedK8sVersion: "1.19.8",
					Drain:              false,
				},
			}
			err := k8sClient.Delete(context.TODO(), ndActive)
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), ndUncorden)
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), ndUncorden)
			Expect(err).NotTo(HaveOccurred())
			reconcileNodeDrain(ndUncorden)
			// node should now be not be unschedulable
			node := &corev1.Node{}
			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: ndUncorden.Spec.NodeName}, node)
			Expect(err).NotTo(HaveOccurred())
			Expect(node.Spec.Unschedulable).To(Equal(false))
			// the CRD should have been deleted
			err = k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(ndUncorden), ndUncorden)
			Expect(err).Error().To(HaveOccurred())
			Expect(k8sErrors.IsNotFound(err)).To(BeTrue())
		})

		It("should reconcile a deleting inactive CR should not uncorden node", func() {
			ndUncorden := &gezbv1.NodeDrain{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "node-maintanance",
					Namespace:  "test",
					Finalizers: []string{gezbv1.Finalizer},
				},
				Spec: gezbv1.NodeDrainSpec{
					Active:             false,
					NodeName:           "node02",
					ExpectedK8sVersion: "1.19.8",
					Drain:              false,
				},
			}
			err := k8sClient.Delete(context.TODO(), ndActive)
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), ndUncorden)
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), ndUncorden)
			Expect(err).NotTo(HaveOccurred())
			reconcileNodeDrain(ndUncorden)
			// node should still be unschedulable
			node := &corev1.Node{}
			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: ndUncorden.Spec.NodeName}, node)
			Expect(err).NotTo(HaveOccurred())
			Expect(node.Spec.Unschedulable).To(Equal(true))
			// the CRD should have been deleted
			err = k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(ndUncorden), ndUncorden)
			Expect(err).Error().To(HaveOccurred())
			Expect(k8sErrors.IsNotFound(err)).To(BeTrue())
		})
	})
})

func getTestObjects() []client.Object {

	return []client.Object{
		&corev1.Node{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Node",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "node01",
			},
			Status: corev1.NodeStatus{
				NodeInfo: corev1.NodeSystemInfo{
					KubeletVersion: "1.19.8"},
			},
		},
		&corev1.Node{TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Node",
		},
			ObjectMeta: metav1.ObjectMeta{
				Name: "node02",
			},
			Spec: corev1.NodeSpec{
				Unschedulable: true,
			},
			Status: corev1.NodeStatus{
				NodeInfo: corev1.NodeSystemInfo{
					KubeletVersion: "1.19.8"},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test",
				Name:      "test-pod-1",
			},
			Spec: corev1.PodSpec{
				NodeName: "node01",
				Containers: []corev1.Container{
					{
						Name:  "c1",
						Image: "i1",
					},
				},
				TerminationGracePeriodSeconds: pointer.Int64Ptr(0),
			},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test",
				Name:      "test-pod-2",
			},
			Spec: corev1.PodSpec{
				NodeName: "node01",
				Containers: []corev1.Container{
					{
						Name:  "c1",
						Image: "i1",
					},
				},
				TerminationGracePeriodSeconds: pointer.Int64Ptr(0),
			},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		},
	}
}
