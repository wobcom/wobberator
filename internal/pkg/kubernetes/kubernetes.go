package kubernetes

import (
	"fmt"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"log/slog"
	"os"
	"path/filepath"
)

func GetClient() (*kubernetes.Clientset, error) {

	// Note:
	// We try to do an InClusterConfig first. If this fails, we check for a .kube/config file.
	// If this also does not exist, we raise an error.

	config, err := rest.InClusterConfig()
	if err == nil {
		slog.Info("InClusterConfig worked, authenticated within a Kubernetes cluster...")
		return kubernetes.NewForConfig(config)
	}

	slog.Info("InClusterConfig failed, trying to find a .kube/config or loading the environment variable")

	var kubeconfigPath string
	if os.Getenv("KUBECONFIG") != "" {
		slog.Info("Using KUBECONFIG")
		envPath := os.Getenv("KUBECONFIG")
		kubeconfigPath = envPath
	} else if home := homedir.HomeDir(); home != "" {
		slog.Info("Using .kube/config")
		homePath := filepath.Join(home, ".kube", "config")
		kubeconfigPath = homePath
	} else {
		return nil, fmt.Errorf("Unable to find a kubeconfig")
	}

	config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, err
	}

	slog.Info("OutOfClusterConfig worked...")

	return kubernetes.NewForConfig(config)
}
