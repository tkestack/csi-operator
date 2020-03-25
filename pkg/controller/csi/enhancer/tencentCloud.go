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
	"encoding/base64"
	"fmt"

	csiv1 "tkestack.io/csi-operator/pkg/apis/storage/v1"
	"tkestack.io/csi-operator/pkg/config"
	"tkestack.io/csi-operator/pkg/types"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	secretID  = "secretID"
	secretKey = "secretKey"
	vpcID     = "vpcID"
	subnetID  = "subnetID"
)

// tencentCloudInfo if a set of information of TencentCloud secrets.
type tencentCloudInfo struct {
	SecretID  string
	SecretKey string
	VpcID     string
	SubnetID  string
}

// newTencentCloudEnhancer creates a tencentCloudEnhancer.
func newTencentCloudEnhancer(config *config.Config) Enhancer {
	return &tencentCloudEnhancer{config}
}

// tencentCloudEnhancer is an Enhancer for TencentCloud storage.
type tencentCloudEnhancer struct {
	config *config.Config
}

// Enhance enhances a well known CSI type.
func (e *tencentCloudEnhancer) Enhance(csiDeploy *csiv1.CSI) error {
	switch csiDeploy.Spec.DriverName {
	case csiv1.CSIDriverTencentCBS:
		return e.enhanceTencentCBS(csiDeploy)
	case csiv1.CSIDriverTencentCFS:
		return e.enhanceTencentCFS(csiDeploy)
	}
	return fmt.Errorf("unknown type: %s", csiDeploy.Spec.DriverName)
}

// enhanceTencentCBS enhances CSI for TencentCloud CBS storage.
func (e *tencentCloudEnhancer) enhanceTencentCBS(csiDeploy *csiv1.CSI) error {
	csiVersion, err := getCSIVersion(csiDeploy)
	if err != nil {
		return err
	}
	enhanceExternalComponents(e.config, csiDeploy, csiVersion)
	if csiDeploy.Spec.Node.LivenessProbe != nil {
		csiDeploy.Spec.Node.LivenessProbe.Parameters = map[string]string{
			types.LivenessProbePortKey: tencentCBSLivenessProbePorts.Node,
		}
	}
	if csiDeploy.Spec.Controller.LivenessProbe != nil {
		csiDeploy.Spec.Controller.LivenessProbe.Parameters = map[string]string{
			types.LivenessProbePortKey: tencentCBSLivenessProbePorts.Controller,
		}
	}

	csiDeploy.Spec.DriverTemplate = e.generateCBSDriverTemplate(csiVersion, csiDeploy)

	if csiDeploy.Spec.Version == csiv1.CSIVersionV1 {
		csiDeploy.Spec.DriverTemplate.Template.Spec.Containers[0].Command = []string{
			"/csi-tencentcloud-cbs",
		}
	}

	tencentInfo, err := e.getTencentInfo(csiDeploy)
	if err != nil {
		return fmt.Errorf("get tencent info failed: %v", err)
	}

	csiDeploy.Spec.Secrets, csiDeploy.Spec.StorageClasses, err = e.generateCBSSecretAndSCs(
		csiDeploy, tencentInfo)
	if err != nil {
		return fmt.Errorf("enhance TencentCloud Secret or StorageClasses failed: %v", err)
	}

	return nil
}

// enhanceTencentCFS enhances CSI for TencentCloud CFS storage.
func (e *tencentCloudEnhancer) enhanceTencentCFS(csiDeploy *csiv1.CSI) error {
	csiVersion, err := getCSIVersion(csiDeploy)
	if err != nil {
		return err
	}
	enhanceExternalComponents(e.config, csiDeploy, csiVersion)
	if csiDeploy.Spec.Node.LivenessProbe != nil {
		csiDeploy.Spec.Node.LivenessProbe.Parameters = map[string]string{
			types.LivenessProbePortKey: tencentCFSLivenessProbePorts.Node,
		}
	}
	if csiDeploy.Spec.Controller.LivenessProbe != nil {
		csiDeploy.Spec.Controller.LivenessProbe.Parameters = map[string]string{
			types.LivenessProbePortKey: tencentCFSLivenessProbePorts.Controller,
		}
	}

	csiDeploy.Spec.DriverTemplate = e.generateCFSDriverTemplate(csiVersion, csiDeploy)

	if csiDeploy.Spec.Version == csiv1.CSIVersionV1 {
		csiDeploy.Spec.DriverTemplate.Template.Spec.Containers[0].Command = []string{
			"/csi-tencentcloud-cfs",
		}
	}

	tencentInfo, err := e.getTencentInfo(csiDeploy)
	if err != nil {
		return fmt.Errorf("get tencent info failed: %v", err)
	}

	csiDeploy.Spec.Secrets, csiDeploy.Spec.StorageClasses, err = e.generateCFSSecretAndSCs(
		csiDeploy, tencentInfo)
	if err != nil {
		return fmt.Errorf("enhance TencentCloud Secret or StorageClasses failed: %v", err)
	}

	return nil
}

