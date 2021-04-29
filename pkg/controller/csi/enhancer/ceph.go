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
	"encoding/json"
	"fmt"
	"k8s.io/klog"
	"strings"

	csiv1 "tkestack.io/csi-operator/pkg/apis/storage/v1"
	"tkestack.io/csi-operator/pkg/config"
	"tkestack.io/csi-operator/pkg/types"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Key used to specify ceph monitor domains in StorageClass.
	monitorsKey = "monitors"
	// Key used to specify ceph admin ID in StorageClass.
	adminIDKey = "adminID"
	// Key used to specify ceph admin keyring in StorageClass.
	adminKeyringKey = "adminKey"
	// Key used to specify ceph pools in StorageClass.
	poolsKey = "pools"
	// Key used to specify ceph config used by ceph-csi of cephFS versions 3.2.0 and above.
	configsKey = "configs"
)

var (
	// These keys used to specify specific parameters in StorageClass.
	controllerExpandSecretKey = map[csiv1.CSIVersion]keySet{
		csiv1.CSIVersionV1: {
			Name:      "csi.storage.k8s.io/controller-expand-secret-name",
			Namespace: "csi.storage.k8s.io/controller-expand-secret-namespace",
		},
		csiv1.CSIVersionV0: {
			Name:      "csiControllerExpandSecretName",
			Namespace: "csiControllerExpandSecretNamespace",
		},
	}
	controllerPublishSecretKey = map[csiv1.CSIVersion]keySet{
		csiv1.CSIVersionV1: {
			Name:      "csi.storage.k8s.io/controller-publish-secret-name",
			Namespace: "csi.storage.k8s.io/controller-publish-secret-namespace",
		},
		csiv1.CSIVersionV0: {
			Name:      "csiControllerPublishSecretName",
			Namespace: "csiControllerPublishSecretNamespace",
		},
	}
	provisionerSecretKey = map[csiv1.CSIVersion]keySet{
		csiv1.CSIVersionV1: {
			Name:      "csi.storage.k8s.io/provisioner-secret-name",
			Namespace: "csi.storage.k8s.io/provisioner-secret-namespace",
		},
		csiv1.CSIVersionV0: {
			Name:      "csiProvisionerSecretName",
			Namespace: "csiProvisionerSecretNamespace",
		},
	}
	nodeSecretKey = map[csiv1.CSIVersion]map[string]keySet{
		csiv1.CSIVersionV1: {
			csiv1.CSIDriverCephRBD: {
				Name:      "csi.storage.k8s.io/node-publish-secret-name",
				Namespace: "csi.storage.k8s.io/node-publish-secret-namespace",
			},
			csiv1.CSIDriverCephFS: {
				Name:      "csi.storage.k8s.io/node-stage-secret-name",
				Namespace: "csi.storage.k8s.io/node-stage-secret-namespace",
			},
		},
		csiv1.CSIVersionV0: {
			csiv1.CSIDriverCephRBD: {
				Name:      "csiNodePublishSecretName",
				Namespace: "csiNodePublishSecretNamespace",
			},
			csiv1.CSIDriverCephFS: {
				Name:      "csiNodeStageSecretName",
				Namespace: "csiNodeStageSecretNamespace",
			},
		},
	}
)

// newCephEnhancer creates a cephEnhancer.
func newCephEnhancer(config *config.Config) Enhancer {
	return &cephEnhancer{config}
}

// cephEnhancer is a Enhancer for ceph.
type cephEnhancer struct {
	config *config.Config
}

// Enhance enhances a well known CSI type.
func (e *cephEnhancer) Enhance(csiDeploy *csiv1.CSI) error {
	switch csiDeploy.Spec.DriverName {
	case csiv1.CSIDriverCephRBD:
		return e.enhanceCephRBD(csiDeploy)
	case csiv1.CSIDriverCephFS:
		return e.enhanceCephFS(csiDeploy)
	}
	return fmt.Errorf("unknown type: %s", csiDeploy.Spec.DriverName)
}

