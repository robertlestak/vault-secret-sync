package kubesecret

import (
	"context"
	"strings"

	"github.com/robertlestak/vault-secret-sync/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetSecret(ctx context.Context, namespace string, secretName string) (map[string][]byte, error) {
	gopt := metav1.GetOptions{}
	kc, err := kube.CreateKubeClient()
	if err != nil {
		return nil, err
	}
	var ns, n string
	if strings.Contains(secretName, "/") {
		parts := strings.Split(secretName, "/")
		ns = parts[0]
		n = parts[1]
	} else {
		ns = namespace
		n = secretName
	}
	sc, err := kc.CoreV1().Secrets(ns).Get(ctx, n, gopt)
	if err != nil {
		return nil, err
	}
	// Return the secret
	return sc.Data, nil
}
