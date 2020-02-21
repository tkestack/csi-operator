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
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	csiv1 "tkestack.io/csi-operator/pkg/apis/storage/v1"
	"tkestack.io/csi-operator/pkg/types"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/klog"
)

const (
	nodeDriverLabel       = "storage.tkestack.io/nodedriver"
	controllerDriverLabel = "storage.tkestack.io/controllerdriver"

	socketVolumeName         = "csi-socket"
	registrationVolumeName   = "registration-dir"
	podMountDirVolumeName    = "pod-mount"
	deviceMountDirVolumeName = "device-mount"

	deviceMountRelPath = "plugins/kubernetes.io/csi/volumeDevices"

	endpointENVName         = "CSI_ENDPOINT"
	endpointInsideContainer = "/csi/csi.sock"

	// TODO: Make these configurable.
	livenessProbePortName         = "healthz"
	livenessProbePeriod           = 2
	livenessProbeTimeout          = 3
	livenessProbeInitialDelay     = 10
	livenessProbeFailureThreshold = 5
	// Use different port for node driver and controller driver as some CSI driver
	// will set hostNetwork to true.
	nodeLivenessProbePort       = 9808
	controllerLivenessProbePort = 9809

	systemNamespace = "kube-system"
)

var (
	csiV1  = version.MustParseGeneric("v1.0.0")
	csiV11 = version.MustParseGeneric("v1.1.0")
	csiV2  = version.MustParseGeneric("v2.0.0")
)

// syncNodeDriver updates the Node Driver of CSI.
func (r *ReconcileCSI) syncNodeDriver(csiDeploy *csiv1.CSI) (*appsv1.DaemonSet, bool, error) {
	existDS := &appsv1.DaemonSet{}
	desired := r.generateNodeDriver(csiDeploy)

	err := r.getObject(k8stypes.NamespacedName{Namespace: desired.Namespace, Name: desired.Name}, existDS)
	if err != nil {
		if errors.IsNotFound(err) {
			createErr := r.createObject(desired)
			if createErr != nil {
				return nil, false, fmt.Errorf("create node driver failed: %s", createErr.Error())
			}
			klog.Infof("Create node driver for %s/%s", csiDeploy.Namespace, csiDeploy.Name)
			return desired, true, nil
		}
		return nil, false, fmt.Errorf("get node driver failed: %s", err.Error())
	}

	updateDS := existDS.DeepCopy()

	if mergeObjectMeta(&desired.ObjectMeta, &updateDS.ObjectMeta) ||
		// This may be due to someone has changed the object manually.
		!hasSameGeneration(existDS, updateDS.GroupVersionKind(), csiDeploy) ||
		// The CSI object changed, we can't determine which field are changed,
		// so we assume the CSI.Spec.NodeDriverTemplate changed.
		csiDeploy.Status.ObservedGeneration != csiDeploy.Generation {
		updateDS.Spec = desired.Spec
		updateErr := r.updateObject(updateDS)
		if updateErr != nil {
			return nil, false, fmt.Errorf("update node driver failed: %s", updateErr.Error())
		}
		return updateDS, true, nil
	}

	return updateDS, false, nil
}

