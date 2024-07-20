package kube

import (
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func CreateKubeClient() (*kubernetes.Clientset, error) {
	l := log.WithFields(
		log.Fields{
			"action": "createKubeClient",
		},
	)
	l.Debug("get createKubeClient")
	var kubeconfig string
	var err error
	if os.Getenv("KUBECONFIG") != "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	} else if home := homedir.HomeDir(); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	}
	var config *rest.Config
	// naïvely assume if no kubeconfig file that we are running in cluster
	if _, err := os.Stat(kubeconfig); os.IsNotExist(err) {
		config, err = rest.InClusterConfig()
		if err != nil {
			l.Debugf("res.InClusterConfig error=%v", err)
			return nil, err
		}
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			l.Debugf("clientcmd.BuildConfigFromFlags error=%v", err)
			return nil, err
		}
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		l.Debugf("kubernetes.NewForConfig error=%v", err)
		return nil, err
	}
	return client, nil
}
