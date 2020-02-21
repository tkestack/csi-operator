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

	csiv1 "tkestack.io/csi-operator/pkg/apis/storage/v1"
	"tkestack.io/csi-operator/pkg/types"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
)

const (
	namePrefix       = "csi-"
	nodePrefix       = "node-"
	controllerPrefix = "controller-"
)

// 1. Create or update the ClusterRole object
// 2. Create or update the ServiceAccount object for Controller Driver and Node Driver
// 3. Create or update the ClusterRoleBinding object for Controller Driver and Node Driver
func (r *ReconcileCSI) syncRBACObjects(csiDeploy *csiv1.CSI) (bool, error) {
	var (
		updated bool
		errs    types.ErrorList
	)
	if rulesUpdated, err := r.syncClusterRoles(csiDeploy); err != nil {
		errs = append(errs, err)
	} else if rulesUpdated {
		updated = true
	}
	if scUpdated, err := r.syncServiceAccounts(csiDeploy); err != nil {
		errs = append(errs, err)
	} else if scUpdated {
		updated = true
	}
	if crbUpdated, err := r.syncClusterRoleBinding(csiDeploy); err != nil {
		errs = append(errs, err)
	} else if crbUpdated {
		updated = crbUpdated
	}
	if len(errs) > 0 {
		return updated, errs
	}
	return updated, nil
}

// Create or update the ClusterRole object
func (r *ReconcileCSI) syncClusterRoles(csiDeploy *csiv1.CSI) (bool, error) {
	rules := []*rbacv1.ClusterRole{generateNodeRole(csiDeploy)}
	if hasController(csiDeploy) {
		rules = append(rules, generateControllerRole(csiDeploy))
	}

	syncer := func(cr *rbacv1.ClusterRole) (bool, error) {
		exist := &rbacv1.ClusterRole{}
		err := r.getObject(k8stypes.NamespacedName{Name: cr.Name}, exist)
		if err != nil {
			if errors.IsNotFound(err) {
				klog.Infof("Create ClusterRole: %s", cr.Name)
				return true, r.createObject(cr)
			}
			return false, fmt.Errorf("check ClusterRole exist or not failed: %s", err.Error())
		}

		// ClusterRole already exists, update it if has wrong rules.
		if !equality.Semantic.DeepEqual(exist.Rules, cr.Rules) {
			// Copy the ObjectMeta and update it.
			cr.ObjectMeta = exist.ObjectMeta
			klog.Infof("Update ClusterRole: %s", cr.Name)
			return true, r.updateObject(cr)
		}

		return false, nil
	}

	var (
		updated bool
		errs    types.ErrorList
	)
	for _, cr := range rules {
		if crUpdated, err := syncer(cr); err != nil {
			errs = append(errs, err)
		} else if crUpdated {
			updated = true
		}
	}

	if len(errs) > 0 {
		return updated, errs
	}

	return updated, nil
}

// generateNodeRole generates the PolicyRules needed by Node Driver.
func generateNodeRole(csiDeploy *csiv1.CSI) *rbacv1.ClusterRole {
	// These rules are needed by both Controller Driver and Node Driver.
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"persistentvolumes"},
			Verbs:     []string{"get", "list", "watch", "update"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"nodes"},
			Verbs:     []string{"get", "list", "update"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"namespaces"},
			Verbs:     []string{"get", "list"},
		},
		{
			APIGroups: []string{"storage.k8s.io"},
			Resources: []string{"volumeattachments"},
			Verbs:     []string{"get", "list", "watch", "update"},
		},
	}

	rules = append(rules, csiDeploy.Spec.DriverTemplate.Rules...)

	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(csiDeploy, false)},
		Rules:      rules,
	}
}

