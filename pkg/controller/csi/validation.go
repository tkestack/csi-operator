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
	"regexp"

	csiv1 "tkestack.io/csi-operator/pkg/apis/storage/v1"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

const (
	maxCSIDriverName     = 63
	csiDriverNameRexpFmt = `^[a-zA-Z0-9][-a-zA-Z0-9_.]{0,61}[a-zA-Z-0-9]$`
)

var csiDriverNameRexp = regexp.MustCompile(csiDriverNameRexpFmt)

// validateCSIObject checks whether a CSI object is valid.
func (r *ReconcileCSI) validateCSIObject(csiDeploy *csiv1.CSI) field.ErrorList {
	var errs field.ErrorList
	fieldPath := field.NewPath("spec")

	errs = append(errs, r.validateDriverName(csiDeploy.Spec.DriverName,
		fieldPath.Child("driverName"))...)
	errs = append(errs, r.validateDriverTemplate(csiDeploy.Spec.DriverTemplate,
		fieldPath.Child("nodeDriverTemplate"))...)

	return errs
}

// validateDriverName checks whether a Driver name is valid.
func (r *ReconcileCSI) validateDriverName(driverName string, fieldPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	if len(driverName) > maxCSIDriverName {
		errs = append(errs, field.TooLong(fieldPath, driverName, maxCSIDriverName))
	}

	if !csiDriverNameRexp.MatchString(driverName) {
		errs = append(errs, field.Invalid(
			fieldPath,
			driverName,
			validation.RegexError(
				"must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character",
				csiDriverNameRexpFmt,
				"csi-rbdplugin")))
	}

	return errs
}

// validateDriverTemplate checks whether the driver template is valid.
func (r *ReconcileCSI) validateDriverTemplate(
	template *csiv1.CSIDriverTemplate,
	fieldPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	if template == nil {
		return errs
	}

	// CSIDriverTemplate should contains one and only one container.
	switch len(template.Template.Spec.Containers) {
	case 0:
		errs = append(errs, field.Invalid(fieldPath, template.Template.Spec.Containers, validation.EmptyError()))
	case 1:
	default:
		errs = append(errs, field.Invalid(fieldPath,
			template.Template.Spec.Containers, "must have one and only one container"))
	}

	return errs
}
