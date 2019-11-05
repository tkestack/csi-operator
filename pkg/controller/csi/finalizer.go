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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog"
)

const csiDeploymentFinalizer = "storage.tke.cloud.tencent.com"

// Add a finalizer to CSI object to avoid unsafe deletion.
func (r *ReconcileCSI) addFinalizer(csiDeploy *csiv1.CSI) error {
	if hasFinalizer(csiDeploy.Finalizers, csiDeploymentFinalizer) {
		return nil
	}
	csiDeploy.Finalizers = append(csiDeploy.Finalizers, csiDeploymentFinalizer)
	return r.updateObject(csiDeploy)
}

// Clear the finalizer only if all children objects deleted.
func (r *ReconcileCSI) clearFinalizer(csiDeploy *csiv1.CSI) error {
	newCSIDeploy := csiDeploy.DeepCopy()
	newCSIDeploy.Finalizers = []string{}
	for _, f := range csiDeploy.Finalizers {
		if f == csiDeploymentFinalizer {
			continue
		}
		newCSIDeploy.Finalizers = append(newCSIDeploy.Finalizers, f)
	}

	err := r.updateObject(newCSIDeploy)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("clear csiDeploymentFinalizer of %s/%s failed: %s",
			csiDeploy.Namespace, csiDeploy.Name, err.Error())
	}

	klog.V(4).Infof("Finalizer of %s/%s cleared", csiDeploy.Namespace, csiDeploy.Name)

	return nil
}

// hasFinalizer returns true if a CSI object container specified finalizer.
func hasFinalizer(finalizers []string, finalizer string) bool {
	for _, f := range finalizers {
		if f == finalizer {
			return true
		}
	}
	return false
}
