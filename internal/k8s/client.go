package k8s

import (
	"context"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Client wraps the Kubernetes clientset.
type Client struct {
	Clientset kubernetes.Clientset
}

func GetKubernetesClient(kubeconfigPath string) kubernetes.Clientset {

	var kubeconfig *rest.Config

	if kubeconfigPath == "" {
		kubeconfigPath = filepath.Join(homedir.HomeDir(), ".kube", "config")
	}

	kubeconfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	return *clientset
}

func CreateNodePortServiceTCP(clientset *kubernetes.Clientset, namespace string, appName string, serviceName string, podPort int, nodePort int) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Selector: map[string]string{
				"app": appName,
			},
			Ports: []corev1.ServicePort{
				{
					TargetPort: intstr.FromInt(podPort),
					NodePort:   int32(nodePort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	_, err := clientset.CoreV1().Services(namespace).Create(context.Background(), service, metav1.CreateOptions{})
	return err
}
