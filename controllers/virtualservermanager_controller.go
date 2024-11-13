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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nginxv1 "virtualservermanager/api/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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

	// 获取 VirtualServerManager 实例
	var manager nginxv1.VirtualServerManager
	if err := r.Get(ctx, req.NamespacedName, &manager); err != nil {
		log.Error(err, "unable to fetch VirtualServerManager")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 获取指定的 VirtualServer
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

	// 更新 VirtualServer 变量
	_, found, err := unstructured.NestedMap(vs.Object, "spec")
	if err != nil || !found {
		log.Error(err, "unable to get VirtualServer spec")
		return ctrl.Result{}, err
	}

	// // 创建metrics客户端
	// config, err := ctrl.GetConfig()
	// if err != nil {
	// 	return ctrl.Result{}, err
	// }
	// metricsClient, err := versioned.NewForConfig(config)
	// if err != nil {
	// 	return ctrl.Result{}, err
	// }

	// // 获取节点CPU使用率
	// nodeMetrics, err := metricsClient.MetricsV1beta1().NodeMetricses().Get(ctx, manager.Spec.NodeName, metav1.GetOptions{})
	// if err != nil {
	// 	log.Error(err, "unable to get node metrics")
	// 	return ctrl.Result{}, err
	// }

	// // 计算CPU使用率百分比
	// cpuUsage := float64(nodeMetrics.Usage.Cpu().MilliValue()) / 10.0 // 转换为百分比

	// // 根据CPU使用率动态调整weight
	// for i := range manager.Spec.Upstreams {
	// 	// 线性调整weight: 100 - cpuUsage
	// 	newWeight := 100 - int(cpuUsage)
	// 	if newWeight < 10 {
	// 		newWeight = 10 // 设置最小值
	// 	}
	// 	manager.Spec.Upstreams[i].Weight = newWeight
	// }

	// 构建 upstreams
	upstreams := make([]interface{}, 0)
	for _, upstream := range manager.Spec.Upstreams {
		upstreamMap := map[string]interface{}{
			"name":    upstream.Name,
			"service": upstream.Service,
			"port":    int64(upstream.Port),
		}
		upstreams = append(upstreams, upstreamMap)
	}

	// 构建 splits
	splits := make([]interface{}, 0)
	for _, upstream := range manager.Spec.Upstreams {
		splitMap := map[string]interface{}{
			"weight": int64(upstream.Weight),
			"action": map[string]interface{}{
				"pass": upstream.Name,
			},
		}
		splits = append(splits, splitMap)
	}

	// 构建 routes
	routes := []interface{}{
		map[string]interface{}{
			"path":   "/",
			"splits": splits,
		},
	}

	// 更新 spec
	if err := unstructured.SetNestedSlice(vs.Object, upstreams, "spec", "upstreams"); err != nil {
		log.Error(err, "unable to set upstreams")
		return ctrl.Result{}, err
	}

	if err := unstructured.SetNestedSlice(vs.Object, routes, "spec", "routes"); err != nil {
		log.Error(err, "unable to set routes")
		return ctrl.Result{}, err
	}

	// 更新 VirtualServer 资源
	if err := r.Update(ctx, &vs); err != nil {
		log.Error(err, "unable to update VirtualServer")
		return ctrl.Result{}, err
	}

	// 为每个upstream创建对应的Deployment
	for _, upstream := range manager.Spec.Upstreams {
		if err := r.createOrUpdateDeployment(ctx, upstream, manager.Spec); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 更新 Status
	manager.Status.Updated = true
	if err := r.Status().Update(ctx, &manager); err != nil {
		log.Error(err, "unable to update VirtualServerManager status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *VirtualServerManagerReconciler) createOrUpdateDeployment(ctx context.Context, upstream nginxv1.Upstream, spec nginxv1.VirtualServerManagerSpec) error {
	log := log.FromContext(ctx)

	// 创建Deployment对象
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
					NodeName: upstream.NodeName, // 指定节点
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

	// 创建或更新Deployment
	if err := r.Create(ctx, deployment); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			log.Error(err, "unable to create Deployment")
			return err
		}
		// 如果已存在则更新
		if err := r.Update(ctx, deployment); err != nil {
			log.Error(err, "unable to update Deployment")
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