// generateNodeDriver generates the content of Node Driver.
func (r *ReconcileCSI) generateNodeDriver(csiDeploy *csiv1.CSI) *appsv1.DaemonSet {
	template := csiDeploy.Spec.DriverTemplate.Template.DeepCopy()

	if csiDeploy.Namespace == systemNamespace {
		// Make node as system critical.
		template.Spec.PriorityClassName = "system-cluster-critical"
	}

	// Set ServiceAccount.
	template.Spec.ServiceAccountName = serviceAccountName(csiDeploy, false)
	if csiDeploy.Spec.Node.NodeRegistrar != nil {
		// Inject node registrar container.
		template.Spec.Containers = append(template.Spec.Containers, r.generateNodeRegistrarContainer(csiDeploy))
	}
	if csiDeploy.Spec.Node.LivenessProbe != nil {
		// Inject LivenessProbe container.
		template.Spec.Containers = append(template.Spec.Containers, r.generateLivenessProbe(csiDeploy, false))
	}
	// Inject CSI and kubelet related volumes.
	template.Spec.Volumes = append(template.Spec.Volumes, r.generateNodeDriverVolumes(csiDeploy)...)
	// Inject volumeMounts into driver container.
	driverContainer := &template.Spec.Containers[0]
	driverContainer.VolumeMounts = append(driverContainer.VolumeMounts, r.generateNodeDriverVolumeMounts(csiDeploy)...)
	// Inject ENVs into driver container.
	driverContainer.Env = append(driverContainer.Env, endpointENV()...)
	if csiDeploy.Spec.Node.LivenessProbe != nil {
		injectLivenessProbe(driverContainer, csiDeploy.Spec.Node.LivenessProbe.Parameters, false)
	}

	name := csiDeploy.Name + "-node"
	selectedLabels := map[string]string{nodeDriverLabel: name}
	mergeLabels(&template.ObjectMeta, selectedLabels)

	// Generate the NodeRegistrar object.
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       csiDeploy.Namespace,
			Name:            name,
			OwnerReferences: ownerReference(csiDeploy),
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectedLabels},
			Template: *template,
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				// TODO: Make this configurable?
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
			},
		},
	}
}

// generateNodeDriverVolumes generates the volumes of Node Driver.
func (r *ReconcileCSI) generateNodeDriverVolumes(csiDeploy *csiv1.CSI) []corev1.Volume {
	return []corev1.Volume{
		// Driver socket path. For example:
		// If csiDeploy.Spec.DriverName is csi-rbdplugin,
		// Path would be /var/lib/kubelet/plugins/csi-rbdplugin.
		{
			Name: socketVolumeName,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: r.nodeSocketDir(csiDeploy),
					Type: hostPathTypePtr(corev1.HostPathDirectoryOrCreate),
				},
			},
		},
		// Registration path for node registrar container. For example:
		// If r.config.KubeletRootDir is /var/lib/kubelet,
		// Path would be /var/lib/kubelet/plugins_registry.
		{
			Name: registrationVolumeName,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: filepath.Join(r.config.KubeletRootDir, "plugins_registry"),
					Type: hostPathTypePtr(corev1.HostPathDirectory),
				},
			},
		},
		// Device mount path for driver container. For example:
		// If r.config.KubeletRootDir is /var/lib/kubelet,
		// Path would be /var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices.
		{
			Name: deviceMountDirVolumeName,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: filepath.Join(r.config.KubeletRootDir, deviceMountRelPath),
					Type: hostPathTypePtr(corev1.HostPathDirectoryOrCreate),
				},
			},
		},
		// Pod mount path for driver container. For example:
		// If r.config.KubeletRootDir is /var/lib/kubelet,
		// Path would be /var/lib/kubelet/pods.
		{
			Name: podMountDirVolumeName,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: filepath.Join(r.config.KubeletRootDir, "pods"),
					Type: hostPathTypePtr(corev1.HostPathDirectoryOrCreate),
				},
			},
		},
	}
}

// generateNodeRegistrarContainer generates the content of NodeRegister container.
func (r *ReconcileCSI) generateNodeRegistrarContainer(csiDeploy *csiv1.CSI) corev1.Container {
	registrar := corev1.Container{
		Name:  "node-driver-registrar",
		Image: csiDeploy.Spec.Node.NodeRegistrar.Image,
		Args: []string{
			"--v=5",
			"--csi-address=$(ADDRESS)",
			"--kubelet-registration-path=$(DRIVER_REG_SOCK_PATH)",
		},
		Resources: csiDeploy.Spec.Node.NodeRegistrar.Resources,
		Env: []corev1.EnvVar{
			// For example: if csiDeploy.Spec.DriverSocket is /var/lib/kubelet/plugins/csi-rbdplugin/csi.sock,
			// then value of ADDRESS would be /csi/csi.sock.
			{
				Name:  "ADDRESS",
				Value: endpointInsideContainer,
			},
			// For example: if r.config.KubeletRootDir is /var/lib/kubelet, csiDeploy.Spec.DriverSocket
			// is /tmp/csi.sock, csiDeploy.Spec.DriverName is csi-rbdplugin, then value of DRIVER_REG_SOCK_PATH
			// would be /var/lib/kubelet/plugins/csi-rbdplugin/csi.sock.
			{
				Name:  "DRIVER_REG_SOCK_PATH",
				Value: filepath.Join(r.nodeSocketDir(csiDeploy), filepath.Base(endpointInsideContainer)),
			},
			{
				Name: "KUBE_NODE_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "spec.nodeName",
					},
				},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      socketVolumeName,
				MountPath: filepath.Dir(endpointInsideContainer),
			},
			{
				Name:      registrationVolumeName,
				MountPath: "/registration",
			},
		},
	}

	copySecurityContext(csiDeploy, &registrar)

	return registrar
}

