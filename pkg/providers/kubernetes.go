package providers

import (
	"context"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/Masterminds/semver"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/karina-ui/pkg/api"
	"github.com/flanksource/kommons"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Kubernetes struct {
	clients map[string]*kommons.Client
}

func NewKubernetes(config api.Config) (Provider, error) {
	clients := map[string]*kommons.Client{}

	for name, cluster := range config.Clusters {
		kubeConfigBytes, err := ioutil.ReadFile(cluster.Kubeconfig)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read kubeconfig %s for cluster %s", cluster.Kubeconfig, name)
		}

		k8sClient, err := kommons.NewClientFromBytes(kubeConfigBytes)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get kubernetes client for cluster %s", name)
		}

		clients[name] = k8sClient
	}

	k8s := &Kubernetes{
		clients: clients,
	}
	return k8s, nil
}

func (k *Kubernetes) Fetch(cluster *api.Cluster, config api.ClusterConfiguration) error {
	client, found := k.clients[cluster.Name]
	if !found {
		return errors.Errorf("kubernetes client for cluster %s not found", cluster.Name)
	}

	clientset, err := client.GetClientset()
	if err != nil {
		return errors.Errorf("failed to get clientset for cluster %s", cluster.Name)
	}

	nodes, err := clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.Errorf("failed to list nodes")
	}

	var cpus, memory, diskSize int64
	var nodesList = []api.Node{}

	for _, node := range nodes.Items {
		cpus += node.Status.Capacity.Cpu().Value()
		memory += node.Status.Capacity.Memory().Value()
		diskSize += node.Status.Capacity.StorageEphemeral().Value()

		nodesList = append(nodesList, getNodeInfo(node))
	}

	knv, kernelAlerts := addAlerts(nodesList, "Kernel", kernelVersion)
	kbv, kubeletAlerts := addAlerts(nodesList, "Kubelet", kubeletVersion)
	crv, criAlerts := addAlerts(nodesList, "CRI", criVersion)
	osv, osAlerts := addAlerts(nodesList, "OS", osVersion)

	cluster.Kubernetes = api.Kubernetes{
		CPU:            int(cpus),
		Memory:         memory,
		Disk:           diskSize,
		KubeletVersion: kbv,
		KernelVersion:  knv,
		OSVersion:      osv,
		CRIVersion:     crv,
		KubeletAlerts:  kubeletAlerts,
		KernelAlerts:   kernelAlerts,
		OSAlerts:       osAlerts,
		CRIAlerts:      criAlerts,
		Nodes:          nodesList,
	}

	return nil
}

func (k *Kubernetes) Name() string {
	return "kubernetes"
}

func getNodeInfo(node v1.Node) api.Node {
	var internalIP, externalIP string

	for _, address := range node.Status.Addresses {
		if address.Type == v1.NodeInternalIP {
			internalIP = address.Address
		}
		if address.Type == v1.NodeExternalIP {
			externalIP = address.Address
		}
	}

	n := api.Node{
		Name:             node.Name,
		InternalIP:       internalIP,
		ExternalIP:       externalIP,
		KernelVersion:    node.Status.NodeInfo.KernelVersion,
		KubeletVersion:   node.Status.NodeInfo.KubeletVersion,
		OSImage:          node.Status.NodeInfo.OSImage,
		ContainerRuntime: node.Status.NodeInfo.ContainerRuntimeVersion,
	}

	return n
}

type versionGetterFn func(api.Node) string

func addAlerts(nodes []api.Node, component string, fn versionGetterFn) (string, []api.Alert) {
	latestVersionSemver, _ := semver.NewVersion("v0.0.0")
	latestVersion := "v0"
	alerts := []api.Alert{}

	if len(nodes) == 0 {
		return "", alerts
	}

	for _, node := range nodes {
		version := fn(node)
		sv, err := semver.NewVersion(version)
		if err != nil {
			logger.Errorf("could not parse version %s: %v", version, err)
			return "", alerts
		}

		if sv.GreaterThan(latestVersionSemver) {
			latestVersionSemver = sv
			latestVersion = version
		}
	}

	for index, node := range nodes {
		version := fn(node)
		sv, _ := semver.NewVersion(version)

		if sv.LessThan(latestVersionSemver) {
			alert := api.Alert{
				Level:   "warning",
				Message: fmt.Sprintf("%s version %s is behind latest version: %s", component, version, latestVersion),
			}
			nodes[index].Alerts = append(nodes[index].Alerts, alert)

			alert = api.Alert{
				Level:   "warning",
				Message: fmt.Sprintf("Node %s with version %s is behind latest version: %s", node.Name, version, latestVersion),
			}
			alerts = append(alerts, alert)
		}
	}

	return latestVersion, alerts
}

func criVersion(node api.Node) string {
	version := node.ContainerRuntime
	if strings.HasPrefix(version, "containerd://") {
		return version[13:]
	}
	if strings.HasPrefix(version, "docker://") {
		return version[9:]
	}
	return version
}

func osVersion(node api.Node) string {
	version := node.OSImage
	if strings.HasPrefix(version, "Ubuntu") {
		parts := strings.Split(version, " ")
		if len(parts) < 2 {
			return version
		}
		return parts[1]
	}
	return version
}

func kernelVersion(node api.Node) string {
	return node.KernelVersion
}

func kubeletVersion(node api.Node) string {
	return node.KubeletVersion
}