// generateCBSDriverTemplate generates the content of TencentCloud CBS storage's DriverTemplate.
func (e *tencentCloudEnhancer) generateCBSDriverTemplate(
	csiVersion *csiVersion,
	csiDeploy *csiv1.CSI) *csiv1.CSIDriverTemplate {
	return &csiv1.CSIDriverTemplate{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				HostNetwork: true,
				HostPID:     true,
				HostIPC:     true,
				DNSPolicy:   corev1.DNSClusterFirstWithHostNet,
				Tolerations: []corev1.Toleration{
					{
						Key:    "node-role.kubernetes.io/master",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
				Containers: []corev1.Container{
					{
						Name: "com-tencent-cloud-csi-cbs",
						SecurityContext: &corev1.SecurityContext{
							Privileged: boolPtr(true),
							Capabilities: &corev1.Capabilities{
								Add: []corev1.Capability{"SYS_ADMIN"},
							},
							AllowPrivilegeEscalation: boolPtr(true),
						},
						Image: getImage(e.config.RegistryDomain, csiVersion.Driver),
						Command: []string{
							"/bin/csi-tencentcloud",
						},
						Args: []string{
							"--v=5",
							"--logtostderr=true",
							"--endpoint=$(CSI_ENDPOINT)",
						},
						Env: []corev1.EnvVar{
							{
								Name: "TENCENTCLOUD_CBS_API_SECRET_ID",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: getSecretName(csiDeploy),
										},
										Key: "TENCENTCLOUD_CBS_API_SECRET_ID",
									},
								},
							},
							{
								Name: "TENCENTCLOUD_CBS_API_SECRET_KEY",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: getSecretName(csiDeploy),
										},
										Key: "TENCENTCLOUD_CBS_API_SECRET_KEY",
									},
								},
							},
						},
						ImagePullPolicy: corev1.PullAlways,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "device-dir",
								MountPath: "/dev",
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "device-dir",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/dev",
							},
						},
					},
				},
			},
		},
	}
}

// generateCFSDriverTemplate generates the content of TencentCloud CFS storage's DriverTemplate.
func (e *tencentCloudEnhancer) generateCFSDriverTemplate(
	csiVersion *csiVersion,
	csiDeploy *csiv1.CSI) *csiv1.CSIDriverTemplate {
	return &csiv1.CSIDriverTemplate{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				HostNetwork: true,
				HostPID:     true,
				HostIPC:     true,
				DNSPolicy:   corev1.DNSClusterFirstWithHostNet,
				Tolerations: []corev1.Toleration{
					{
						Key:    "node-role.kubernetes.io/master",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
				Containers: []corev1.Container{
					{
						Name: "com-tencent-cloud-csi-cfs",
						SecurityContext: &corev1.SecurityContext{
							Privileged: boolPtr(true),
							Capabilities: &corev1.Capabilities{
								Add: []corev1.Capability{"SYS_ADMIN"},
							},
							AllowPrivilegeEscalation: boolPtr(true),
						},
						Image: getImage(e.config.RegistryDomain, csiVersion.Driver),
						Command: []string{
							"/bin/csi-tencentcloud",
						},
						Args: []string{
							"--v=5",
							"--logtostderr=true",
							"--endpoint=$(CSI_ENDPOINT)",
						},
						Env: []corev1.EnvVar{
							{
								Name: "NODE_ID",
								ValueFrom: &corev1.EnvVarSource{
									FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
								},
							},
							{
								Name: "TENCENTCLOUD_API_SECRET_ID",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: getSecretName(csiDeploy),
										},
										Key: "TENCENTCLOUD_API_SECRET_ID",
									},
								},
							},
							{
								Name: "TENCENTCLOUD_API_SECRET_KEY",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: getSecretName(csiDeploy),
										},
										Key: "TENCENTCLOUD_API_SECRET_KEY",
									},
								},
							},
						},
						ImagePullPolicy: corev1.PullAlways,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "device-dir",
								MountPath: "/dev",
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "device-dir",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/dev",
							},
						},
					},
				},
			},
		},
	}
}

