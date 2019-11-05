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

package types

const (
	// FetchError means fetch the CSIDeployment object failed.
	FetchError = "FetchError"
	// SyncError means sync the CSIDeployment's status failed.
	SyncError = "SyncError"
	// RBACSynced means the rbac resources has been synced.
	RBACSynced = "RBACSynced"
	// SecretsSynced means the secrets has been synced.
	SecretsSynced = "SecretsSynced"
	// StorageClassesSynced means the storageClasses has been synced.
	StorageClassesSynced = "StorageClassesSynced"
	// NodeDriverSynced means the node driver daemonSet has been synced.
	NodeDriverSynced = "NodeDriverSynced"
	// ControllerDriverSynced means the controller driver daemonSet has been synced.
	ControllerDriverSynced = "ControllerDriverSynced"
)