// Generate a list of VolumeMounts to inject into the CSI Driver container.
func (r *ReconcileCSI) generateNodeDriverVolumeMounts(csiDeploy *csiv1.CSI) []corev1.VolumeMount {
	bidirectional := corev1.MountPropagationBidirectional
	return []corev1.VolumeMount{
		{
			Name:      socketVolumeName,
			MountPath: filepath.Dir(endpointInsideContainer),
		},
		{
			Name:             deviceMountDirVolumeName,
			MountPath:        filepath.Join(r.config.KubeletRootDir, deviceMountRelPath),
			MountPropagation: &bidirectional,
		},
		{
			Name:             podMountDirVolumeName,
			MountPath:        filepath.Join(r.config.KubeletRootDir, "pods"),
			MountPropagation: &bidirectional,
		},
	}
}

// nodeSocketDir returns the socket dir of the driver.
func (r *ReconcileCSI) nodeSocketDir(csiDeploy *csiv1.CSI) string {
	return filepath.Join(r.config.KubeletRootDir, "plugins", sanitizeDriverName(csiDeploy.Spec.DriverName))
}

// syncControllerDriver updates the Controller Driver of CSI.
func (r *ReconcileCSI) syncControllerDriver(csiDeploy *csiv1.CSI) (*appsv1.Deployment, bool, error) {
	if !hasController(csiDeploy) {
		klog.Infof("Controller service disabled for %s/%s", csiDeploy.Namespace, csiDeploy.Name)
		return nil, false, nil
	}

	existDeploy := &appsv1.Deployment{}
	desired := r.generateControllerDriver(csiDeploy)

	err := r.getObject(k8stypes.NamespacedName{Namespace: desired.Namespace, Name: desired.Name}, existDeploy)
	if err != nil {
		if errors.IsNotFound(err) {
			createErr := r.createObject(desired)
			if createErr != nil {
				return nil, false, fmt.Errorf("create controller driver failed: %s", createErr.Error())
			}
			klog.Infof("Create controller driver for %s/%s", csiDeploy.Namespace, csiDeploy.Name)
			return desired, true, nil
		}
		return nil, false, fmt.Errorf("get controller driver failed: %s", err.Error())
	}

	updateDeploy := existDeploy.DeepCopy()

	if mergeObjectMeta(&desired.ObjectMeta, &updateDeploy.ObjectMeta) ||
		// This may be due to someone has changed the object manually.
		!hasSameGeneration(existDeploy, updateDeploy.GroupVersionKind(), csiDeploy) ||
		// The CSI object changed, we can't determine which field are changed,
		// so we assume the CSI.Spec.ControllerDriverTemplate changed.
		csiDeploy.Status.ObservedGeneration != csiDeploy.Generation {
		updateDeploy.Spec = desired.Spec
		updateErr := r.updateObject(updateDeploy)
		if updateErr != nil {
			return nil, false, fmt.Errorf("update node driver failed: %s", updateErr.Error())
		}
		return updateDeploy, true, nil
	}

	return updateDeploy, false, nil
}