// generateControllerRole generates the PolicyRules needed by Controller Driver.
func generateControllerRole(csiDeploy *csiv1.CSI) *rbacv1.ClusterRole {
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"persistentvolumes"},
			Verbs:     []string{"get", "list", "watch", "update", "create", "delete"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"events"},
			Verbs:     []string{"list", "watch", "create", "update", "patch"},
		},
		// Used for leader election.
		{
			APIGroups: []string{""},
			Resources: []string{"configmaps", "endpoints"},
			Verbs:     []string{"get", "list", "watch", "update", "create", "delete"},
		},
		{
			APIGroups: []string{"coordination.k8s.io"},
			Resources: []string{"leases"},
			Verbs:     []string{"get", "list", "watch", "update", "create", "delete"},
		},
	}
	if needSecretRule(csiDeploy) {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"secrets"},
			Verbs:     []string{"get", "list"},
		})
	}

	// Add PolicyRules needed by Provisioner.
	if csiDeploy.Spec.Controller.Provisioner != nil {
		rules = append(rules, provisionerPolicyRules()...)
	}

	// Add PolicyRules needed by Attacher.
	if csiDeploy.Spec.Controller.Attacher != nil {
		rules = append(rules, attacherPolicyRules()...)
	}

	// Add PolicyRules needed by Snapshotter.
	if csiDeploy.Spec.Controller.Snapshotter != nil {
		rules = append(rules, snapshotterPolicyRules()...)
	}

	// Add PolicyRules needed by Resizer.
	if csiDeploy.Spec.Controller.Resizer != nil {
		// If Provisioner enabled, it means we already have rules needed by Resizer.
		if csiDeploy.Spec.Controller.Provisioner == nil {
			rules = append(rules, resizerPolicyRules()...)
		}
	}

	// Add PolicyRules needed by ClusterRegister.
	if csiDeploy.Spec.Controller.ClusterRegistrar != nil {
		rules = append(rules,
			rbacv1.PolicyRule{
				APIGroups: []string{"storage.k8s.io"},
				Resources: []string{"csidrivers"},
				Verbs:     []string{"create", "delete"},
			})
	}

	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(csiDeploy, true)},
		Rules:      rules,
	}
}

// provisionerPolicyRules returns PolicyRules needed by provisioner.
func provisionerPolicyRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"persistentvolumeclaims"},
			Verbs:     []string{"get", "list", "watch", "update"},
		},
		{
			APIGroups: []string{"storage.k8s.io"},
			Resources: []string{"storageclasses"},
			Verbs:     []string{"get", "list", "watch"},
		},
	}
}

// attacherPolicyRules returns PolicyRules needed by attacher.
func attacherPolicyRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"nodes"},
			Verbs:     []string{"get", "list", "watch", "update", "patch"},
		},
		{
			APIGroups: []string{"storage.k8s.io"},
			Resources: []string{"volumeattachments"},
			Verbs:     []string{"get", "list", "watch", "update"},
		},
		{
			APIGroups: []string{"storage.k8s.io"},
			Resources: []string{"csinodes"},
			Verbs:     []string{"get", "list", "watch", "update"},
		},
	}
}

// snapshotterPolicyRules returns PolicyRules needed by snapshotter.
func snapshotterPolicyRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{APIGroups: []string{"snapshot.storage.k8s.io"},
			Resources: []string{"volumesnapshotclasses"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"snapshot.storage.k8s.io"},
			Resources: []string{"volumesnapshotcontents"},
			Verbs:     []string{"create", "get", "list", "watch", "update", "delete"},
		},
		{
			APIGroups: []string{"snapshot.storage.k8s.io"},
			Resources: []string{"volumesnapshots"},
			Verbs:     []string{"get", "list", "watch", "update"},
		},
		{
			APIGroups: []string{"apiextensions.k8s.io"},
			Resources: []string{"customresourcedefinitions"},
			Verbs:     []string{"create", "list", "watch", "delete"},
		},
	}
}

// resizerPolicyRules returns PolicyRules needed by resizer.
func resizerPolicyRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"persistentvolumeclaims"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"persistentvolumeclaims/status"},
			Verbs:     []string{"update", "patch"},
		},
		{
			APIGroups: []string{"storage.k8s.io"},
			Resources: []string{"storageclasses"},
			Verbs:     []string{"get", "list", "watch"},
		},
	}
}

