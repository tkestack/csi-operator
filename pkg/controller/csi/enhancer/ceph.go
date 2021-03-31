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
)

var (
	// These keys used to specify specific parameters in StorageClass.
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

	// Fill ceph related information.
	cephInfo := e.getCephInfo(csiDeploy)
	if cephInfo != nil {
		csiDeploy.Spec.Secrets, csiDeploy.Spec.StorageClasses = e.enhanceCephSecretAndStorageClasses(csiDeploy, cephInfo)
	}

	return nil
}

func (e *cephEnhancer) generateCephFSDriverTemplate(
	csiVersion *csiVersion,
	csiDeploy *csiv1.CSI) {
	csiDeploy.Spec.DriverTemplate = &csiv1.CSIDriverTemplate{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
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
			Name:         "mount-cache-dir",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})

		container := &spec.Containers[0]
		container.Args = append(container.Args, "--mountcachedir=/mount-cache-dir")
		container.VolumeMounts = append(container.VolumeMounts,
			corev1.VolumeMount{Name: "mount-cache-dir", MountPath: "/mount-cache-dir"})
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
