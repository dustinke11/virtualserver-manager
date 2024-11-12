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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nginxv1 "virtualservermanager/api/v1"
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

	// 更新 Status
	manager.Status.Updated = true
	if err := r.Status().Update(ctx, &manager); err != nil {
		log.Error(err, "unable to update VirtualServerManager status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VirtualServerManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&nginxv1.VirtualServerManager{}).
		Complete(r)
}
