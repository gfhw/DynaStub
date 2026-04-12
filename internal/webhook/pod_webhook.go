package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	dynastubv1 "httpteststub.example.com/api/v1"
)

// PodMutator 实现 admission.DecoderInjector
type PodMutator struct {
	Client  client.Client
	Decoder admission.Decoder
}

//+kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod.kb.io,admissionReviewVersions=v1

// Handle 处理 Pod 创建请求
func (m *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := log.FromContext(ctx)

	// 安全检查：如果 Decoder 为 nil，手动初始化
	if m.Decoder == nil {
		log.Info("Decoder is nil, initializing manually")
		scheme := runtime.NewScheme()
		_ = corev1.AddToScheme(scheme)
		m.Decoder = admission.NewDecoder(scheme)
	}

	pod := &corev1.Pod{}
	err := m.Decoder.Decode(req, pod)
	if err != nil {
		log.Error(err, "failed to decode pod")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// 检查是否有匹配的 BehaviorStub
	behaviorStub, err := m.findMatchingBehaviorStub(ctx, pod)
	if err != nil {
		log.Error(err, "failed to find matching BehaviorStub")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// 如果没有匹配的 BehaviorStub，不做任何修改
	if behaviorStub == nil {
		return admission.Allowed("No matching BehaviorStub found")
	}

	log.Info("Injecting sidecar for pod",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"behaviorStub", behaviorStub.Name,
	)

	// 注入 Sidecar 和 Volume
	m.injectSidecar(pod, behaviorStub)

	// 序列化修改后的 Pod
	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		log.Error(err, "failed to marshal pod")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

// findMatchingBehaviorStub 查找匹配的 BehaviorStub
func (m *PodMutator) findMatchingBehaviorStub(ctx context.Context, pod *corev1.Pod) (*dynastubv1.BehaviorStub, error) {
	behaviorStubList := &dynastubv1.BehaviorStubList{}
	err := m.Client.List(ctx, behaviorStubList, client.InNamespace(pod.Namespace))
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

// injectSidecar 注入 Sidecar 容器和 Volume
func (m *PodMutator) injectSidecar(pod *corev1.Pod, behaviorStub *dynastubv1.BehaviorStub) {
	log := log.FromContext(context.Background())
	log.Info("[Webhook] Starting sidecar injection",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"behaviorStub", behaviorStub.Name,
	)

	// 1. 添加 emptyDir Volume
	emptyDirVolume := corev1.Volume{
		Name: "dynastub-shared",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, emptyDirVolume)
	log.Info("[Webhook] Added emptyDir volume", "name", "dynastub-shared")

	// 2. 添加 hostPath Volume（只读）
	hostPathVolume := corev1.Volume{
		Name: "dynastub-scripts",
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: behaviorStub.Spec.ScriptVolume.HostPath,
			},
		},
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, hostPathVolume)
	log.Info("[Webhook] Added hostPath volume",
		"name", "dynastub-scripts",
		"hostPath", behaviorStub.Spec.ScriptVolume.HostPath,
	)

	// 3. 添加 Sidecar 容器（使用原生 sidecar 模式）
	sidecarImage := behaviorStub.Spec.SidecarImage
	if sidecarImage == "" {
		sidecarImage = "dynastub-sidecar:latest"
	}

	sidecarContainer := corev1.Container{
		Name:           "dynastub-sidecar",
		Image:          sidecarImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "dynastub-shared",
				MountPath: "/dynastub/bin",
			},
			{
				Name:      "dynastub-scripts",
				MountPath: behaviorStub.Spec.ScriptVolume.MountPath,
				ReadOnly:  true,
			},
		},
		Env: []corev1.EnvVar{
			{
				Name:  "BEHAVIOR_STUB_NAME",
				Value: behaviorStub.Name,
			},
			{
				Name:  "BEHAVIOR_STUB_NAMESPACE",
				Value: behaviorStub.Namespace,
			},
			{
				Name:  "SHARED_DIR",
				Value: "/dynastub/bin",
			},
			{
				Name:  "SCRIPT_MOUNT_PATH",
				Value: behaviorStub.Spec.ScriptVolume.MountPath,
			},
		},
	}

	// 添加资源限制（如果配置了）
	if behaviorStub.Spec.SidecarResources != nil {
		sidecarContainer.Resources = *behaviorStub.Spec.SidecarResources
	}

	// 使用原生 sidecar 模式（K8s 1.28+）
	sidecarContainer.RestartPolicy = func() *corev1.ContainerRestartPolicy {
		p := corev1.ContainerRestartPolicyAlways
		return &p
	}()

	// 将 Sidecar 添加到 initContainers（原生 sidecar 模式）
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, sidecarContainer)
	log.Info("[Webhook] Added sidecar container",
		"name", "dynastub-sidecar",
		"image", sidecarImage,
	)

	// 4. 为业务容器添加 /dynastub/bin 挂载和 PATH 环境变量
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == "dynastub-sidecar" {
			continue
		}

		containerName := pod.Spec.Containers[i].Name
		log.Info("[Webhook] Processing business container", "name", containerName)

		// 添加 /dynastub/bin VolumeMount（只读）
		volumeMount := corev1.VolumeMount{
			Name:      "dynastub-shared",
			MountPath: "/dynastub/bin",
			ReadOnly:  true,
		}
		pod.Spec.Containers[i].VolumeMounts = append(pod.Spec.Containers[i].VolumeMounts, volumeMount)
		log.Info("[Webhook] Added volume mount to business container",
			"container", containerName,
			"mountPath", "/dynastub/bin",
		)

		// 修改 PATH 环境变量，将 /dynastub/bin 放在最前面
		pathEnvFound := false
		originalPath := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

		for j := range pod.Spec.Containers[i].Env {
			if pod.Spec.Containers[i].Env[j].Name == "PATH" {
				originalPath = pod.Spec.Containers[i].Env[j].Value
				pod.Spec.Containers[i].Env[j].Value = "/dynastub/bin:" + originalPath
				pathEnvFound = true
				log.Info("[Webhook] Updated existing PATH",
					"container", containerName,
					"originalPath", originalPath,
					"newPath", pod.Spec.Containers[i].Env[j].Value,
				)
				break
			}
		}

		// 如果没有找到 PATH 环境变量，添加一个
		if !pathEnvFound {
			newPath := "/dynastub/bin:" + originalPath
			pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, corev1.EnvVar{
				Name:  "PATH",
				Value: newPath,
			})
			log.Info("[Webhook] Added new PATH",
				"container", containerName,
				"newPath", newPath,
			)
		}

		// 记录要劫持的命令列表
		var targetCommands []string
		for _, behavior := range behaviorStub.Spec.Behaviors {
			targetCommand := filepath.Base(behavior.TargetPath)
			targetCommands = append(targetCommands, targetCommand)
		}
		log.Info("[Webhook] Configured target commands for hijacking",
			"container", containerName,
			"commands", targetCommands,
		)
	}

	log.Info("[Webhook] Sidecar injection completed successfully",
		"pod", pod.Name,
		"namespace", pod.Namespace,
	)
}

// InjectDecoder injects the decoder.
func (m *PodMutator) InjectDecoder(d admission.Decoder) error {
	m.Decoder = d
	return nil
}

// SetupWebhookWithManager sets up the webhook with the Manager.
func SetupWebhookWithManager(mgr manager.Manager) error {
	mutator := &PodMutator{
		Client: mgr.GetClient(),
	}

	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/mutate-v1-pod", &admission.Webhook{
		Handler: mutator,
	})

	return nil
}
