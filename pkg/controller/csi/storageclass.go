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
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// syncStorageClasses creates or updates all StorageClasses needed by CephRBD and CephFS.
func (r *ReconcileCSI) syncStorageClasses(csiDeploy *csiv1.CSI) (bool, error) {
	storageClasses := &storagev1.StorageClassList{}
	err := r.listObjects(storageClasses, &client.ListOptions{
		LabelSelector: ownerLabelSelector(csiDeploy),
	})
	if err != nil {
		return false, fmt.Errorf("list StorageClasses failed: %s", err.Error())
	}
	existSCSet := make(map[string]*storagev1.StorageClass, len(storageClasses.Items))
	for i := range storageClasses.Items {
		sc := &storageClasses.Items[i]
		existSCSet[sc.Name] = sc
	}

	var (
		updated bool
		errs    types.ErrorList
	)

	for _, sc := range csiDeploy.Spec.StorageClasses {
		exist := existSCSet[sc.Name]
		delete(existSCSet, sc.Name)
		if scUpdated, err := r.syncStorageClass(exist, sc, csiDeploy); err != nil {
			errs = append(errs, err)
		} else if scUpdated {
			updated = true
		}
	}

	// Remaining StorageClasses are no longer needed by this CSI, delete them.
	for _, sc := range existSCSet {
		if err := r.deleteObject(sc); err != nil {
			errs = append(errs, err)
		} else {
			updated = true
		}
	}

	if len(errs) != 0 {
		return updated, errs
	}
	return updated, nil
}

// syncStorageClass creates or updates a single StorageClass.
func (r *ReconcileCSI) syncStorageClass(
	exist *storagev1.StorageClass,
	sc storagev1.StorageClass,
	csiDeploy *csiv1.CSI) (bool, error) {
	// StorageClass objects cannot be claimed by the GC Controller, so we need to
	// add owner related labels so that we can delete them manually.
	addOwnerLabels(&sc.ObjectMeta, csiDeploy)
	sc.Provisioner = csiDeploy.Spec.DriverName

	if exist != nil {
		// StorageClass already exists, update it if necessary.
		updateObj := sc.DeepCopy()
		updateObj.TypeMeta = exist.TypeMeta
		updateObj.ObjectMeta = exist.ObjectMeta
		filterSCDefaultFields(updateObj, exist)
		updated := !equality.Semantic.DeepEqual(updateObj, exist)
		if mergeObjectMeta(&sc.ObjectMeta, &updateObj.ObjectMeta) {
			updated = true
		}
		if !updated {
			return false, nil
		}
		// Only update the SotrageClass object if needed.
		klog.Infof("StorageClass %s of %s/%s is changed, delete it first",
			sc.Name, csiDeploy.Namespace, csiDeploy.Name)
		if err := r.deleteObject(exist); err != nil {
			return false, fmt.Errorf("delete old StorageClass %s of %s/%s failed: %v",
				sc.Name, csiDeploy.Namespace, csiDeploy.Name, err)
		}
	}

	klog.Infof("Create StorageClass %s for %s/%s",
		sc.Name, csiDeploy.Namespace, csiDeploy.Name)
	return true, r.createObject(&sc)
}

// clearStorageClasses deletes all StorageClasses owned by a specified CSI.
func (r *ReconcileCSI) clearStorageClasses(csiDeploy *csiv1.CSI) error {
	// List the StorageClasses by owner label.
	list := &storagev1.StorageClassList{}
	err := r.listObjects(list, &client.ListOptions{
		LabelSelector: ownerLabelSelector(csiDeploy),
	})
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("list StorageClasses for %s/%s failed: %s",
				csiDeploy.Namespace, csiDeploy.Name, err.Error())
		}
		return nil
	}

	var errs types.ErrorList
	for _, sc := range list.Items {
		if err := r.deleteObject(&sc); err != nil && !errors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("delete StorageClass %s failed: %s", sc.Name, err.Error()))
		}
		klog.V(4).Infof("StorageClass %s deleted", sc.Name)
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Clear unconcerned fields when comparing two StorageClass object.
func filterSCDefaultFields(sc, ref *storagev1.StorageClass) {
	if sc.ReclaimPolicy == nil &&
		ref.ReclaimPolicy != nil &&
		*ref.ReclaimPolicy == corev1.PersistentVolumeReclaimDelete {
		sc.ReclaimPolicy = ref.ReclaimPolicy
	}
	if sc.VolumeBindingMode == nil &&
		ref.VolumeBindingMode != nil &&
		*ref.VolumeBindingMode == storagev1.VolumeBindingImmediate {
		sc.VolumeBindingMode = ref.VolumeBindingMode
	}
}
