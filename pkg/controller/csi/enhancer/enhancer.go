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

package enhancer

import (
	"fmt"
	"reflect"
	"strings"

	csiv1 "tkestack.io/csi-operator/pkg/apis/storage/v1"
	"tkestack.io/csi-operator/pkg/config"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
)

var (
	criticalComponents = sets.NewString("Provisioner", "Attacher", "Snapshotter", "Resizer")
	controllerResource = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("100Mi"),
		},
	}
	cephRBDLivenessProbePorts    = livenessProbePorts{Node: "9809", Controller: "9808"}
	cephFSLivenessProbePorts     = livenessProbePorts{Node: "9819", Controller: "9818"}
	tencentCBSLivenessProbePorts = livenessProbePorts{Node: "9829", Controller: "9828"}
)

// livenessProbePorts is the set of livenessProbe ports of CSI components.
type livenessProbePorts struct {
	Node       string
	Controller string
}

// csiVersion is the set of versions of all CSI components.
type csiVersion struct {
	Provisioner      string
	Attacher         string
	Resizer          string
	Snapshotter      string
	LivenessProbe    string
	NodeRegistrar    string
	ClusterRegistrar string
	Driver           string
}

var csiVersionMap = map[string]map[csiv1.CSIVersion]*csiVersion{
	csiv1.CSIDriverCephRBD: {
		csiv1.CSIVersionV0: {
			Provisioner:   "csi-provisioner:v0.4.2",
			Attacher:      "csi-attacher:v0.4.2",
			Snapshotter:   "csi-snapshotter:v0.4.1",
			LivenessProbe: "livenessprobe:v0.4.1",
			NodeRegistrar: "driver-registrar:v0.3.0",
			Driver:        "rbdplugin:v0.3.0",
		},
		csiv1.CSIVersionV1: {
			Provisioner:   "csi-provisioner:v1.0.1",
			Attacher:      "csi-attacher:v1.1.0",
			Snapshotter:   "csi-snapshotter:v1.1.0",
			LivenessProbe: "livenessprobe:v1.1.0",
			NodeRegistrar: "csi-node-driver-registrar:v1.1.0",
			Driver:        "rbdplugin:v1.0.0",
			// TODO: Add resizer.
			// Resizer:          "v0.1.0",
		},
	},
	csiv1.CSIDriverCephFS: {
		csiv1.CSIVersionV0: {
			Provisioner:   "csi-provisioner:v0.4.2",
			Attacher:      "csi-attacher:v0.4.2",
			LivenessProbe: "livenessprobe:v0.4.1",
			NodeRegistrar: "driver-registrar:v0.3.0",
			Driver:        "cephfsplugin:v0.3.0",
		},
		csiv1.CSIVersionV1: {
			Provisioner:   "csi-provisioner:v1.0.1",
			Attacher:      "csi-attacher:v1.1.0",
			LivenessProbe: "livenessprobe:v1.1.0",
			NodeRegistrar: "csi-node-driver-registrar:v1.1.0",
			Driver:        "cephfsplugin:v1.0.0",
			// TODO: Add resizer.
			// Resizer:          "v0.1.0",
		},
	},
	csiv1.CSIDriverTencentCBS: {
		csiv1.CSIVersionV0: {
			Provisioner:   "csi-provisioner:v0.4.2",
			Attacher:      "csi-attacher:v0.4.2",
			NodeRegistrar: "driver-registrar:v0.3.0",
			Driver:        "csi-tencentcloud-cbs:v0.2.1",
		},
		csiv1.CSIVersionV1: {
			Provisioner:   "csi-provisioner:v1.6.0",
			Attacher:      "csi-attacher:v1.1.0",
			Snapshotter:   "csi-snapshotter:v1.2.2",
			NodeRegistrar: "csi-node-driver-registrar:v1.1.0",
			Driver:        "csi-tencentcloud-cbs:v1.2.0",
			Resizer:       "csi-resizer:v0.5.0",
		},
		csiv1.CSIVersionV1p1: {
			Provisioner:   "csi-provisioner:v1.6.0",
			Attacher:      "csi-attacher:v1.1.0",
			Snapshotter:   "csi-snapshotter:v1.2.2",
			NodeRegistrar: "csi-node-driver-registrar:v1.1.0",
			Driver:        "csi-tencentcloud-cbs:f9b4997",
			Resizer:       "csi-resizer:v0.5.0",
		},
	},
}

