/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nginxv1 "virtualserver-manager/api/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
	"k8s.io/utils/pointer"
)

// VirtualServerManagerReconciler reconciles a VirtualServerManager object
type VirtualServerManagerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=nginx.dustinke.me,resources=virtualservermanagers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nginx.dustinke.me,resources=virtualservermanagers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nginx.dustinke.me,resources=virtualservermanagers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the VirtualServerManager object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.1/pkg/reconcile
func (r *VirtualServerManagerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("virtualservermanager", req.NamespacedName)

	// Get VirtualServerManager instance
	var manager nginxv1.VirtualServerManager
	if err := r.Get(ctx, req.NamespacedName, &manager); err != nil {
		log.Error(err, "unable to fetch VirtualServerManager")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Get the specified VirtualServer
	var vs unstructured.Unstructured
	vs.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "k8s.nginx.org",
		Version: "v1",
		Kind:    "VirtualServer",
	})

	if err := r.Get(ctx, types.NamespacedName{
		Name:      manager.Spec.Name,
		Namespace: manager.Spec.Namespace,
	}, &vs); err != nil {
		log.Error(err, "unable to fetch VirtualServer")
		return ctrl.Result{}, err
	}

	// Create Deployment objects for each upstream
	for _, upstream := range manager.Spec.Upstreams {
		if err := r.createOrUpdateDeployment(ctx, upstream, manager.Spec); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.createOrUpdateService(ctx, upstream, manager.Spec.Namespace); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Create metrics client
	config, err := ctrl.GetConfig()
	if err != nil {
		return ctrl.Result{}, err
	}
	metricsClient, err := metricsclientset.NewForConfig(config)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Initialize CPU usage map
	cpuUsageEachNode := make(map[string]float64)

	for _, upstream := range manager.Spec.Upstreams {
		// Get node CPU usage
		nodeMetrics, err := metricsClient.MetricsV1beta1().NodeMetricses().Get(ctx, upstream.NodeName, metav1.GetOptions{})
		if err != nil {
			log.Error(err, "unable to get node metrics")
			return ctrl.Result{}, err
		}

		// CPU usage in nanocores (n)
		cpuQuantity := nodeMetrics.Usage.Cpu()
		// Convert to percentage (1000m = 1 core = 100%)
		cpuUsage := float64(cpuQuantity.MilliValue()) / 10.0
		cpuUsageEachNode[upstream.NodeName] = cpuUsage
	}

	// Calculate total CPU usage
	totalCPUUsage := 0.0
	for _, cpuUsage := range cpuUsageEachNode {
		totalCPUUsage += cpuUsage
	}

	// Calculate CPU usage percentage for each node
	cpuUsagePercentage := make(map[string]int)
	remainingPercentage := 100
	nodeCount := len(cpuUsageEachNode)

	if totalCPUUsage > 0 {
		for nodeName, cpuUsage := range cpuUsageEachNode {
			if nodeCount == 1 {
				cpuUsagePercentage[nodeName] = remainingPercentage
			} else {
				percentage := int((cpuUsage / totalCPUUsage) * 100)
				if percentage > remainingPercentage {
					percentage = remainingPercentage
				}
				cpuUsagePercentage[nodeName] = percentage
				remainingPercentage -= percentage
				nodeCount--
			}
		}
	} else {
		equalShare := 100 / len(cpuUsageEachNode)
		remaining := 100 % len(cpuUsageEachNode)
		for nodeName := range cpuUsageEachNode {
			cpuUsagePercentage[nodeName] = equalShare
			if remaining > 0 {
				cpuUsagePercentage[nodeName]++
				remaining--
			}
		}
	}

	for nodeName, percentage := range cpuUsagePercentage {
		log.Info("Node CPU usage percentage", "node", nodeName, "percentage", percentage)
	}

	// Update VirtualServer variable
	_, found, err := unstructured.NestedMap(vs.Object, "spec")
	if err != nil || !found {
		log.Error(err, "unable to get VirtualServer spec")
		return ctrl.Result{}, err
	}

	// Build upstreams
	upstreams := make([]interface{}, 0)
	for _, upstream := range manager.Spec.Upstreams {
		upstreamMap := map[string]interface{}{
			"name":    upstream.Name,
			"service": upstream.Service,
			"port":    int64(upstream.Port),
		}
		upstreams = append(upstreams, upstreamMap)
	}

	splits := make([]interface{}, 0)
	for _, upstream := range manager.Spec.Upstreams {
		percentage := cpuUsagePercentage[upstream.NodeName]
		newWeight := 100 - percentage
		splitMap := map[string]interface{}{
			"weight": int64(newWeight),
			"action": map[string]interface{}{
				"pass": upstream.Name,
			},
		}
		log.Info("Update node weight", "node", upstream.NodeName, "new weight", newWeight)
		splits = append(splits, splitMap)
	}

	// Build routes
	routes := []interface{}{
		map[string]interface{}{
			"path":   "/",
			"splits": splits,
		},
	}

	// Update spec
	if err := unstructured.SetNestedSlice(vs.Object, upstreams, "spec", "upstreams"); err != nil {
		log.Error(err, "unable to set upstreams")
		return ctrl.Result{}, err
	}

	if err := unstructured.SetNestedSlice(vs.Object, routes, "spec", "routes"); err != nil {
		log.Error(err, "unable to set routes")
		return ctrl.Result{}, err
	}

	// Update VirtualServer resource
	if err := r.Update(ctx, &vs); err != nil {
		log.Error(err, "unable to update VirtualServer")
		return ctrl.Result{}, err
	}

	// Update Status
	manager.Status.Updated = true
	if err := r.Status().Update(ctx, &manager); err != nil {
		log.Error(err, "unable to update VirtualServerManager status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute * 1}, nil
}

func (r *VirtualServerManagerReconciler) createOrUpdateDeployment(ctx context.Context, upstream nginxv1.Upstream, spec nginxv1.VirtualServerManagerSpec) error {
	log := log.FromContext(ctx)

	// Create Deployment object
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      upstream.Name,
			Namespace: spec.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": upstream.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": upstream.Name,
					},
				},
				Spec: corev1.PodSpec{
					NodeName: upstream.NodeName, // Specify node
					Containers: []corev1.Container{
						{
							Name:            "web-container",
							Image:           "leothecat/775-demo:test",
							ImagePullPolicy: corev1.PullAlways,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: int32(upstream.Port),
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "NODE_NAME",
									Value: upstream.Name,
								},
							},
						},
					},
				},
			},
		},
	}

	// Create or update Deployment
	if err := r.Create(ctx, deployment); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			log.Error(err, "unable to create Deployment")
			return err
		}
		// If it already exists, update it
		if err := r.Update(ctx, deployment); err != nil {
			log.Error(err, "unable to update Deployment")
			return err
		}
	}

	return nil
}

func (r *VirtualServerManagerReconciler) createOrUpdateService(ctx context.Context, upstream nginxv1.Upstream, namespace string) error {
	log := log.FromContext(ctx)

	// Create Service object
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      upstream.Name,
			Namespace: namespace,
			Labels: map[string]string{
				"app": upstream.Name,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": upstream.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       int32(upstream.Port),
					TargetPort: intstr.FromInt(upstream.Port),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	// Create or update Service
	if err := r.Create(ctx, service); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			log.Error(err, "unable to create Service")
			return err
		}
		// If it already exists, update it
		if err := r.Update(ctx, service); err != nil {
			log.Error(err, "unable to update Service")
			return err
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VirtualServerManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&nginxv1.VirtualServerManager{}).
		Complete(r)
}