// generateControllerDriver generates the content of Controller Driver.
func (r *ReconcileCSI) generateControllerDriver(csiDeploy *csiv1.CSI) *appsv1.Deployment {
	template := csiDeploy.Spec.DriverTemplate.Template.DeepCopy()

	if csiDeploy.Namespace == systemNamespace {
		// Make controller as system critical.
		template.Spec.PriorityClassName = "system-cluster-critical"
	}

	// Set ServiceAccount. SC's name is equal to CSI's name.
	template.Spec.ServiceAccountName = serviceAccountName(csiDeploy, true)
	if csiDeploy.Spec.Controller.Provisioner != nil {
		// Inject Provisioner container.
		template.Spec.Containers = append(template.Spec.Containers, r.generateProvisioner(csiDeploy))
	}
	if csiDeploy.Spec.Controller.Attacher != nil {
		// Inject Attacher container.
		template.Spec.Containers = append(template.Spec.Containers, r.generateAttacher(csiDeploy))
	}
	if csiDeploy.Spec.Controller.Resizer != nil {
		// Inject Resizer container.
		template.Spec.Containers = append(template.Spec.Containers, r.generateResizer(csiDeploy))
	}
	if csiDeploy.Spec.Controller.Snapshotter != nil {
		// Inject Snapshotter container.
		template.Spec.Containers = append(template.Spec.Containers, r.generateSnapshotter(csiDeploy))
	}
	if csiDeploy.Spec.Controller.ClusterRegistrar != nil {
		// Inject Cluster registrar container.
		template.Spec.Containers = append(template.Spec.Containers, r.generateClusterRegistrar(csiDeploy))
	}
	if csiDeploy.Spec.Controller.LivenessProbe != nil {
		// Inject LivenessProbe container.
		template.Spec.Containers = append(template.Spec.Containers, r.generateLivenessProbe(csiDeploy, true))
	}
	// Inject CSI and kubelet related volumes.
	template.Spec.Volumes = append(template.Spec.Volumes, r.generateControllerDriverVolumes(csiDeploy)...)
	// Inject volumeMounts into driver container.
	driverContainer := &template.Spec.Containers[0]
	driverContainer.VolumeMounts = append(driverContainer.VolumeMounts,
		r.generateControllerDriverVM(csiDeploy)...)
	// Inject ENVs into driver container.
	driverContainer.Env = append(driverContainer.Env, endpointENV()...)
	if csiDeploy.Spec.Controller.LivenessProbe != nil {
		injectLivenessProbe(driverContainer, csiDeploy.Spec.Controller.LivenessProbe.Parameters, true)
	}

	name := csiDeploy.Name + "-controller"
	selectedLabels := map[string]string{controllerDriverLabel: name}
	mergeLabels(&template.ObjectMeta, selectedLabels)

	// Generate the NodeRegistrar object.
	replicas := csiDeploy.Spec.Controller.Replicas
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       csiDeploy.Namespace,
			Name:            name,
			OwnerReferences: ownerReference(csiDeploy),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectedLabels},
			Template: *template,
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
		},
	}
}

// generateProvisioner generates the content of Provisioner container.
func (r *ReconcileCSI) generateProvisioner(csiDeploy *csiv1.CSI) corev1.Container {
	image := csiDeploy.Spec.Controller.Provisioner.Image

	provisioner := corev1.Container{
		Name:  "csi-provisioner",
		Image: image,
		Args: []string{
			"--v=5",
			"--csi-address=$(ADDRESS)",
			"--enable-leader-election=true",
		},
		Resources:    csiDeploy.Spec.Controller.Provisioner.Resources,
		Env:          sidecarEnvs(),
		VolumeMounts: sidecarVolumeMounts(),
	}

	// TODO: Add a version field to the CRD definition instead of extracting from image tag.
	v := version.MustParseGeneric(image[strings.LastIndex(image, ":")+1:])
	if !v.AtLeast(csiV1) {
		provisioner.Args = append(provisioner.Args, "--provisioner="+csiDeploy.Spec.DriverName)
	}

	copySecurityContext(csiDeploy, &provisioner)
	return provisioner
}

