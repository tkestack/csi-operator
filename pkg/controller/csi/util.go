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
	"context"
	"time"

	csiv1 "tkestack.io/csi-operator/pkg/apis/storage/v1"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// TODO: Make this configurable.
	apiTimeout = time.Minute
)

// getObject returns a specific object from k8s.
func (r *ReconcileCSI) getObject(key k8stypes.NamespacedName, object runtime.Object) error {
	ctx, cancel := getContext()
	defer cancel()
	return r.client.Get(ctx, key, object)
}

// createObject creates an object to k8s.
func (r *ReconcileCSI) createObject(object runtime.Object) error {
	ctx, cancel := getContext()
	defer cancel()
	return r.client.Create(ctx, object)
}

// updateObject updates an object to k8s.
func (r *ReconcileCSI) updateObject(object runtime.Object) error {
	ctx, cancel := getContext()
	defer cancel()
	return r.client.Update(ctx, object)
}

// listObjects list objects from k8s.
func (r *ReconcileCSI) listObjects(list runtime.Object, opts *client.ListOptions) error {
	ctx, cancel := getContext()
	defer cancel()
	return r.client.List(ctx, list, opts)
}

// deleteObject deletes an object in k8s.
func (r *ReconcileCSI) deleteObject(obj runtime.Object) error {
	ctx, cancel := getContext()
	defer cancel()
	return r.client.Delete(ctx, obj)
}

// updateCSIStatus updates CSI's status.
func (r *ReconcileCSI) updateCSIStatus(oldDeploy, newDeploy *csiv1.CSI) error {
	if equality.Semantic.DeepEqual(oldDeploy.Status, newDeploy.Status) {
		return nil
	}
	ctx, cancel := getContext()
	defer cancel()
	return r.client.Status().Update(ctx, newDeploy)
}

// getContext creates a Context with a specified timeout.
func getContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), apiTimeout)
}

// isTerminating returns true if the CSI object is deleted by user.
func isTerminating(csiDeploy *csiv1.CSI) bool {
	return csiDeploy.DeletionTimestamp != nil
}

// hasSameGeneration returns true if one or more children of the CSI has the same generation of obj.
func hasSameGeneration(obj runtime.Object, gvk schema.GroupVersionKind, csiDeploy *csiv1.CSI) bool {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return false
	}
	for _, child := range csiDeploy.Status.Children {
		if child.Group == gvk.Group && child.Kind == gvk.Kind &&
			child.Name == accessor.GetName() && child.Namespace == accessor.GetNamespace() {
			return child.LastGeneration == accessor.GetGeneration()
		}
	}
	return false
}

// mergeObjectMeta merges src into dst and returns true if dst is changed.
func mergeObjectMeta(src, dst *metav1.ObjectMeta) bool {
	changed := false

	if !equality.Semantic.DeepEqual(src.OwnerReferences, dst.OwnerReferences) {
		changed = true
		dst.OwnerReferences = src.OwnerReferences
	}

	// We can't just copy labels as it maybe remove system added labels.
	if dst.Labels == nil {
		if src.Labels != nil {
			changed = true
			dst.Labels = src.Labels
		}
	} else {
		for k, v := range src.Labels {
			if dst.Labels[k] != v {
				changed = true
				dst.Labels[k] = v
			}
		}
	}

	// We can't just copy annotations as it maybe remove system added annotations.
	if dst.Annotations == nil {
		if src.Annotations != nil {
			changed = true
			dst.Annotations = src.Annotations
		}
	} else {
		for k, v := range src.Annotations {
			if dst.Annotations[k] != v {
				changed = true
				dst.Annotations[k] = v
			}
		}
	}

	return changed
}

// mergeLabels merges labels into meta.
func mergeLabels(meta *metav1.ObjectMeta, labels map[string]string) {
	if meta.Labels == nil {
		meta.Labels = labels
	} else {
		for k, v := range labels {
			meta.Labels[k] = v
		}
	}
}

// hasController returns true if need to create the Controller Driver of CSI.
func hasController(csiDeploy *csiv1.CSI) bool {
	ctrl := csiDeploy.Spec.Controller
	return ctrl.Provisioner != nil ||
		ctrl.Attacher != nil ||
		ctrl.Snapshotter != nil ||
		ctrl.Resizer != nil ||
		ctrl.ClusterRegistrar != nil ||
		ctrl.LivenessProbe != nil
}

// newNoNeedRetryError creates a noNeedRetryError.
func newNoNeedRetryError(message string) error {
	return noNeedRetryError{message}
}

// isNoNeedRetryError returns true if error is a noNeedRetryError.
func isNoNeedRetryError(err error) bool {
	_, ok := err.(noNeedRetryError)
	return ok
}

// noNeedRetryError means this error can't be fixed by retry mechanism.
type noNeedRetryError struct {
	message string
}

// Error returns error message.
func (e noNeedRetryError) Error() string {
	return e.message
}