// enhanceCephRBD enhances a CephRBD volume.
func (e *cephEnhancer) enhanceCephRBD(csiDeploy *csiv1.CSI) error {
	csiVersion, err := getCSIVersion(csiDeploy)
	if err != nil {
		return err
	}
	enhanceExternalComponents(e.config, csiDeploy, csiVersion)
	// Fill livenessProbe ports.
	if csiDeploy.Spec.Node.LivenessProbe != nil {
		csiDeploy.Spec.Node.LivenessProbe.Parameters = map[string]string{
			types.LivenessProbePortKey: cephRBDLivenessProbePorts.Node,
		}
	}
	if csiDeploy.Spec.Controller.LivenessProbe != nil {
		csiDeploy.Spec.Controller.LivenessProbe.Parameters = map[string]string{
			types.LivenessProbePortKey: cephRBDLivenessProbePorts.Controller,
		}
	}

	csiDeploy.Spec.DriverTemplate = &csiv1.CSIDriverTemplate{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				HostNetwork: true,
				HostPID:     true,
				DNSPolicy:   corev1.DNSClusterFirstWithHostNet,
				Tolerations: []corev1.Toleration{
					{
						Key:    "node-role.kubernetes.io/master",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
				Containers: []corev1.Container{
					{
						Name: "csi-rbd",
						SecurityContext: &corev1.SecurityContext{
							Privileged: boolPtr(true),
							Capabilities: &corev1.Capabilities{
								Add: []corev1.Capability{"SYS_ADMIN"},
							},
							AllowPrivilegeEscalation: boolPtr(true),
						},
						Image: getImage(e.config.RegistryDomain, csiVersion.Driver),
						Args: []string{
							"--nodeid=$(NODE_ID)",
							"--endpoint=$(CSI_ENDPOINT)",
							"--v=5",
							"--drivername=" + csiDeploy.Spec.DriverName,
							"--containerized=true",
							"--metadatastorage=k8s_configmap",
						},
						Env:             append(fieldEnvs(), corev1.EnvVar{Name: "HOST_ROOTFS", Value: "/rootfs"}),
						ImagePullPolicy: corev1.PullAlways,
						VolumeMounts:    append(hostVolumeMounts(), corev1.VolumeMount{Name: "host-rootfs", MountPath: "/rootfs"}),
					},
				},
				Volumes: append(hostVolumes(),
					// Mount host file system to the container.
					corev1.Volume{
						Name:         "host-rootfs",
						VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}},
					}),
			},
		},
	}

	cephInfo := e.getCephInfo(csiDeploy)
	if cephInfo != nil {
		csiDeploy.Spec.Secrets, csiDeploy.Spec.StorageClasses = e.enhanceCephSecretAndStorageClasses(csiDeploy, cephInfo)
	}

	return nil
}

// enhanceCephFS enhance a CephFS volume.
func (e *cephEnhancer) enhanceCephFS(csiDeploy *csiv1.CSI) error {
	csiVersion, err := getCSIVersion(csiDeploy)
	if err != nil {
		return err
	}
	enhanceExternalComponents(e.config, csiDeploy, csiVersion)
	if csiDeploy.Spec.Node.LivenessProbe != nil {
		csiDeploy.Spec.Node.LivenessProbe.Parameters = map[string]string{
			types.LivenessProbePortKey: cephFSLivenessProbePorts.Node,
		}
	}
	if csiDeploy.Spec.Controller.LivenessProbe != nil {
		csiDeploy.Spec.Controller.LivenessProbe.Parameters = map[string]string{
			types.LivenessProbePortKey: cephFSLivenessProbePorts.Controller,
		}
	}

	// Fill DriverTemplate.
	e.generateCephFSDriverTemplate(csiVersion, csiDeploy)

	if csiDeploy.Spec.Version == csiv1.CSIVersionV0 {
		// Fill ceph related information.
		cephInfo := e.getCephInfo(csiDeploy)
		if cephInfo != nil {
			csiDeploy.Spec.Secrets, csiDeploy.Spec.StorageClasses = e.enhanceCephSecretAndStorageClasses(csiDeploy, cephInfo)
		}
	} else {
		cephConfigs := e.getCephConfigs(csiDeploy)
		if cephConfigs != nil {
			csiDeploy.Spec.Secrets, csiDeploy.Spec.StorageClasses, csiDeploy.Spec.ConfigMaps =
				e.enhanceCephSecretsStorageClassesAndConfigMap(csiDeploy, cephConfigs)
		}
	}

	return nil
}

