package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dynastubv1 "httpteststub.example.com/api/v1"
)

// BehaviorStubReconciler reconciles a BehaviorStub object
type BehaviorStubReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=dynastub.example.com,resources=behaviorstubs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=dynastub.example.com,resources=behaviorstubs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *BehaviorStubReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. 获取 BehaviorStub
	behaviorStub := &dynastubv1.BehaviorStub{}
	err := r.Get(ctx, req.NamespacedName, behaviorStub)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch BehaviorStub")
		return ctrl.Result{}, err
	}

	// 2. 根据 targetSelector 查找目标 Pod
	podList := &corev1.PodList{}
	selector, err := metav1.LabelSelectorAsSelector(&behaviorStub.Spec.TargetSelector)
	if err != nil {
		logger.Error(err, "invalid target selector")
		r.updateStatus(ctx, behaviorStub, "Failed", 0, 0)
		return ctrl.Result{}, err
	}

	err = r.List(ctx, podList, client.InNamespace(req.Namespace), client.MatchingLabelsSelector{Selector: selector})
	if err != nil {
		logger.Error(err, "unable to list pods")
		return ctrl.Result{}, err
	}

	// 3. 统计注入状态
	totalPods := int32(len(podList.Items))
	injectedPods := int32(0)

	for i := range podList.Items {
		// 检查 Pod 是否已被注入（通过检查是否有 Sidecar 容器）
		if r.isPodInjected(&podList.Items[i]) {
			injectedPods++
		}
	}

	// 4. 更新 CR 状态
	phase := "Pending"
	if totalPods > 0 {
		if injectedPods == totalPods {
			phase = "Running"
		} else if injectedPods > 0 {
			phase = "Partial"
		}
	}

	err = r.updateStatusWithRetry(ctx, req, phase, injectedPods, totalPods)
	if err != nil {
		logger.Error(err, "unable to update BehaviorStub status")
		return ctrl.Result{}, err
	}

	logger.Info("BehaviorStub reconciled",
		"name", behaviorStub.Name,
		"namespace", behaviorStub.Namespace,
		"phase", phase,
		"injectedPods", injectedPods,
		"totalPods", totalPods,
	)

	return ctrl.Result{}, nil
}

// isPodInjected 检查 Pod 是否已被注入 Sidecar
func (r *BehaviorStubReconciler) isPodInjected(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == "dynastub-sidecar" {
			return true
		}
	}
	// 也检查 initContainers（原生 sidecar 模式）
	for _, container := range pod.Spec.InitContainers {
		if container.Name == "dynastub-sidecar" {
			return true
		}
	}
	return false
}

// updateStatus 更新 BehaviorStub 状态
func (r *BehaviorStubReconciler) updateStatus(ctx context.Context, behaviorStub *dynastubv1.BehaviorStub, phase string, injectedPods, totalPods int32) error {
	behaviorStub.Status.Phase = phase
	behaviorStub.Status.InjectedPods = injectedPods
	behaviorStub.Status.TotalPods = totalPods

	now := metav1.Now()
	behaviorStub.Status.LastUpdateTime = &now

	// 更新条件
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: behaviorStub.Generation,
		LastTransitionTime: now,
		Reason:             "Reconciling",
		Message:            "BehaviorStub is being reconciled",
	}

	if phase == "Running" {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "Running"
		condition.Message = "All target pods have been injected"
	} else if phase == "Partial" {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "Partial"
		condition.Message = "Some target pods have been injected"
	}

	// 更新或添加条件
	found := false
	for i, c := range behaviorStub.Status.Conditions {
		if c.Type == condition.Type {
			behaviorStub.Status.Conditions[i] = condition
			found = true
			break
		}
	}
	if !found {
		behaviorStub.Status.Conditions = append(behaviorStub.Status.Conditions, condition)
	}

	return r.Status().Update(ctx, behaviorStub)
}

func (r *BehaviorStubReconciler) updateStatusWithRetry(ctx context.Context, req ctrl.Request, phase string, injectedPods, totalPods int32) error {
	logger := log.FromContext(ctx)
	for i := 0; i < 3; i++ {
		behaviorStub := &dynastubv1.BehaviorStub{}
		if err := r.Get(ctx, req.NamespacedName, behaviorStub); err != nil {
			return err
		}
		if err := r.updateStatus(ctx, behaviorStub, phase, injectedPods, totalPods); err != nil {
			if errors.IsConflict(err) {
				logger.Info("Conflict updating status, retrying", "attempt", i+1)
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("failed to update status after 3 retries")
}

// SetupWithManager sets up the controller with the Manager.
func (r *BehaviorStubReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dynastubv1.BehaviorStub{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

// GetBehaviorStubByPod 根据 Pod 查找匹配的 BehaviorStub
func GetBehaviorStubByPod(ctx context.Context, c client.Client, pod *corev1.Pod) (*dynastubv1.BehaviorStub, error) {
	behaviorStubList := &dynastubv1.BehaviorStubList{}
	err := c.List(ctx, behaviorStubList, client.InNamespace(pod.Namespace))
	if err != nil {
		return nil, err
	}

	for i := range behaviorStubList.Items {
		bs := &behaviorStubList.Items[i]
		selector, err := metav1.LabelSelectorAsSelector(&bs.Spec.TargetSelector)
		if err != nil {
			continue
		}
		if selector.Matches(labels.Set(pod.Labels)) {
			return bs, nil
		}
	}

	return nil, nil
}
