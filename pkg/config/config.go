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

package config

import (
	"flag"
)

// Config is a bunch of global configurable parameters.
type Config struct {
	CephConfig
	TencentCloudConfig
	// Path to Kubelet's root dir.
	KubeletRootDir string
	// Domain of the image registry.
	RegistryDomain string
	// Supported file system for block storage, such as CephRBD, CBS.
	Filesystems string
	// NeedDefaultSC indicates whether the cluster need to create default storage classes.
	NeedDefaultSc bool
}

// AddFlags add the configurations to global flag.
func (config *Config) AddFlags() {
	flag.StringVar(&config.KubeletRootDir, "kubelet-root-dir",
		"/var/lib/kubelet", "Path to Kubelet's root dir")
	flag.StringVar(&config.RegistryDomain, "registry-domain",
		"ccr.ccs.tencentyun.com/tke3/library", "Domain of the image registry")
	flag.StringVar(&config.Filesystems, "file-systems",
		"xfs,ext4", "Supported file systems for well known block volumes")
	flag.BoolVar(&config.NeedDefaultSc, "need-default-sc", true,
		"NeedDefaultSC indicates whether the cluster need to create default storage classes")
	config.CephConfig.AddFlags()
	config.TencentCloudConfig.AddFlags()
}

// CephConfig is a bunch of global configurations of Ceph cluster.
type CephConfig struct {
	// Monitor addresses of Ceph cluster.
	Monitors string
	// ID of Ceph admin user.
	AdminID string
	// Key of Ceph admin user.
	AdminKey string
}

// AddFlags add the ceph configurations to global flag.
func (c *CephConfig) AddFlags() {
	flag.StringVar(&c.Monitors, "ceph-monitors", "", "Monitor addresses of Ceph cluster")
	flag.StringVar(&c.AdminID, "ceph-admin-id", "admin", "ID of Ceph admin user")
	flag.StringVar(&c.AdminKey, "ceph-admin-key", "", "Key of Ceph admin user")
}

// TencentCloudConfig is a bunch of global configurations of TencentCloud cluster.
type TencentCloudConfig struct {
	// Secret ID of Tencent Cloud
	SecretID string
	// Secret Key of Tencent Cloud
	SecretKey string
}

// AddFlags add the TencentCloud configurations to global flag.
func (c *TencentCloudConfig) AddFlags() {
	flag.StringVar(&c.SecretID, "tencent-cloud-secret-id", "", "API Secret ID of Tencent Cloud")
	flag.StringVar(&c.SecretKey, "tencent-cloud-secret-key", "", "API Secret Key of Tencent Cloud")
}
