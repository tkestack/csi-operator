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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	csiv1 "tkestack.io/csi-operator/pkg/apis/storage/v1"
)

const (
	// ownerName is name of label with name of owner CSI.
	ownerName = "storage.tke.cloud.tencent.com/owner-name"
	// ownerNamespace is name of label with namespace of owner CSI.
	ownerNamespace = "storage.tke.cloud.tencent.com/owner-namespace"
)

// newOwnerLabelHandler enqueues Requests for the owner of an object. The owner is determined based on
// labels instead of ObjectMeta.OwnerReferences, because OwnerReferences don't work for non-namespaced objects.
func newOwnerLabelHandler() handler.EventHandler {
	mapper := func(object handler.MapObject) []reconcile.Request {
		labelSet := object.Meta.GetLabels()
		name, foundName := labelSet[ownerName]
		namespace, foundNamespace := labelSet[ownerNamespace]

		if foundName && foundNamespace {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}}}
		}
		return nil
	}
	return &handler.EnqueueRequestsFromMapFunc{
		ToRequests: handler.ToRequestsFunc(mapper),
	}
}

// addOwnerLabels add labels of owner info to the CSI object.
func addOwnerLabels(meta *metav1.ObjectMeta, csiDeploy *csiv1.CSI) {
	if meta.Labels == nil {
		meta.Labels = make(map[string]string)
	}
	meta.Labels[ownerName] = csiDeploy.Name
	meta.Labels[ownerNamespace] = csiDeploy.Namespace
}

// ownerLabelSelector returns a selector to select components owned by a specific CSI.
func ownerLabelSelector(csiDeploy *csiv1.CSI) labels.Selector {
	return labels.SelectorFromSet(labels.Set{
		ownerName:      csiDeploy.Name,
		ownerNamespace: csiDeploy.Namespace,
	})
}

// ownerReference generates an OwnerReference of CSI.
func ownerReference(csiDeploy *csiv1.CSI) []metav1.OwnerReference {
	isController := true
	return []metav1.OwnerReference{
		{
			APIVersion: csiv1.SchemeGroupVersion.String(),
			// Use a const instead of csiDeploy.Kind as it maybe empty, this is odd.
			Kind:       "CSI",
			Name:       csiDeploy.Name,
			UID:        csiDeploy.UID,
			Controller: &isController,
		},
	}
}

// addOwnerReference add OwnerReference to a component.
func addOwnerReference(meta *metav1.ObjectMeta, csiDeploy *csiv1.CSI) {
	meta.OwnerReferences = append(meta.OwnerReferences, ownerReference(csiDeploy)...)
}
