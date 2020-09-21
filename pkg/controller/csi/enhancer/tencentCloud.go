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
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/klog"
)

const (
	secretID  = "secretID"
	secretKey = "secretKey"

	// TencentCloudAPISecretID represents the name of tencent cloud secret id's environment variable,
	// which used in tencent cloud's csi plugin.
	TencentCloudAPISecretID = "TENCENTCLOUD_CBS_API_SECRET_ID"
	// TencentCloudAPISecretKey represents the name of tencent cloud secret key's environment variable,
	// which used in tencent cloud's csi plugin.
	TencentCloudAPISecretKey = "TENCENTCLOUD_CBS_API_SECRET_KEY"
)

// tencentCloudInfo if a set of information of TencentCloud secrets.
type tencentCloudInfo struct {
	SecretID  string
	SecretKey string
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
	if csiDeploy.Spec.DriverName == csiv1.CSIDriverTencentCBS {
		return e.enhanceTencentCBS(csiDeploy)
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

	csiDeploy.Spec.DriverTemplate = e.generateDriverTemplate(csiVersion, csiDeploy)

	if curVersion, err := version.ParseGeneric(string(csiDeploy.Spec.Version)); err == nil {
		csiV1 := version.MustParseGeneric(csiv1.CSIVersionV1)
		if curVersion.AtLeast(csiV1) {
			csiDeploy.Spec.DriverTemplate.Template.Spec.Containers[0].Command = []string{
				"/csi-tencentcloud-cbs",
			}
		}
	} else {
		klog.Warningf("invalid csi version: %+v", curVersion)
	}

	tencentInfo, err := e.getTencentInfo(csiDeploy)
	if err != nil {
		return fmt.Errorf("get tencent info failed: %v", err)
	}

	csiDeploy.Spec.Secrets, csiDeploy.Spec.StorageClasses, err = e.generateSecretAndSCs(
		csiDeploy, tencentInfo)
	if err != nil {
		return fmt.Errorf("enhance TencentCloud Secret or StorageClasses failed: %v", err)
	}

	return nil
}

// generateDriverTemplate generates the content of DriverTemplate.
func (e *tencentCloudEnhancer) generateDriverTemplate(
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
								Name: TencentCloudAPISecretID,
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: getSecretName(csiDeploy),
										},
										Key: TencentCloudAPISecretID,
									},
								},
							},
							{
								Name: TencentCloudAPISecretKey,
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: getSecretName(csiDeploy),
										},
										Key: TencentCloudAPISecretKey,
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

	return &tencentCloudInfo{
		SecretID:  secretID,
		SecretKey: secretKey,
	}, nil
}

// generateSecretAndSCs generates secrets and StorageClasses needed by TencentCloud storage.
func (e *tencentCloudEnhancer) generateSecretAndSCs(
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
		TencentCloudAPISecretID:  secretID,
		TencentCloudAPISecretKey: secretKey,
	}

	// Generate storageClasses.
	reclaimPolicy := corev1.PersistentVolumeReclaimDelete
	var basicSC = storagev1.StorageClass{
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
	var premiumSC = storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cbs-premium",
		},
		Provisioner:   csiDeploy.Spec.DriverName,
		ReclaimPolicy: &reclaimPolicy,
		Parameters: map[string]string{
			"diskType": "CLOUD_PREMIUM",
		},
	}
	var ssdSC = storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cbs-ssd",
		},
		Provisioner:   csiDeploy.Spec.DriverName,
		ReclaimPolicy: &reclaimPolicy,
		Parameters: map[string]string{
			"diskType": "CLOUD_SSD",
		},
	}

	return []corev1.Secret{secret}, []storagev1.StorageClass{basicSC, premiumSC, ssdSC}, nil
}
