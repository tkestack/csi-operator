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
	csiv1 "tkestack.io/csi-operator/pkg/apis/storage/v1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// updateCondition updates a CSI object's condition.
func updateCondition(
	csiDeploy *csiv1.CSI,
	typ, message string, status corev1.ConditionStatus) {
	exist := findCondition(csiDeploy.Status.Conditions, typ)
	if exist == nil {
		csiDeploy.Status.Conditions = append(csiDeploy.Status.Conditions, generateCondition(typ, message, status))
	} else {
		exist.Message = message
		if exist.Status != status {
			exist.Status = status
			exist.LastTransitionTime = metav1.Now()
		}
	}
}

// findCondition returns a specific condition from a set of conditions.
func findCondition(conditions []csiv1.CSICondition, typ string) *csiv1.CSICondition {
	for i := range conditions {
		if conditions[i].Type == typ {
			return &conditions[i]
		}
	}
	return nil
}

// generateCondition is an utility function to create a condition.
func generateCondition(typ, message string, status corev1.ConditionStatus) csiv1.CSICondition {
	return csiv1.CSICondition{
		Type:               typ,
		Status:             status,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}
}