// Create or update the ServiceAccount object for Controller Driver and Node Driver.
func (r *ReconcileCSI) syncServiceAccounts(csiDeploy *csiv1.CSI) (bool, error) {
	scs := []*corev1.ServiceAccount{generateServiceAccount(csiDeploy, false)}
	if hasController(csiDeploy) {
		scs = append(scs, generateServiceAccount(csiDeploy, true))
	}

	// syncer used to create or update a ServiceAccount object.
	syncer := func(sc *corev1.ServiceAccount) (bool, error) {
		exist := &corev1.ServiceAccount{}
		err := r.getObject(k8stypes.NamespacedName{Name: sc.Name, Namespace: sc.Namespace}, exist)
		if err != nil {
			if errors.IsNotFound(err) {
				klog.Infof("Create ServiceAccount for %s/%s", csiDeploy.Namespace, csiDeploy.Name)
				return true, r.createObject(sc)
			}
			return false, fmt.Errorf("check ServiceAccount exist or not failed: %s", err.Error())
		}

		// ServiceAccount already exists, update it if has wrong OwnerReferences.
		updatedObj := exist.DeepCopy()
		if mergeObjectMeta(&sc.ObjectMeta, &updatedObj.ObjectMeta) {
			return true, r.updateObject(updatedObj)
		}

		return false, nil
	}

	var (
		updated bool
		errs    types.ErrorList
	)
	for _, sc := range scs {
		if crUpdated, err := syncer(sc); err != nil {
			errs = append(errs, err)
		} else if crUpdated {
			updated = true
		}
	}

	if len(errs) > 0 {
		return updated, errs
	}

	return updated, nil
}

// generateServiceAccount generates a SA for Controller Driver or Node Driver.
func generateServiceAccount(csiDeploy *csiv1.CSI, controller bool) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            serviceAccountName(csiDeploy, controller),
			Namespace:       csiDeploy.Namespace,
			OwnerReferences: ownerReference(csiDeploy),
		},
	}
}

// Create or update the ClusterRoleBinding object
func (r *ReconcileCSI) syncClusterRoleBinding(csiDeploy *csiv1.CSI) (bool, error) {
	crbs := []*rbacv1.ClusterRoleBinding{generateClusterRoleBinding(csiDeploy, false)}
	if hasController(csiDeploy) {
		crbs = append(crbs, generateClusterRoleBinding(csiDeploy, true))
	}

	// syncer used to create or update a ClusterRoleBinding object.
	syncer := func(crb *rbacv1.ClusterRoleBinding) (bool, error) {
		exist := &rbacv1.ClusterRoleBinding{}
		err := r.getObject(k8stypes.NamespacedName{Name: crb.Name}, exist)
		if err != nil {
			if errors.IsNotFound(err) {
				klog.Infof("Create ClusterRoleBinding for %s/%s", csiDeploy.Namespace, csiDeploy.Name)
				return true, r.createObject(crb)
			}
			return false, fmt.Errorf("check ClusterRoleBinding exist or not failed: %s", err.Error())
		}

		// ServiceAccount already exists, update it if has wrong Subjects or RoleRef.
		updateObj := exist.DeepCopy()
		updated := mergeObjectMeta(&crb.ObjectMeta, &updateObj.ObjectMeta)
		if !equality.Semantic.DeepEqual(updateObj.Subjects, crb.Subjects) {
			updated = true
			updateObj.Subjects = crb.Subjects
		}
		if !equality.Semantic.DeepEqual(exist.RoleRef, crb.RoleRef) {
			updated = true
			updateObj.RoleRef = crb.RoleRef
		}
		if updated {
			return true, r.updateObject(updateObj)
		}
		return false, nil
	}

	var (
		updated bool
		errs    types.ErrorList
	)
	for _, crb := range crbs {
		if crUpdated, err := syncer(crb); err != nil {
			errs = append(errs, err)
		} else if crUpdated {
			updated = true
		}
	}

	if len(errs) > 0 {
		return updated, errs
	}

	return updated, nil
}

