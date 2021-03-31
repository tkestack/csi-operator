/*
 * Tencent is pleased to support the open source community by making TKEStack available.
 *
 * Copyright (C) 2012-2021 Tencent. All Rights Reserved.
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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	csiv1 "tkestack.io/csi-operator/pkg/apis/storage/v1"
	"tkestack.io/csi-operator/pkg/types"
)

// syncConfigMaps updates all configMaps.
func (r *ReconcileCSI) syncConfigMaps(csiDeploy *csiv1.CSI) (bool, error) {
	configMaps := &corev1.ConfigMapList{}
	err := r.listObjects(configMaps, &client.ListOptions{LabelSelector: ownerLabelSelector(csiDeploy)})
	if err != nil {
		return false, fmt.Errorf("list ConfigMaps failed: %s", err.Error())
	}
	configMapSets := make(map[string]*corev1.ConfigMap, len(configMaps.Items))
	for i := range configMaps.Items {
		configMap := &configMaps.Items[i]
		configMapSets[k8stypes.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}.String()] = configMap
	}

	var (
		updated bool
		errs    types.ErrorList
	)

	for _, configMap := range csiDeploy.Spec.ConfigMaps {
		key := k8stypes.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}.String()
		exist := configMapSets[key]
		delete(configMapSets, key)
		if configMapUpdated, err := r.syncConfigMap(exist, configMap, csiDeploy); err != nil {
			errs = append(errs, err)
		} else if configMapUpdated {
			updated = true
		}
	}

	// Remaining configMaps are no longer needed by this CSI, delete them.
	for _, configMap := range configMapSets {
		if err := r.deleteObject(configMap); err != nil {
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

// syncConfigMap updates a configMap.
func (r *ReconcileCSI) syncConfigMap(
	exist *corev1.ConfigMap,
	configMap corev1.ConfigMap,
	csiDeploy *csiv1.CSI) (bool, error) {
	addOwnerLabels(&configMap.ObjectMeta, csiDeploy)
	addOwnerReference(&configMap.ObjectMeta, csiDeploy)

	if exist == nil {
		klog.Infof("Create ConfigMap %s/%s for %s/%s",
			configMap.Namespace, configMap.Name, csiDeploy.Namespace, csiDeploy.Name)
		return true, r.createObject(&configMap)
	}

	// ConfigMap already exists, update it if necessary.
	updateObj := configMap.DeepCopy()
	updateObj.TypeMeta = exist.TypeMeta
	updateObj.ObjectMeta = exist.ObjectMeta
	updated := !equality.Semantic.DeepEqual(updateObj, exist)
	if mergeObjectMeta(&configMap.ObjectMeta, &updateObj.ObjectMeta) {
		updated = true
	}
	if updated {
		klog.Infof("Update ConfigMap %s/%s for %s/%s",
			configMap.Namespace, configMap.Name, csiDeploy.Namespace, csiDeploy.Name)
		return true, r.updateObject(updateObj)
	}

	return false, nil
}