func (e *cephEnhancer) generateCephFSDriverTemplate(
	csiVersion *csiVersion,
	csiDeploy *csiv1.CSI) {
	csiDeploy.Spec.DriverTemplate = &csiv1.CSIDriverTemplate{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				HostPID:     true,
				HostNetwork: true,
				DNSPolicy:   corev1.DNSClusterFirstWithHostNet,
				Tolerations: []corev1.Toleration{
					{
						Key:    "node-role.kubernetes.io/master",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
				Containers: []corev1.Container{
					{
						Name: "csi-cephfs",
						SecurityContext: &corev1.SecurityContext{
							Privileged: boolPtr(true),
							Capabilities: &corev1.Capabilities{
								Add: []corev1.Capability{"SYS_ADMIN"},
							},
							AllowPrivilegeEscalation: boolPtr(true),
						},
						Image: getImage(e.config.RegistryDomain, csiVersion.Driver),
						Args: []string{
							"--nodeid=$(NODE_ID)",
							"--endpoint=$(CSI_ENDPOINT)",
							"--v=5",
							"--drivername=" + csiDeploy.Spec.DriverName,
							"--metadatastorage=k8s_configmap",
						},
						Env:             fieldEnvs(),
						ImagePullPolicy: corev1.PullAlways,
						VolumeMounts:    hostVolumeMounts(),
					},
				},
				Volumes: hostVolumes(),
			},
		},
	}

	// Mount cache related parameters are only supported in CSI V1.x.
	if csiDeploy.Spec.Version == csiv1.CSIVersionV1 {
		spec := &csiDeploy.Spec.DriverTemplate.Template.Spec
		spec.Volumes = append(spec.Volumes, corev1.Volume{
			Name: "ceph-csi-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: getConfigMapName(csiDeploy),
					},
				},
			},
		}, corev1.Volume{
			Name: "keys-tmp-dir",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium: corev1.StorageMediumMemory,
				},
			},
		})

		container := &spec.Containers[0]
		container.Args = []string{
			"--nodeid=$(NODE_ID)",
			"--endpoint=$(CSI_ENDPOINT)",
			"--v=5",
			"--drivername=" + csiDeploy.Spec.DriverName,
			"--type=cephfs",
		}
		container.Env = append(container.Env, corev1.EnvVar{
			Name: "POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
			},
		})
		container.VolumeMounts = append(container.VolumeMounts,
			corev1.VolumeMount{Name: "ceph-csi-config", MountPath: "/etc/ceph-csi-config/"},
			corev1.VolumeMount{Name: "keys-tmp-dir", MountPath: "/tmp/csi/keys"})
	}
}

// 1. Generate a Secret to hold ceph secret information
// 2. Create a StorageClass for each pool and file system
func (e *cephEnhancer) enhanceCephSecretAndStorageClasses(
	csiDeploy *csiv1.CSI,
	cephInfo *cephInfo) ([]corev1.Secret, []storagev1.StorageClass) {
	// Generate secrets.
	secretName := getSecretName(csiDeploy)
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: csiDeploy.Namespace,
		},
	}
	adminKey := []byte(cephInfo.AdminKey)
	switch csiDeploy.Spec.DriverName {
	case csiv1.CSIDriverCephFS:
		secret.Data = map[string][]byte{
			"adminKey": adminKey,
			"adminID":  []byte(cephInfo.AdminID),
		}
	case csiv1.CSIDriverCephRBD:
		secret.Data = map[string][]byte{cephInfo.AdminID: adminKey}
	}

	// Generate storageClasses.
	reclaimPolicy := corev1.PersistentVolumeReclaimDelete
	var scList []storagev1.StorageClass
	for _, pool := range cephInfo.Pools {
		sc := storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: csiDeploy.Spec.DriverName + "-" + pool,
			},
			Provisioner:   csiDeploy.Spec.DriverName,
			ReclaimPolicy: &reclaimPolicy,
			Parameters: map[string]string{
				"monitors": cephInfo.Monitors,
				"pool":     pool,
				"adminid":  cephInfo.AdminID,
				"userid":   cephInfo.AdminID,

				provisionerSecretKey[csiDeploy.Spec.Version].Name:      secretName,
				provisionerSecretKey[csiDeploy.Spec.Version].Namespace: secret.Namespace,

				nodeSecretKey[csiDeploy.Spec.Version][csiDeploy.Spec.DriverName].Name:      secretName,
				nodeSecretKey[csiDeploy.Spec.Version][csiDeploy.Spec.DriverName].Namespace: secret.Namespace,
			},
		}

		if csiDeploy.Spec.DriverName == csiv1.CSIDriverCephRBD {
			sc.Parameters["imageFormat"] = "2"
		}
		if csiDeploy.Spec.DriverName == csiv1.CSIDriverCephFS {
			sc.Parameters["provisionVolume"] = "true"
		}

		scList = append(scList, sc)
	}

	if len(scList) == 1 {
		scList[0].Name = csiDeploy.Spec.DriverName
	}

	return []corev1.Secret{secret}, e.getStorageClassesWithFS(csiDeploy.Spec.DriverName, scList)
}