// getTencentInfo generates TencentCloud information from CSI object and global config.
func (e *tencentCloudEnhancer) getTencentInfo(csiDeploy *csiv1.CSI) (*tencentCloudInfo, error) {
	secretID := csiDeploy.Spec.Parameters[secretID]
	if len(secretID) == 0 {
		secretID = e.config.SecretID
	}

	secretKey := csiDeploy.Spec.Parameters[secretKey]
	if len(secretKey) == 0 {
		secretKey = e.config.SecretKey
	}

	if len(secretID) == 0 || len(secretKey) == 0 {
		return nil, fmt.Errorf("no tencent info of secretID, secretKey in csiDeploy.Spec.Parameters: %v", csiDeploy.Spec.Parameters)
	}

	vpcID := csiDeploy.Spec.Parameters[vpcID]

	subnetID := csiDeploy.Spec.Parameters[subnetID]

	if csiDeploy.Spec.DriverName == csiv1.CSIDriverTencentCFS {
		if len(vpcID) == 0 || len(subnetID) == 0 {
			return nil, fmt.Errorf("no tencent info of vpcID, subnetID in csiDeploy.Spec.Parameters: %v", csiDeploy.Spec.Parameters)
		}
	}

	return &tencentCloudInfo{
		SecretID:  secretID,
		SecretKey: secretKey,
		VpcID:     vpcID,
		SubnetID:  subnetID,
	}, nil
}

// generateCBSSecretAndSCs generates secrets and StorageClasses needed by TencentCloud CBS storage.
func (e *tencentCloudEnhancer) generateCBSSecretAndSCs(
	csiDeploy *csiv1.CSI,
	tencentInfo *tencentCloudInfo) ([]corev1.Secret, []storagev1.StorageClass, error) {
	// Generate secrets.
	secretName := getSecretName(csiDeploy)
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: csiDeploy.Namespace,
		},
	}

	secretID, err := base64.StdEncoding.DecodeString(tencentInfo.SecretID)
	if err != nil {
		return nil, nil, fmt.Errorf("secretID decoding failed: %v", err)
	}
	secretKey, err := base64.StdEncoding.DecodeString(tencentInfo.SecretKey)
	if err != nil {
		return nil, nil, fmt.Errorf("secretKey decoding failed: %v", err)
	}

	secret.Data = map[string][]byte{
		"TENCENTCLOUD_CBS_API_SECRET_ID":  secretID,
		"TENCENTCLOUD_CBS_API_SECRET_KEY": secretKey,
	}

	// Generate storageClasses.
	reclaimPolicy := corev1.PersistentVolumeReclaimDelete
	var storageClass = storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cbs-basic-prepaid",
		},
		Provisioner:   csiDeploy.Spec.DriverName,
		ReclaimPolicy: &reclaimPolicy,
		Parameters: map[string]string{
			"diskType":                    "CLOUD_BASIC",
			"diskChargeType":              "PREPAID",
			"diskChargeTypePrepaidPeriod": "2",
			"diskChargePrepaidRenewFlag":  "NOTIFY_AND_AUTO_RENEW",
		},
	}

	return []corev1.Secret{secret}, []storagev1.StorageClass{storageClass}, nil
}

// generateCFSSecretAndSCs generates secrets and StorageClasses needed by TencentCloud CFS storage.
func (e *tencentCloudEnhancer) generateCFSSecretAndSCs(
	csiDeploy *csiv1.CSI,
	tencentInfo *tencentCloudInfo) ([]corev1.Secret, []storagev1.StorageClass, error) {
	// Generate secrets.
	secretName := getSecretName(csiDeploy)
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: csiDeploy.Namespace,
		},
	}

	secretID, err := base64.StdEncoding.DecodeString(tencentInfo.SecretID)
	if err != nil {
		return nil, nil, fmt.Errorf("secretID decoding failed: %v", err)
	}
	secretKey, err := base64.StdEncoding.DecodeString(tencentInfo.SecretKey)
	if err != nil {
		return nil, nil, fmt.Errorf("secretKey decoding failed: %v", err)
	}

	secret.Data = map[string][]byte{
		"TENCENTCLOUD_API_SECRET_ID":  secretID,
		"TENCENTCLOUD_API_SECRET_KEY": secretKey,
	}

	// Generate storageClasses.
	reclaimPolicy := corev1.PersistentVolumeReclaimDelete
	var storageClass = storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cfsauto",
		},
		Provisioner:   csiDeploy.Spec.DriverName,
		ReclaimPolicy: &reclaimPolicy,
		Parameters: map[string]string{
			"vpcid":    tencentInfo.VpcID,
			"subnetid": tencentInfo.SubnetID,
		},
	}

	return []corev1.Secret{secret}, []storagev1.StorageClass{storageClass}, nil
}
