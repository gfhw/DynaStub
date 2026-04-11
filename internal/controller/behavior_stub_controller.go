package controller

import (
	"context"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dynastubv1 "httpteststub.example.com/api/v1"
)

const (
	behaviorStubFinalizer = "behaviorstub.dynastub.example.com/finalizer"
	webhookName           = "mpod.kb.io"
)

// BehaviorStubReconciler reconciles a BehaviorStub object
type BehaviorStubReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=dynastub.example.com,resources=behaviorstubs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=dynastub.example.com,resources=behaviorstubs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=dynastub.example.com,resources=behaviorstubs/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete

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

	// 2. 处理删除逻辑
	if !behaviorStub.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, behaviorStub)
	}

	// 3. 添加 finalizer
	if !controllerutil.ContainsFinalizer(behaviorStub, behaviorStubFinalizer) {
		controllerutil.AddFinalizer(behaviorStub, behaviorStubFinalizer)
		if err := r.Update(ctx, behaviorStub); err != nil {
			logger.Error(err, "unable to add finalizer")
			return ctrl.Result{}, err
		}
		logger.Info("Added finalizer to BehaviorStub", "name", behaviorStub.Name)
		// 重新获取对象以获取最新的 resourceVersion
		if err := r.Get(ctx, req.NamespacedName, behaviorStub); err != nil {
			logger.Error(err, "unable to fetch BehaviorStub after adding finalizer")
			return ctrl.Result{}, err
		}
	}

	// 4. 确保 webhook 配置存在（第一个 CR 创建时会创建 webhook）
	if err := r.ensureWebhookConfiguration(ctx); err != nil {
		logger.Error(err, "unable to ensure webhook configuration")
		r.Recorder.Eventf(behaviorStub, corev1.EventTypeWarning, "WebhookError",
			"Failed to ensure webhook configuration: %v", err)
		return ctrl.Result{}, err
	}

	// 5. 根据 targetSelector 查找目标 Pod
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

	// 6. 统计注入状态
	totalPods := int32(len(podList.Items))
	injectedPods := int32(0)

	for i := range podList.Items {
		// 检查 Pod 是否已被注入（通过检查是否有 Sidecar 容器）
		if r.isPodInjected(&podList.Items[i]) {
			injectedPods++
		}
	}

	// 7. 更新 CR 状态
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

// reconcileDelete 处理删除逻辑
func (r *BehaviorStubReconciler) reconcileDelete(ctx context.Context, behaviorStub *dynastubv1.BehaviorStub) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling delete for BehaviorStub", "name", behaviorStub.Name, "namespace", behaviorStub.Namespace)

	// 检查是否还有其他 CR
	behaviorStubList := &dynastubv1.BehaviorStubList{}
	if err := r.List(ctx, behaviorStubList); err != nil {
		logger.Error(err, "unable to list BehaviorStubs")
		return ctrl.Result{}, err
	}

	// 如果这是最后一个 CR，删除 webhook 配置
	if len(behaviorStubList.Items) <= 1 {
		logger.Info("This is the last BehaviorStub, deleting webhook configuration")
		if err := r.deleteWebhookConfiguration(ctx); err != nil {
			logger.Error(err, "unable to delete webhook configuration")
			return ctrl.Result{}, err
		}
		logger.Info("Webhook configuration deleted successfully")
	} else {
		logger.Info("Other BehaviorStubs exist, keeping webhook configuration",
			"remainingCount", len(behaviorStubList.Items)-1)
	}

	// 移除 finalizer
	controllerutil.RemoveFinalizer(behaviorStub, behaviorStubFinalizer)
	if err := r.Update(ctx, behaviorStub); err != nil {
		logger.Error(err, "unable to remove finalizer")
		return ctrl.Result{}, err
	}

	logger.Info("Finalizer removed, BehaviorStub can be deleted")
	return ctrl.Result{}, nil
}

