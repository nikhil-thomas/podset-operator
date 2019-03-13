package podset

import (
	"context"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"k8s.io/apimachinery/pkg/labels"

	appv1alpha1 "github.com/nikhil-thomas/podset-operator/pkg/apis/app/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_podset")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new PodSet Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcilePodSet{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("podset-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource PodSet
	err = c.Watch(&source.Kind{Type: &appv1alpha1.PodSet{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner PodSet
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &appv1alpha1.PodSet{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcilePodSet{}

// ReconcilePodSet reconciles a PodSet object
type ReconcilePodSet struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a PodSet object and makes changes based on the state read
// and what is in the PodSet.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcilePodSet) Reconcile(request reconcile.Request) (reconcile.Result, error) {

	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling PodSet")

	podSet := &appv1alpha1.PodSet{}
	err := r.client.Get(context.TODO(), request.NamespacedName, podSet)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// list all pods owned by this PodSet instance
	lbls := labels.Set{
		"app":     podSet.Name,
		"version": "v0.1",
	}

	existingPods := &corev1.PodList{}
	err = r.client.List(context.TODO(),
		&client.ListOptions{
			Namespace:     request.Namespace,
			LabelSelector: labels.SelectorFromSet(lbls),
		},
		existingPods,
	)

	if err != nil {
		reqLogger.Error(err, "failed to list existing pods in the podset")
		return reconcile.Result{}, err
	}
	existingPodNames := []string{}
	for _, pod := range existingPods.Items {
		if pod.GetObjectMeta().GetDeletionTimestamp() != nil {
			continue
		}
		if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning {
			existingPodNames = append(existingPodNames, pod.GetObjectMeta().GetName())
		}
	}

	reqLogger.Info("Checking podset", "expected replicas", podSet.Spec.Replicas, "Pod names", existingPodNames)
	status := appv1alpha1.PodSetStatus{
		PodNames: existingPodNames,
	}

	if !reflect.DeepEqual(podSet.Status, status) {
		podSet.Status = status
		err := r.client.Update(context.TODO(), podSet)
		if err != nil {
			reqLogger.Error(err, "failed to update the podSet")
			return reconcile.Result{}, err
		}
	}

	// scale down pods
	if int32(len(existingPodNames)) > podSet.Spec.Replicas {
		reqLogger.Info("Deleting a pod in the podset", "expected replicas", podSet.Spec.Replicas, "Pod.Names", existingPodNames)
		pod := existingPods.Items[0]
		err = r.client.Delete(context.TODO(), &pod)
		if err != nil {
			reqLogger.Error(err, "failed to delete a pod")
			return reconcile.Result{}, err
		}
	}

	//scale up pods
	if int32(len(existingPodNames)) < podSet.Spec.Replicas {
		reqLogger.Info("Adding a pod in the podset", "expected replicas", podSet.Spec.Replicas, "Pod.Names", existingPodNames)
		pod := newPodForCR(podSet)
		if err := controllerutil.SetControllerReference(podSet, pod, r.scheme); err != nil {
			reqLogger.Error(err, "unable to set owner reference on new pod")
			return reconcile.Result{}, err
		}
		err = r.client.Create(context.TODO(), pod)
		if err != nil {
			reqLogger.Error(err, "failed to create a pod")
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{Requeue: true}, nil

	// reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	// reqLogger.Info("Reconciling PodSet")

	// // Fetch the PodSet instance
	// instance := &appv1alpha1.PodSet{}
	// err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	// if err != nil {
	// 	if errors.IsNotFound(err) {
	// 		// Request object not found, could have been deleted after reconcile request.
	// 		// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
	// 		// Return and don't requeue
	// 		return reconcile.Result{}, nil
	// 	}
	// 	// Error reading the object - requeue the request.
	// 	return reconcile.Result{}, err
	// }

	// // Define a new Pod object
	// pod := newPodForCR(instance)

	// // Set PodSet instance as the owner and controller
	// if err := controllerutil.SetControllerReference(instance, pod, r.scheme); err != nil {
	// 	return reconcile.Result{}, err
	// }

	// // Check if this Pod already exists
	// found := &corev1.Pod{}
	// err = r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, found)
	// if err != nil && errors.IsNotFound(err) {
	// 	reqLogger.Info("Creating a new Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
	// 	err = r.client.Create(context.TODO(), pod)
	// 	if err != nil {
	// 		return reconcile.Result{}, err
	// 	}

	// 	// Pod created successfully - don't requeue
	// 	return reconcile.Result{}, nil
	// } else if err != nil {
	// 	return reconcile.Result{}, err
	// }

	// // Pod already exists - don't requeue
	// reqLogger.Info("Skip reconcile: Pod already exists", "Pod.Namespace", found.Namespace, "Pod.Name", found.Name)
	// return reconcile.Result{}, nil
}

// newPodForCR returns a busybox pod with the same name/namespace as the cr
func newPodForCR(cr *appv1alpha1.PodSet) *corev1.Pod {
	labels := map[string]string{
		"app":     cr.Name,
		"version": "v0.1",
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			//Name: cr.Name + "-pod",
			GenerateName: cr.Name + "-pod",
			Namespace:    cr.Namespace,
			Labels:       labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "busybox",
					Image:   "busybox",
					Command: []string{"sleep", "3600"},
				},
			},
		},
	}
}
