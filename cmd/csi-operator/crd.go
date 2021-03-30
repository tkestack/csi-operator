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

package main

import (
	"fmt"

	"tkestack.io/csi-operator/pkg/apis/storage"

	extensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
)

var schema = &extensionsv1beta1.JSONSchemaProps{
	Properties: map[string]extensionsv1beta1.JSONSchemaProps{
		"apiVersion": {Type: "string"},
		"kind":       {Type: "string"},
		"metadata":   {Type: "object"},
		"spec": {
			Type: "object",
			Properties: map[string]extensionsv1beta1.JSONSchemaProps{
				"controller":     {Type: "object"},
				"driverName":     {Type: "string"},
				"driverVersion":  {Type: "string"},
				"node":           {Type: "object"},
				"secrets":        {Type: "array"},
				"storageClasses": {Type: "array"},
				"version":        {Type: "string"},
			},
			Required: []string{"driverName"},
		},
	},
}

var csiCRD = &extensionsv1beta1.CustomResourceDefinition{
	ObjectMeta: metav1.ObjectMeta{
		Name: "csis." + storage.GroupName,
	},
	TypeMeta: metav1.TypeMeta{
		Kind:       "CustomResourceDefinition",
		APIVersion: "apiextensions.k8s.io/v1beta1",
	},
	Spec: extensionsv1beta1.CustomResourceDefinitionSpec{
		Group: storage.GroupName,
		Scope: extensionsv1beta1.ResourceScope("Namespaced"),
		Names: extensionsv1beta1.CustomResourceDefinitionNames{
			Plural:   "csis",
			Singular: "csi",
			Kind:     "CSI",
			ListKind: "CSIList",
		},
		Version: "v1",
		Versions: []extensionsv1beta1.CustomResourceDefinitionVersion{
			{
				Name:    "v1",
				Served:  true,
				Storage: true,
			},
		},
		Validation: &extensionsv1beta1.CustomResourceValidation{
			OpenAPIV3Schema: schema,
		},
		Subresources: &extensionsv1beta1.CustomResourceSubresources{
			Status: &extensionsv1beta1.CustomResourceSubresourceStatus{},
		},
	},
}

// syncCRD creates and updates the CRD object.
func syncCRD(config *rest.Config) error {
	client, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create apiextensions client failed: %v", err)
	}
	crdClient := client.ApiextensionsV1beta1().CustomResourceDefinitions()

	oldCRD, err := crdClient.Get(csiCRD.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("get crd failed: %v", err)
		}
		if _, createErr := crdClient.Create(csiCRD); createErr != nil {
			return fmt.Errorf("create crd failed: %v", createErr)
		}
		klog.Info("CRD created")
		return nil
	}

	// Update the crd if needed.
	if equality.Semantic.DeepEqual(oldCRD.Spec, csiCRD.Spec) {
		klog.Info("CRD is already created, no need to update it")
		return nil
	}

	klog.Info("Try to update crd")
	newCRD := oldCRD.DeepCopy()
	newCRD.Spec = csiCRD.Spec
	_, err = crdClient.Update(newCRD)
	if err == nil {
		klog.Info("CRD updated")
	}

	return err
}