// generateAttacher generates the content of Attacher container.
func (r *ReconcileCSI) generateAttacher(csiDeploy *csiv1.CSI) corev1.Container {
	image := csiDeploy.Spec.Controller.Attacher.Image
	attacher := corev1.Container{
		Name:  "csi-attacher",
		Image: image,
		Args: []string{
			"--v=5",
			"--csi-address=$(ADDRESS)",
			"--leader-election",
			"--leader-election-namespace=$(MY_NAMESPACE)",
			"--leader-election-identity=$(MY_NAME)",
		},
		Resources: csiDeploy.Spec.Controller.Attacher.Resources,
		Env: append([]corev1.EnvVar{
			{
				Name:      "MY_NAME",
				ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}},
			},
			{
				Name:      "MY_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
			},
		}, sidecarEnvs()...),
		VolumeMounts: sidecarVolumeMounts(),
	}

	v := version.MustParseGeneric(image[strings.LastIndex(image, ":")+1:])
	if v.LessThan(csiV2) && v.AtLeast(csiV11) {
		klog.V(3).Infof("%s's attacher version is %s, need leader-election-type arg", csiDeploy.Name, v)
		attacher.Args = append(attacher.Args, "--leader-election-type=leases")
	} else {
		klog.V(3).Infof("%s's attacher version is %s, no need leader-election-type arg", csiDeploy.Name, v)
	}

	copySecurityContext(csiDeploy, &attacher)
	return attacher
}

// generateSnapshotter generates the content of Snapshotter container.
func (r *ReconcileCSI) generateSnapshotter(csiDeploy *csiv1.CSI) corev1.Container {
	snapshotter := corev1.Container{
		Name:  "csi-snapshotter",
		Image: csiDeploy.Spec.Controller.Snapshotter.Image,
		Args: []string{
			"--v=5",
			"--csi-address=$(ADDRESS)",
			"--connection-timeout=1m",
		},
		Resources:    csiDeploy.Spec.Controller.Snapshotter.Resources,
		Env:          sidecarEnvs(),
		VolumeMounts: sidecarVolumeMounts(),
	}
	copySecurityContext(csiDeploy, &snapshotter)
	return snapshotter
}

// generateResizer generates the content of Resizer container.
func (r *ReconcileCSI) generateResizer(csiDeploy *csiv1.CSI) corev1.Container {
	resizer := corev1.Container{
		Name:  "csi-resizer",
		Image: csiDeploy.Spec.Controller.Resizer.Image,
		Args: []string{
			"--v=5",
			"--csi-address=$(ADDRESS)",
			"--leader-election",
			"--leader-election-namespace=$(MY_NAMESPACE)",
			"--leader-election-identity=$(MY_NAME)",
		},
		Resources: csiDeploy.Spec.Controller.Resizer.Resources,
		Env: append([]corev1.EnvVar{
			{
				Name:      "MY_NAME",
				ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}},
			},
			{
				Name:      "MY_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
			},
		}, sidecarEnvs()...),
		VolumeMounts: sidecarVolumeMounts(),
	}
	copySecurityContext(csiDeploy, &resizer)
	return resizer
}

// generateClusterRegistrar generates the content of ClusterRegistrar container.
func (r *ReconcileCSI) generateClusterRegistrar(csiDeploy *csiv1.CSI) corev1.Container {
	registrar := corev1.Container{
		Name:  "cluster-driver-registrar",
		Image: csiDeploy.Spec.Controller.ClusterRegistrar.Image,
		Args: []string{
			"--v=5",
			"--csi-address=$(ADDRESS)",
			"--pod-info-mount",
		},
		Env:          sidecarEnvs(),
		Resources:    csiDeploy.Spec.Controller.ClusterRegistrar.Resources,
		VolumeMounts: sidecarVolumeMounts(),
	}
	copySecurityContext(csiDeploy, &registrar)
	return registrar
}

