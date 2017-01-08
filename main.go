package main

import (
  "log"
  "github.com/jgensler8/firefly/service"
  kubernetes "k8s.io/client-go/kubernetes"
  clientcmd "k8s.io/client-go/tools/clientcmd"
)

const kubeCfgFile = "/Users/genslerj/.kube/config"
const ingressControllerImage = "nginxdemos/nginx-ingress:0.6.0"
const namespace = "applications"

const namespaceYAML = "examples/Namespace.yaml"
const ingressYAML = "examples/Ingress.yaml"
const serviceYAML = "examples/Service.yaml"
const deploymentYAML = "examples/Deployment.yaml"

const ingressShadowYAML = "examples/IngressShadow.yaml"

func main() {
  kubeClientSet, err := newKubeClientset(kubeCfgFile)
	if err != nil {
		log.Fatal(err)
	}

  s := service.ServiceBuilder.
    MaxDepth(5).
    NamespaceYAMLFile(namespaceYAML).
    DeploymentYAMLFile(deploymentYAML).
    ServiceYAMLFile(serviceYAML).
    IngressYAMLFile(ingressYAML).
    IngressShadowYAMLFile(ingressShadowYAML).
    IngressControllerImage(ingressControllerImage).
    Namespace(namespace).
    KubernetesClientSet(*kubeClientSet).
    Build()

  s.Watch()
}

func newKubeClientset(kubeCfgFile string) (*kubernetes.Clientset, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeCfgFile)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return clientset, nil
}
