/*
 * Tencent is pleased to support the open source community by making TKEStack available.
 *
 * Copyright (C) 2012-2019 Tencent. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use
 * this file except in compliance with the License. You may obtain a copy of the
 * License at
 *
 * https://opensource.org/licenses/Apache-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
 * WARRANTIES OF ANY KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations under the License.
 */

package csi

import (
	"fmt"
	"k8s.io/apimachinery/pkg/api/equality"

	"tkestack.io/csi-operator/pkg/controller/csi/enhancer"

	csiv1 "tkestack.io/csi-operator/pkg/apis/storage/v1"
	"tkestack.io/csi-operator/pkg/config"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"tkestack.io/csi-operator/pkg/types"
)

// Add creates a new CSI Controller and adds it to the Manager with default RBAC.
// The Manager will set fields on the Controller and Start it when the Manager is Started.
func Add(mgr manager.Manager, cfg *config.Config) error {
	return add(mgr, newReconciler(mgr, cfg))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, cfg *config.Config) reconcile.Reconciler {
	return &ReconcileCSI{
		client:   mgr.GetClient(),
		config:   cfg,
		recorder: mgr.GetEventRecorderFor("csi-operator"),
		enhancer: enhancer.New(cfg),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("csi-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	ownerLabelHandler := newOwnerLabelHandler()
	ownerRefHandler := &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &csiv1.CSI{},
	}

	// Watch for changes to CSI
	err = c.Watch(&source.Kind{Type: &csiv1.CSI{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for Deployment which running the external controllers.
	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, ownerRefHandler)
	if err != nil {
		return err
	}

	// Watch for NodeRegistrar which running the CSI Driver on each node.
	err = c.Watch(&source.Kind{Type: &appsv1.DaemonSet{}}, ownerRefHandler)
	if err != nil {
		return err
	}

	// Watch for Secrets which used to operate the CSI volume.
	err = c.Watch(&source.Kind{Type: &corev1.Secret{}}, ownerRefHandler)
	if err != nil {
		return err
	}

	// Watch for StorageClasses which used to create the CSI volume.
	err = c.Watch(&source.Kind{Type: &storagev1.StorageClass{}}, ownerLabelHandler)
	if err != nil {
		return err
	}

	// Watch for RBAC related objects.
	err = c.Watch(&source.Kind{Type: &corev1.ServiceAccount{}}, ownerRefHandler)
	if err != nil {
		return err
	}
	err = c.Watch(&source.Kind{Type: &rbacv1.ClusterRole{}}, ownerLabelHandler)
	if err != nil {
		return err
	}
	err = c.Watch(&source.Kind{Type: &rbacv1.ClusterRoleBinding{}}, ownerLabelHandler)
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileCSI{}

// ReconcileCSI reconciles a CSI object
type ReconcileCSI struct {
	client client.Client

	config   *config.Config
	recorder record.EventRecorder

	enhancer enhancer.Enhancer
}

// Reconcile reads that state of the cluster for a CSI object and makes changes based on the state read
// and what is in the CSI.Spec
func (r *ReconcileCSI) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the CSI instance
	csiDeploy := &csiv1.CSI{}
	if err := r.getObject(request.NamespacedName, csiDeploy); err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object, record the event and requeue the request.
		r.recorder.Event(csiDeploy, corev1.EventTypeWarning, types.FetchError, err.Error())
		return reconcile.Result{}, err
	}
	klog.V(4).Infof("Start to handle %s/%s", csiDeploy.Namespace, csiDeploy.Name)

	return reconcile.Result{}, r.handle(csiDeploy)
}

// handle processes a CSI object.
func (r *ReconcileCSI) handle(csiDeploy *csiv1.CSI) error {
	var errToRecord error
	newCSIDeploy := csiDeploy.DeepCopy()

	if isTerminating(newCSIDeploy) {
		// The Object is deleting, clear sub objects.
		errToRecord = r.clearCSIDeployment(newCSIDeploy)
	} else {
		validateErr := r.validateCSIObject(newCSIDeploy)
		if len(validateErr) != 0 {
			// Not a valid CSI object, update the Status.Conditions to reflect this.
			errToRecord = validateErr.ToAggregate()
			updateCondition(newCSIDeploy, types.Validated, errToRecord.Error(), corev1.ConditionFalse)
		} else {
			errToRecord = r.syncCSI(newCSIDeploy)
		}
	}

	if errToRecord != nil {
		r.recorder.Event(csiDeploy, corev1.EventTypeWarning, types.SyncError, errToRecord.Error())
	}

	err := r.updateCSIStatus(csiDeploy, newCSIDeploy)
	if err != nil {
		klog.Errorf("Update status of %s/%s failed: %v", csiDeploy.Namespace, csiDeploy.Name, err)
	}
	return err
}

// clearCSIDeployment deletes the non-namespaced objects created by the CSI:
// - StorageClass
// - ClusterRole
// - ClusterRoleBinding
// And remove the csiDeploymentFinalizer of CSI.
func (r *ReconcileCSI) clearCSIDeployment(csiDeploy *csiv1.CSI) error {
	var errs types.ErrorList
	klog.Infof("Clear %s/%s", csiDeploy.Namespace, csiDeploy.Name)

	if err := r.clearStorageClasses(csiDeploy); err != nil {
		errs = append(errs, err)
	}

	if err := r.clearRBACObjects(csiDeploy); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errs
	}

	// Remove the csiDeploymentFinalizer only if all non-namespaced objects removed.
	return r.clearFinalizer(csiDeploy)
}

// syncCSI updates a CSI object.
func (r *ReconcileCSI) syncCSI(csiDeploy *csiv1.CSI) error {
	if csiDeploy.Status.Phase == csiv1.CSIFailed {
		klog.V(5).Infof("Skip failed CSI %s/%s", csiDeploy.Namespace, csiDeploy.Name)
		return nil
	}

	if err := r.addFinalizer(csiDeploy); err != nil {
		// Return immediately. We shouldn't create subsequent objects without the finalizer
		// as we may forget to clear some objects when deleting a CSI object.
		return fmt.Errorf("add finalizer failed: %s", err.Error())
	}

	if err := r.enhance(csiDeploy); err != nil {
		syncCSIStatus(csiDeploy, nil, nil, err)
		return err
	}

	var errs types.ErrorList

	if updated, err := r.syncRBACObjects(csiDeploy); err != nil {
		errs = append(errs, err)
	} else if updated {
		r.recorder.Event(csiDeploy, corev1.EventTypeNormal, types.RBACSynced, "RBAC resources has been synced")
	}

	if updated, err := r.syncSecrets(csiDeploy); err != nil {
		errs = append(errs, err)
	} else if updated {
		r.recorder.Event(csiDeploy, corev1.EventTypeNormal, types.SecretsSynced, "Secrets has been synced")
	}

	if updated, err := r.syncStorageClasses(csiDeploy); err != nil {
		errs = append(errs, err)
	} else if updated {
		r.recorder.Event(csiDeploy, corev1.EventTypeNormal, types.StorageClassesSynced, "StorageClasses has been synced")
	}

	var children []csiv1.Generation

	nodeDriver, updated, nodeSyncErr := r.syncNodeDriver(csiDeploy)
	if nodeSyncErr != nil {
		errs = append(errs, nodeSyncErr)
	} else {
		if nodeDriver != nil {
			children = append(children, csiv1.Generation{
				Group:          nodeDriver.GroupVersionKind().Group,
				Kind:           nodeDriver.Kind,
				Namespace:      nodeDriver.Namespace,
				Name:           nodeDriver.Name,
				LastGeneration: nodeDriver.Generation,
			})
		}
		if updated {
			r.recorder.Event(csiDeploy, corev1.EventTypeNormal, types.NodeDriverSynced, "Node Drivers has been synced")
		}
	}

	controllerDriver, updated, ctrlSyncErr := r.syncControllerDriver(csiDeploy)
	if ctrlSyncErr != nil {
		errs = append(errs, ctrlSyncErr)
	} else {
		if controllerDriver != nil {
			children = append(children, csiv1.Generation{
				Group:          controllerDriver.GroupVersionKind().Group,
				Kind:           controllerDriver.Kind,
				Namespace:      controllerDriver.Namespace,
				Name:           controllerDriver.Name,
				LastGeneration: controllerDriver.Generation,
			})
		}
		if updated {
			r.recorder.Event(csiDeploy, corev1.EventTypeNormal,
				types.ControllerDriverSynced, "Controller Drivers has been synced")
		}
	}

	var err error
	if len(errs) > 0 {
		err = errs
	}

	csiDeploy.Status.Children = children
	syncCSIStatus(csiDeploy, nodeDriver, controllerDriver, err)

	return err
}

// enhance enhances a CSI object for well known CSI.
func (r *ReconcileCSI) enhance(csiDeploy *csiv1.CSI) error {
	if csiDeploy.Spec.Version == "" {
		klog.V(3).Infof("CSI %s/%s is not a well known type", csiDeploy.Namespace, csiDeploy.Name)
		return nil
	}

	tmpCSI := csiDeploy.DeepCopy()

	if err := r.enhancer.Enhance(tmpCSI); err != nil {
		return newNoNeedRetryError(fmt.Sprintf("enhance failed: %v", err))
	}
	if equality.Semantic.DeepEqual(tmpCSI.Spec, csiDeploy.Spec) {
		klog.V(5).Infof("CSI %s/%s already enhanced", csiDeploy.Namespace, csiDeploy.Name)
		return nil
	}
	csiDeploy.Spec = tmpCSI.Spec
	klog.Infof("Enhance CSI %s/%s", csiDeploy.Namespace, csiDeploy.Name)

	return r.updateObject(csiDeploy)
}

// syncCSIStatus updates CSI's status.
func syncCSIStatus(
	csiDeploy *csiv1.CSI,
	nodeDriver *appsv1.DaemonSet,
	controller *appsv1.Deployment,
	err error) {
	csiDeploy.Status.Phase = getCSIPhase(nodeDriver, controller, err)
	syncCSIConditions(csiDeploy, nodeDriver, controller, err)
}

// syncCSIConditions updates CSI's conditions.
func syncCSIConditions(
	csiDeploy *csiv1.CSI,
	nodeDriver *appsv1.DaemonSet,
	controller *appsv1.Deployment,
	err error) {
	// Update the Sync condition.
	reason, message, status := types.Synced, "", corev1.ConditionTrue
	if err == nil {
		csiDeploy.Status.ObservedGeneration = csiDeploy.Generation
	} else {
		message, status = err.Error(), corev1.ConditionFalse
	}
	updateCondition(csiDeploy, reason, message, status)

	// Update the node Availability condition.
	reason, message, status = types.NodeAvailable, "", corev1.ConditionUnknown
	if nodeDriver != nil {
		if nodeDriver.Status.NumberUnavailable > 0 {
			message, status = fmt.Sprintf("Node driver has %d not ready replicas",
				nodeDriver.Status.NumberUnavailable), corev1.ConditionFalse
		} else {
			status = corev1.ConditionTrue
		}
	}
	updateCondition(csiDeploy, reason, message, status)

	// Update the controller Availability condition.
	reason, message, status = types.ControllerAvailable, "", corev1.ConditionUnknown
	if controller != nil {
		if controller.Status.UnavailableReplicas > 0 {
			message, status = fmt.Sprintf("Controller driver has %d not ready replicas",
				controller.Status.UnavailableReplicas), corev1.ConditionFalse
		} else {
			status = corev1.ConditionTrue
		}
	}
	updateCondition(csiDeploy, reason, message, status)
}

// getCSIPhase calculates CSI's status.
func getCSIPhase(
	nodeDriver *appsv1.DaemonSet,
	controller *appsv1.Deployment,
	err error) csiv1.CSIPhase {
	if err != nil && isNoNeedRetryError(err) {
		return csiv1.CSIFailed
	}

	// Update the node Availability condition.
	if nodeDriver != nil && nodeDriver.Status.NumberUnavailable == 0 &&
		controller != nil && controller.Status.UnavailableReplicas == 0 {
		return csiv1.CSIRunning
	}

	return csiv1.CSIPending
}