// generateClusterRoleBinding generates a CRB for for Controller Driver or Node Driver.
func generateClusterRoleBinding(csiDeploy *csiv1.CSI, controller bool) *rbacv1.ClusterRoleBinding {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleBindingName(csiDeploy, controller)},
		Subjects: []rbacv1.Subject{
			{
				// ServiceAccount's name/namespace are equal to CSI's name/namespace.
				Kind:      "ServiceAccount",
				Name:      serviceAccountName(csiDeploy, controller),
				Namespace: csiDeploy.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     clusterRoleName(csiDeploy, controller),
		},
	}
	addOwnerLabels(&crb.ObjectMeta, csiDeploy)
	return crb
}

// clearRBACObjects delete all RBAC objects.
func (r *ReconcileCSI) clearRBACObjects(csiDeploy *csiv1.CSI) error {
	var errs types.ErrorList
	if err := r.clearClusterRoles(csiDeploy); err != nil {
		errs = append(errs, err)
	}
	if err := r.clearClusterRoleBindings(csiDeploy); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Delete all ClusterRoles.
func (r *ReconcileCSI) clearClusterRoles(csiDeploy *csiv1.CSI) error {
	names := []string{clusterRoleName(csiDeploy, false)}
	if hasController(csiDeploy) {
		names = append(names, clusterRoleName(csiDeploy, true))
	}

	clear := func(name string) error {
		crb := &rbacv1.ClusterRole{}
		err := r.getObject(k8stypes.NamespacedName{Name: name}, crb)
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("get ClusterRole %s for %s/%s failed: %s",
				name, csiDeploy.Namespace, csiDeploy.Name, err.Error())
		}

		if err := r.deleteObject(crb); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete ClusterRole %s for %s/%s failed: %s",
				name, csiDeploy.Namespace, csiDeploy.Name, err.Error())
		}

		klog.V(4).Infof("ClusterRole %s of %s/%s deleted",
			name, csiDeploy.Namespace, csiDeploy.Name)

		return nil
	}

	var errs types.ErrorList
	for _, name := range names {
		if err := clear(name); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Delete all ClusterRoleBindings.
func (r *ReconcileCSI) clearClusterRoleBindings(csiDeploy *csiv1.CSI) error {
	names := []string{clusterRoleBindingName(csiDeploy, false)}
	if hasController(csiDeploy) {
		names = append(names, clusterRoleBindingName(csiDeploy, true))
	}

	clear := func(name string) error {
		crb := &rbacv1.ClusterRoleBinding{}
		err := r.getObject(k8stypes.NamespacedName{Name: name}, crb)
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("get ClusterRoleBing %s for %s/%s failed: %s",
				name, csiDeploy.Namespace, csiDeploy.Name, err.Error())
		}

		if err := r.deleteObject(crb); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete ClusterRoleBing %s for %s/%s failed: %s",
				name, csiDeploy.Namespace, csiDeploy.Name, err.Error())
		}

		klog.V(4).Infof("ClusterRoleBing %s of %s/%s deleted",
			name, csiDeploy.Namespace, csiDeploy.Name)

		return nil
	}

	var errs types.ErrorList
	for _, name := range names {
		if err := clear(name); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Generate a name for ClusterRoleBinding.
func clusterRoleBindingName(csiDeploy *csiv1.CSI, controller bool) string {
	tag := nodePrefix
	if controller {
		tag = controllerPrefix
	}
	return namePrefix + tag + string(csiDeploy.UID)
}

// Generate a name for ClusterRole.
func clusterRoleName(csiDeploy *csiv1.CSI, controller bool) string {
	tag := nodePrefix
	if controller {
		tag = controllerPrefix
	}
	return namePrefix + tag + string(csiDeploy.UID)
}

// // Generate a name for ServiceAccount.
func serviceAccountName(csiDeploy *csiv1.CSI, controller bool) string {
	tag := nodePrefix
	if controller {
		tag = controllerPrefix
	}
	return namePrefix + tag + csiDeploy.Name
}

// Return true if the CSI needs to operate secrets.
func needSecretRule(csiDeploy *csiv1.CSI) bool {
	controller := csiDeploy.Spec.Controller
	return controller.Provisioner != nil ||
		controller.Attacher != nil ||
		controller.Resizer != nil ||
		controller.Snapshotter != nil
}
