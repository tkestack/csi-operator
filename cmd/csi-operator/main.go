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
	"flag"

	"tkestack.io/csi-operator/pkg/apis"
	"tkestack.io/csi-operator/pkg/controller"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"
	"tkestack.io/csi-operator/pkg/config"
)

var (
	master = flag.String("master", "",
		"Master URL to build a client config from. Either this or kubeconfig needs to be set if the provisioner "+
			"is being run out of cluster.")
	kubeConfig = flag.String("kubeconfig", "", "Absolute path to the kubeconfig")

	createCRD = flag.Bool("create-crd", true, "Create the crd when operator started.")

	leaderElection   = flag.Bool("leader-election", false, "Enable leader election.")
	leaderElectionID = flag.String("leader-election-id", "csi-operator",
		"Unique identity of the leader election object")
	leaderElectionNamespace = flag.String("leader-election-namespace", "kube-system",
		"Namespace where this operator runs.")
	metricsAddr = flag.String("metrics-addr", ":1234",
		"The address the metric endpoint binds to.")
)

// main func.
func main() {
	klog.InitFlags(nil)
	cfg := &config.Config{}
	cfg.AddFlags()

	flag.Parse()

	klog.Infof("Config: %+v", cfg)

	// Get a config to talk to the apiserver
	klog.Info("setting up client for csi-operator")
	kubeConfig, err := newK8sConfig()
	if err != nil {
		klog.Fatalf("Unable to set up client config: %v", err)
	}

	if *createCRD {
		if crdErr := syncCRD(kubeConfig); crdErr != nil {
			klog.Fatalf("Sync CRD failed: %v", crdErr)
		}
	}

	// Create a new Cmd to provide shared dependencies and start components
	klog.Info("setting up csi-operator")
	mgr, err := manager.New(kubeConfig, manager.Options{
		LeaderElection:          *leaderElection,
		LeaderElectionID:        *leaderElectionID,
		LeaderElectionNamespace: *leaderElectionNamespace,
		MetricsBindAddress:      *metricsAddr,
	})
	if err != nil {
		klog.Fatalf("Unable to set up overall controller csi-operator: %v", err)
	}

	klog.Info("Registering Components.")

	// Setup Scheme for all resources
	klog.Info("setting up scheme")
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Fatalf("Unable add APIs to scheme: %s", err)
	}

	// Setup all Controllers
	klog.Info("Setting up controller")
	if err := controller.AddToManager(mgr, cfg); err != nil {
		klog.Fatalf("Unable to register controllers to the csi-operator: %v", err)
	}

	// Start the Cmd
	klog.Info("Starting the Operator.")
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		klog.Fatalf("Unable to run the csi-operator: %v", err)
	}
}

// newK8sConfig generates the config of k8s cluster.
func newK8sConfig() (*rest.Config, error) {
	if *master != "" || *kubeConfig != "" {
		return clientcmd.BuildConfigFromFlags(*master, *kubeConfig)
	}
	return rest.InClusterConfig()
}