// 1. Generate a Secret to hold ceph secret information
// 2. Create a StorageClass for each pool and file system
// 3. Create a ConfigMap holds ceph clusters' information
func (e *cephEnhancer) enhanceCephSecretsStorageClassesAndConfigMap(
	csiDeploy *csiv1.CSI,
	cephConfigs []cephConfig) ([]corev1.Secret, []storagev1.StorageClass, []corev1.ConfigMap) {
	secrets := make([]corev1.Secret, 0)
	storageClasses := make([]storagev1.StorageClass, 0)
	cephDriverConfigs := make([]cephDriverConfig, 0)
	for _, conf := range cephConfigs {
		// Generate secret.
		secretName := fmt.Sprintf("%s-%s", getSecretName(csiDeploy), conf.ClusterID)
		secret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: csiDeploy.Namespace,
			},
		}
		adminKey := []byte(conf.AdminKey)
		switch csiDeploy.Spec.DriverName {
		case csiv1.CSIDriverCephFS:
			secret.Data = map[string][]byte{
				"adminKey": adminKey,
				"adminID":  []byte(conf.AdminID),
			}
			if conf.UserKey != "" && conf.UserID != "" {
				secret.Data["userID"] = []byte(conf.UserID)
				secret.Data["userKey"] = []byte(conf.UserKey)
			}
		case csiv1.CSIDriverCephRBD:
			secret.Data = map[string][]byte{conf.AdminID: adminKey}
		}
		secrets = append(secrets, secret)

		// Generate storageClasses.
		reclaimPolicy := corev1.PersistentVolumeReclaimDelete
		pools := strings.Split(conf.Pools, ",")
		if len(conf.FSName) == 0 {
			conf.FSName = "cephfs"
		}
		for _, pool := range pools {
			sc := storagev1.StorageClass{
				AllowVolumeExpansion: boolPtr(true),
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%s-%s", csiDeploy.Spec.DriverName, conf.ClusterID, pool),
				},
				Provisioner:   csiDeploy.Spec.DriverName,
				ReclaimPolicy: &reclaimPolicy,
				Parameters: map[string]string{
					"pool":      pool,
					"adminid":   conf.AdminID,
					"userid":    conf.AdminID,
					"clusterID": conf.ClusterID,
					"fsName":    conf.FSName,

					controllerPublishSecretKey[csiDeploy.Spec.Version].Name:      secretName,
					controllerPublishSecretKey[csiDeploy.Spec.Version].Namespace: secret.Namespace,

					controllerExpandSecretKey[csiDeploy.Spec.Version].Name:      secretName,
					controllerExpandSecretKey[csiDeploy.Spec.Version].Namespace: secret.Namespace,

					provisionerSecretKey[csiDeploy.Spec.Version].Name:      secretName,
					provisionerSecretKey[csiDeploy.Spec.Version].Namespace: secret.Namespace,

					nodeSecretKey[csiDeploy.Spec.Version][csiDeploy.Spec.DriverName].Name:      secretName,
					nodeSecretKey[csiDeploy.Spec.Version][csiDeploy.Spec.DriverName].Namespace: secret.Namespace,
				},
			}

			if csiDeploy.Spec.DriverName == csiv1.CSIDriverCephRBD {
				sc.Parameters["imageFormat"] = "2"
			}
			if csiDeploy.Spec.DriverName == csiv1.CSIDriverCephFS {
				sc.Parameters["provisionVolume"] = "true"
			}

			storageClasses = append(storageClasses, sc)

			// convert cephConfig to cephDriverConfig
			driverConf := cephDriverConfig{
				ClusterID: conf.ClusterID,
				Monitors:  strings.Split(conf.Monitors, ","),
			}
			if len(conf.SubVolumeGroup) != 0 {
				driverConf.CephFS = &cephFSDriverConfig{
					SubVolumeGroup: conf.SubVolumeGroup,
				}
			}
			cephDriverConfigs = append(cephDriverConfigs, driverConf)
		}
	}

	if len(storageClasses) == 1 {
		storageClasses[0].Name = csiDeploy.Spec.DriverName
	}

	// Generate configMap.
	configMapName := getConfigMapName(csiDeploy)
	configMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: csiDeploy.Namespace,
		},
	}

	if body, err := json.Marshal(cephDriverConfigs); err == nil {
		configMap.Data = map[string]string{
			"config.json": string(body),
		}
	} else {
		klog.Warningf("marshal cephDriverConfigs failed: %v", err)
		configMap.Data = map[string]string{
			"config.json": "[]",
		}
	}

	return secrets, e.getStorageClassesWithFS(csiDeploy.Spec.DriverName, storageClasses), []corev1.ConfigMap{configMap}
}