// generateLivenessProbe generates the content of LivenessProbe container.
func (r *ReconcileCSI) generateLivenessProbe(csiDeploy *csiv1.CSI, controller bool) corev1.Container {
	var port int32
	if controller {
		port = getLivenessProbePort(csiDeploy.Spec.Controller.LivenessProbe.Parameters, controller)
	} else {
		port = getLivenessProbePort(csiDeploy.Spec.Node.LivenessProbe.Parameters, controller)
	}

	probe := corev1.Container{
		Name: "liveness-probe",
		Args: []string{
			"--v=5",
			"--csi-address=$(ADDRESS)",
			fmt.Sprintf("--health-port=%d", port),
			fmt.Sprintf("--connection-timeout=%ds", livenessProbeTimeout),
		},
		Env:          sidecarEnvs(),
		VolumeMounts: sidecarVolumeMounts(),
	}
	if controller {
		probe.Image = csiDeploy.Spec.Controller.LivenessProbe.Image
		probe.Resources = csiDeploy.Spec.Controller.LivenessProbe.Resources
	} else {
		probe.Image = csiDeploy.Spec.Node.LivenessProbe.Image
		probe.Resources = csiDeploy.Spec.Node.LivenessProbe.Resources
	}
	copySecurityContext(csiDeploy, &probe)
	return probe
}

// injectLivenessProbe injects the LivenessProbe config into container.
func injectLivenessProbe(container *corev1.Container, parameters map[string]string, controller bool) {
	container.Ports = append(container.Ports, corev1.ContainerPort{
		Name:          livenessProbePortName,
		ContainerPort: getLivenessProbePort(parameters, controller),
		Protocol:      corev1.ProtocolTCP,
	})
	container.LivenessProbe = &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/healthz",
				Port: intstr.IntOrString{Type: intstr.String, StrVal: livenessProbePortName},
			},
		},
		PeriodSeconds:       livenessProbePeriod,
		TimeoutSeconds:      livenessProbeTimeout,
		FailureThreshold:    livenessProbeFailureThreshold,
		InitialDelaySeconds: livenessProbeInitialDelay,
	}
}

// getLivenessProbePort gets the livenessProbe port.
func getLivenessProbePort(parameters map[string]string, controller bool) int32 {
	for key, value := range parameters {
		if key == types.LivenessProbePortKey {
			port, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				klog.Errorf("Parse liveness probe %s failed: %v", value, err)
				break
			}
			return int32(port)
		}
	}

	port := nodeLivenessProbePort
	if controller {
		port = controllerLivenessProbePort
	}
	return int32(port)
}

// sidecarEnvs returns a set of common ENVs.
func sidecarEnvs() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "ADDRESS",
			Value: endpointInsideContainer,
		},
	}
}

// sidecarVolumeMounts returns a set of common volumeMounts.
func sidecarVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      socketVolumeName,
			MountPath: filepath.Dir(endpointInsideContainer),
		},
	}
}

// generateControllerDriverVolumes generates volumes for Controller Driver.
func (r *ReconcileCSI) generateControllerDriverVolumes(csiDeploy *csiv1.CSI) []corev1.Volume {
	return []corev1.Volume{
		{
			Name: socketVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}
}

// generateControllerDriverVM generates volumeMounts for Controller Driver.
func (r *ReconcileCSI) generateControllerDriverVM(csiDeploy *csiv1.CSI) []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      socketVolumeName,
			MountPath: filepath.Dir(endpointInsideContainer),
		},
	}
}

// hostPathTypePtr returns a point of HostPathType.
func hostPathTypePtr(typ corev1.HostPathType) *corev1.HostPathType {
	result := typ
	return &result
}

// sanitizeDriverName sanitizes CSI driver name to be usable as a directory name.
// All dangerous characters are replaced by '-'.
func sanitizeDriverName(driver string) string {
	re := regexp.MustCompile("[^a-zA-Z0-9-.]")
	name := re.ReplaceAllString(driver, "-")
	return name
}

// copySecurityContext copies SecurityContext from templates into a container.
func copySecurityContext(csiDeploy *csiv1.CSI, container *corev1.Container) {
	// Copy Driver container's SecurityContext config.
	securityCtx := csiDeploy.Spec.DriverTemplate.Template.Spec.Containers[0].SecurityContext
	if securityCtx != nil {
		container.SecurityContext = securityCtx.DeepCopy()
	}
}

// endpointENV returns the ENV for CSI endpoint.
func endpointENV() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  endpointENVName,
			Value: "unix:/" + endpointInsideContainer,
		},
	}
}
