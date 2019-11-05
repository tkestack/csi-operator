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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"tkestack.io/csi-operator/pkg/types"
)

// syncSecrets updates all secrets.
func (r *ReconcileCSI) syncSecrets(csiDeploy *csiv1.CSI) (bool, error) {
	secrets := &corev1.SecretList{}
	err := r.listObjects(secrets, &client.ListOptions{LabelSelector: ownerLabelSelector(csiDeploy)})
	if err != nil {
		return false, fmt.Errorf("list Secrets failed: %s", err.Error())
	}
	secretSets := make(map[string]*corev1.Secret, len(secrets.Items))
	for i := range secrets.Items {
		secret := &secrets.Items[i]
		secretSets[k8stypes.NamespacedName{Namespace: secret.Namespace, Name: secret.Name}.String()] = secret
	}

	var (
		updated bool
		errs    types.ErrorList
	)

	for _, secret := range csiDeploy.Spec.Secrets {
		key := k8stypes.NamespacedName{Namespace: secret.Namespace, Name: secret.Name}.String()
		exist := secretSets[key]
		delete(secretSets, key)
		if secretUpdated, err := r.syncSecret(exist, secret, csiDeploy); err != nil {
			errs = append(errs, err)
		} else if secretUpdated {
			updated = true
		}
	}

	// Remaining secrets are no longer needed by this CSI, delete them.
	for _, secret := range secretSets {
		if err := r.deleteObject(secret); err != nil {
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

// syncSecret updates a secret.
func (r *ReconcileCSI) syncSecret(
	exist *corev1.Secret,
	secret corev1.Secret,
	csiDeploy *csiv1.CSI) (bool, error) {
	addOwnerLabels(&secret.ObjectMeta, csiDeploy)
	addOwnerReference(&secret.ObjectMeta, csiDeploy)

	if exist == nil {
		klog.Infof("Create Secret %s/%s for %s/%s",
			secret.Namespace, secret.Name, csiDeploy.Namespace, csiDeploy.Name)
		return true, r.createObject(&secret)
	}

	// Secret already exists, update it if necessary.
	updateObj := secret.DeepCopy()
	updateObj.TypeMeta = exist.TypeMeta
	updateObj.ObjectMeta = exist.ObjectMeta
	filterSecretDefaultFields(updateObj, exist)
	updated := !equality.Semantic.DeepEqual(updateObj, exist)
	if mergeObjectMeta(&secret.ObjectMeta, &updateObj.ObjectMeta) {
		updated = true
	}
	if updated {
		klog.Infof("Update Secret %s/%s for %s/%s",
			secret.Namespace, secret.Name, csiDeploy.Namespace, csiDeploy.Name)
		return true, r.updateObject(updateObj)
	}

	return false, nil
}

// filterSecretDefaultFields filters fields filled by k8s.
func filterSecretDefaultFields(secret, ref *corev1.Secret) {
	if secret.Type == "" && ref.Type == corev1.SecretTypeOpaque {
		secret.Type = ref.Type
	}
}