// ensureWebhookConfiguration 确保 webhook 配置存在
func (r *BehaviorStubReconciler) ensureWebhookConfiguration(ctx context.Context) error {
	logger := log.FromContext(ctx)

	webhook := &admissionregistrationv1.MutatingWebhookConfiguration{}
	err := r.Get(ctx, client.ObjectKey{Name: "dynastub-k8s-http-fake-operator-webhook"}, webhook)
	if err == nil {
		// Webhook 已存在
		return nil
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get webhook configuration: %w", err)
	}

	// Webhook 不存在，创建它
	logger.Info("Creating MutatingWebhookConfiguration")

	// 读取 CA 证书（对于自签名证书，tls.crt 就是 CA 证书）
	certPath := "/tmp/k8s-webhook-server/serving-certs/tls.crt"
	caCert, err := os.ReadFile(certPath)
	if err != nil {
		logger.Info("Failed to read cert, using empty CABundle", "path", certPath, "error", err)
		caCert = []byte{}
	} else {
		logger.Info("Successfully read cert", "path", certPath, "certLength", len(caCert))
	}

	// 获取 webhook service 的 namespace 和证书信息
	// 这里假设 service 和 secret 已经由 Helm 创建
	webhook = &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dynastub-k8s-http-fake-operator-webhook",
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "dynastub-operator",
			},
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{
				Name: webhookName,
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					Service: &admissionregistrationv1.ServiceReference{
						Name:      "k8s-http-fake-operator-webhook",
						Namespace: "default",
						Path:      strPtr("/mutate-v1-pod"),
						Port:      int32Ptr(443),
					},
					CABundle: caCert,
				},
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
							admissionregistrationv1.Update,
						},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
							Scope:       scopePtr(admissionregistrationv1.NamespacedScope),
						},
					},
				},
				NamespaceSelector:       &metav1.LabelSelector{},
				FailurePolicy:           failurePolicyPtr(admissionregistrationv1.Ignore),
				SideEffects:             sideEffectPtr(admissionregistrationv1.SideEffectClassNone),
				AdmissionReviewVersions: []string{"v1"},
				TimeoutSeconds:          int32Ptr(30),
			},
		},
	}

	if err := r.Create(ctx, webhook); err != nil {
		return fmt.Errorf("failed to create webhook configuration: %w", err)
	}

	logger.Info("MutatingWebhookConfiguration created successfully")
	return nil
}

// deleteWebhookConfiguration 删除 webhook 配置
func (r *BehaviorStubReconciler) deleteWebhookConfiguration(ctx context.Context) error {
	logger := log.FromContext(ctx)

	webhook := &admissionregistrationv1.MutatingWebhookConfiguration{}
	err := r.Get(ctx, client.ObjectKey{Name: "dynastub-k8s-http-fake-operator-webhook"}, webhook)
	if err != nil {
		if errors.IsNotFound(err) {
			// Webhook 不存在，无需删除
			return nil
		}
		return fmt.Errorf("failed to get webhook configuration: %w", err)
	}

	// 检查是否由 operator 管理
	if webhook.Labels["app.kubernetes.io/managed-by"] != "dynastub-operator" {
		logger.Info("Webhook not managed by operator, skipping deletion")
		return nil
	}

	if err := r.Delete(ctx, webhook); err != nil {
		return fmt.Errorf("failed to delete webhook configuration: %w", err)
	}

	logger.Info("MutatingWebhookConfiguration deleted successfully")
	return nil
}

// Helper functions
func strPtr(s string) *string {
	return &s
}

func int32Ptr(i int32) *int32 {
	return &i
}

func scopePtr(s admissionregistrationv1.ScopeType) *admissionregistrationv1.ScopeType {
	return &s
}

func failurePolicyPtr(f admissionregistrationv1.FailurePolicyType) *admissionregistrationv1.FailurePolicyType {
	return &f
}

func sideEffectPtr(s admissionregistrationv1.SideEffectClass) *admissionregistrationv1.SideEffectClass {
	return &s
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