// New creates a Enhancer.
func New(config *config.Config) Enhancer {
	cephEnhancer := newCephEnhancer(config)
	tencentCloudEnhancer := newTencentCloudEnhancer(config)
	return &enhancer{
		enhancers: map[string]Enhancer{
			csiv1.CSIDriverCephRBD:    cephEnhancer,
			csiv1.CSIDriverCephFS:     cephEnhancer,
			csiv1.CSIDriverTencentCBS: tencentCloudEnhancer,
		},
	}
}

// Enhancer is helper used to enhance a well known CSI type.
type Enhancer interface {
	// Enhance enhances a well known CSI type.
	Enhance(csiDeploy *csiv1.CSI) error
}

// enhancer is the implement of Enhancer.
type enhancer struct {
	enhancers map[string]Enhancer
}

// Enhance enhances a well known CSI type.
func (e *enhancer) Enhance(csiDeploy *csiv1.CSI) error {
	enhancer, exist := e.enhancers[csiDeploy.Spec.DriverName]
	if !exist {
		return fmt.Errorf("unknown storage type: %s", csiDeploy.Spec.DriverName)
	}
	return enhancer.Enhance(csiDeploy)
}

// getImage generates a complete image address based on the domain name, image
// name, and tag of the image registry.
func getImage(domain string, name string) string {
	if strings.HasSuffix(domain, "/") {
		domain = strings.TrimSuffix(domain, "/")
	}
	if strings.HasPrefix(name, "/") {
		name = strings.TrimPrefix(name, "/")
	}
	return fmt.Sprintf("%s/%s", domain, name)
}

// boolPtr returns a point of bool.
func boolPtr(value bool) *bool {
	result := new(bool)
	*result = value
	return result
}

// getSecretName returns a name for secret.
func getSecretName(csiDeploy *csiv1.CSI) string {
	return csiDeploy.Spec.DriverName + "-secret"
}

// enhanceExternalComponents enhances information of each CSI components.
func enhanceExternalComponents(globalConfig *config.Config, csiDeploy *csiv1.CSI, csiVersion *csiVersion) {
	hasController := false
	typ := reflect.TypeOf(csiVersion).Elem()
	value := reflect.ValueOf(csiVersion).Elem()
	ctrlValue := reflect.ValueOf(&csiDeploy.Spec.Controller).Elem()
	nodeValue := reflect.ValueOf(&csiDeploy.Spec.Node).Elem()

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		version := value.FieldByName(field.Name).Interface().(string)

		if len(version) > 0 {
			// Component will copy to node and controller.
			component := csiv1.CSIComponent{
				Image: getImage(globalConfig.RegistryDomain, version),
			}
			if criticalComponents.Has(field.Name) {
				component.Resources = controllerResource
			}

			nodeField := nodeValue.FieldByName(field.Name)
			if nodeField.IsValid() {
				ctrlCom := component
				nodeField.Set(reflect.ValueOf(&ctrlCom))
			}

			ctrlField := ctrlValue.FieldByName(field.Name)
			if ctrlField.IsValid() {
				nodeCom := component
				ctrlField.Set(reflect.ValueOf(&nodeCom))
				hasController = true
			}
		}
	}
	if hasController {
		csiDeploy.Spec.Controller.Replicas = 1
	}
}

// getCSIVersion returns the component version.
func getCSIVersion(csiDeploy *csiv1.CSI) (*csiVersion, error) {
	csiVersionMapForType, exist := csiVersionMap[csiDeploy.Spec.DriverName]
	if !exist {
		return nil, fmt.Errorf("unknown CSI type %s", csiDeploy.Spec.DriverName)
	}

	csiVersion, exist := csiVersionMapForType[csiDeploy.Spec.Version]
	if !exist {
		return nil, fmt.Errorf("unknown CSI version %s", csiDeploy.Spec.Version)
	}

	return csiVersion, nil
}