// Only used for Ceph RBD.
func (e *cephEnhancer) getStorageClassesWithFS(
	typ string,
	storageClasses []storagev1.StorageClass) []storagev1.StorageClass {
	if typ != csiv1.CSIDriverCephRBD {
		return storageClasses
	}

	fileSystems := strings.Split(e.config.Filesystems, ",")

	var result []storagev1.StorageClass
	for i := range storageClasses {
		for _, fs := range fileSystems {
			sc := storageClasses[i].DeepCopy()
			sc.Name += "-" + fs
			sc.Parameters["fstype"] = fs
			result = append(result, *sc)
		}
	}

	return result
}

// cephInfo is a set of information of Ceph Cluster.
type cephInfo struct {
	Monitors string
	AdminID  string
	AdminKey string
	Pools    []string
}

// cephConfig is a set of information of CephFS Cluster used by ceph-csi versions 3.2.0 and above.
type cephConfig struct {
	Monitors       string `json:"monitors"`
	AdminID        string `json:"adminID"`
	AdminKey       string `json:"adminKey"`
	Pools          string `json:"pools"`
	ClusterID      string `json:"clusterID"`
	FSName         string `json:"fsName"`
	SubVolumeGroup string `json:"subvolumeGroup"`
	UserID         string `json:"userID"`
	UserKey        string `json:"userKey"`
}

// cephDriverConfig is a set of information of Ceph Cluster stored in ConfigMap and
// used by ceph-csi versions 3.2.0 and above.
type cephDriverConfig struct {
	ClusterID string              `json:"clusterID"`
	Monitors  []string            `json:"monitors"`
	CephFS    *cephFSDriverConfig `json:"cephFS,omitempty"`
}

// cephFSDriverConfig is a set of information of CephFS Cluster stored in ConfigMap and
// used by ceph-csi versions 3.2.0 and above.
type cephFSDriverConfig struct {
	SubVolumeGroup string `json:"subvolumeGroup"`
}

// If Ceph related information specified in CSI, use it. Otherwise use configured information.
func (e *cephEnhancer) getCephConfigs(csiDeploy *csiv1.CSI) []cephConfig {
	configBody := csiDeploy.Spec.Parameters[configsKey]
	cephConfigs := make([]cephConfig, 0)
	err := json.Unmarshal([]byte(configBody), &cephConfigs)
	if err != nil {
		klog.Warningf("parse cephFS's config failed: %+v", err)
		return nil
	}

	return cephConfigs
}

// If Ceph related information specified in CSI, use it. Otherwise use configured information.
func (e *cephEnhancer) getCephInfo(csiDeploy *csiv1.CSI) *cephInfo {
	monitors := csiDeploy.Spec.Parameters[monitorsKey]
	if len(monitors) == 0 {
		monitors = e.config.Monitors
	}

	adminID := csiDeploy.Spec.Parameters[adminIDKey]
	if len(adminID) == 0 {
		adminID = e.config.AdminID
	}

	adminKey := csiDeploy.Spec.Parameters[adminKeyringKey]
	if len(adminKey) == 0 {
		adminKey = e.config.AdminKey
	}

	pools := csiDeploy.Spec.Parameters[poolsKey]

	if len(monitors) > 0 && len(adminID) > 0 && len(adminKey) > 0 && len(pools) > 0 {
		return &cephInfo{
			Monitors: monitors,
			AdminID:  adminID,
			AdminKey: adminKey,
			Pools:    strings.Split(pools, ","),
		}
	}

	return nil
}

// fieldEnvs returns a set of common ENVs for both CephRBD and CephFS.
func fieldEnvs() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: "NODE_ID",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
			},
		},
		{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
	}
}

// hostVolumeMounts returns VolumeMounts for all host path.
func hostVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: "host-dev", MountPath: "/dev"},
		{Name: "host-sys", MountPath: "/sys"},
		{Name: "lib-modules", MountPath: "/lib/modules", ReadOnly: true},
	}
}

// hostVolumes returns Volumes for all host path.
func hostVolumes() []corev1.Volume {
	return []corev1.Volume{
		{Name: "host-dev", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/dev"}}},
		{Name: "host-sys", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/sys"}}},
		{Name: "lib-modules", VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: "/lib/modules"}}},
	}
}

// keySet uses as a key.
type keySet struct {
	Name      string
	Namespace string
}
