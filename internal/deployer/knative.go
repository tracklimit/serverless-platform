package deployer

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
	servingclient "knative.dev/serving/pkg/client/clientset/versioned"

	"serverless-platform/internal/model"
)

const (
	runtimeImagePython = "ghcr.io/tracklimit/runtime-python:latest"
	runtimeImageNodeJS = "ghcr.io/tracklimit/runtime-nodejs:latest"

	labelManagedBy  = "app.kubernetes.io/managed-by"
	labelRuntime    = "platform.tracklimit.io/runtime"
	labelDeployType = "platform.tracklimit.io/deploy-type"

	annotationCode = "platform.tracklimit.io/code"
)

type KnativeDeployer struct {
	kubeClient    kubernetes.Interface
	servingClient servingclient.Interface
	namespace     string
}

func NewKnativeDeployer(kubeClient kubernetes.Interface, servingClient servingclient.Interface, namespace string) *KnativeDeployer {
	return &KnativeDeployer{
		kubeClient:    kubeClient,
		servingClient: servingClient,
		namespace:     namespace,
	}
}

func (d *KnativeDeployer) Deploy(ctx context.Context, fn *model.Function) (string, error) {
	serviceName := serviceName(fn.Name)

	if fn.DeployType == model.DeployTypeManaged {
		if err := d.ensureConfigMap(ctx, fn); err != nil {
			return "", fmt.Errorf("create configmap: %w", err)
		}
	}

	ksvc := d.buildService(fn, serviceName)

	existing, err := d.servingClient.ServingV1().Services(d.namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err == nil {
		existing.Spec = ksvc.Spec
		existing.Labels = ksvc.Labels
		existing.Annotations = ksvc.Annotations
		updated, err := d.servingClient.ServingV1().Services(d.namespace).Update(ctx, existing, metav1.UpdateOptions{})
		if err != nil {
			return "", fmt.Errorf("update knative service: %w", err)
		}
		return serviceURL(updated), nil
	}

	created, err := d.servingClient.ServingV1().Services(d.namespace).Create(ctx, ksvc, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("create knative service: %w", err)
	}
	return serviceURL(created), nil
}

func (d *KnativeDeployer) Get(ctx context.Context, name string) (*model.Function, error) {
	svc, err := d.servingClient.ServingV1().Services(d.namespace).Get(ctx, serviceName(name), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get knative service: %w", err)
	}
	return serviceToFunction(svc), nil
}

func (d *KnativeDeployer) List(ctx context.Context) ([]*model.Function, error) {
	list, err := d.servingClient.ServingV1().Services(d.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelManagedBy + "=serverless-platform",
	})
	if err != nil {
		return nil, fmt.Errorf("list knative services: %w", err)
	}

	functions := make([]*model.Function, 0, len(list.Items))
	for i := range list.Items {
		functions = append(functions, serviceToFunction(&list.Items[i]))
	}
	return functions, nil
}

func (d *KnativeDeployer) Delete(ctx context.Context, name string) error {
	svcName := serviceName(name)

	svc, err := d.servingClient.ServingV1().Services(d.namespace).Get(ctx, svcName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("function not found")
		}
		return fmt.Errorf("get knative service: %w", err)
	}

	if err := d.servingClient.ServingV1().Services(d.namespace).Delete(ctx, svcName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete knative service: %w", err)
	}

	if svc.Labels[labelDeployType] == string(model.DeployTypeManaged) {
		cmName := configMapName(name)
		_ = d.kubeClient.CoreV1().ConfigMaps(d.namespace).Delete(ctx, cmName, metav1.DeleteOptions{})
	}

	return nil
}

func (d *KnativeDeployer) InvokeURL(name string) string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local", serviceName(name), d.namespace)
}

func (d *KnativeDeployer) ensureConfigMap(ctx context.Context, fn *model.Function) error {
	cmName := configMapName(fn.Name)
	fileName := codeFileName(fn.Runtime)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: d.namespace,
			Labels: map[string]string{
				labelManagedBy: "serverless-platform",
			},
		},
		Data: map[string]string{
			fileName: fn.Code,
		},
	}

	existing, err := d.kubeClient.CoreV1().ConfigMaps(d.namespace).Get(ctx, cmName, metav1.GetOptions{})
	if err == nil {
		existing.Data = cm.Data
		_, err = d.kubeClient.CoreV1().ConfigMaps(d.namespace).Update(ctx, existing, metav1.UpdateOptions{})
		return err
	}

	_, err = d.kubeClient.CoreV1().ConfigMaps(d.namespace).Create(ctx, cm, metav1.CreateOptions{})
	return err
}

func (d *KnativeDeployer) buildService(fn *model.Function, svcName string) *servingv1.Service {
	podSpec := d.basePodSpec(fn)

	labels := map[string]string{
		labelManagedBy:  "serverless-platform",
		labelRuntime:    string(fn.Runtime),
		labelDeployType: string(fn.DeployType),
	}

	annotations := map[string]string{}
	if fn.DeployType == model.DeployTypeManaged && fn.Code != "" {
		annotations[annotationCode] = fn.Code
	}

	return &servingv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        svcName,
			Namespace:   d.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: servingv1.ServiceSpec{
			ConfigurationSpec: servingv1.ConfigurationSpec{
				Template: servingv1.RevisionTemplateSpec{
					Spec: servingv1.RevisionSpec{
						PodSpec: podSpec,
					},
				},
			},
		},
	}
}

func (d *KnativeDeployer) basePodSpec(fn *model.Function) corev1.PodSpec {
	tolerations := []corev1.Toleration{
		{
			Key:      "node-role.kubernetes.io/workloads",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}

	affinity := &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "node-role.kubernetes.io/workloads",
								Operator: corev1.NodeSelectorOpExists,
							},
						},
					},
				},
			},
		},
	}

	imagePullSecrets := []corev1.LocalObjectReference{
		{Name: "ghcr-pull"},
	}

	if fn.DeployType == model.DeployTypeBYOI {
		return corev1.PodSpec{
			Tolerations:      tolerations,
			Affinity:         affinity,
			ImagePullSecrets: imagePullSecrets,
			Containers: []corev1.Container{
				{Name: "user-function", Image: fn.Image},
			},
		}
	}

	cmName := configMapName(fn.Name)
	return corev1.PodSpec{
		Tolerations:      tolerations,
		Affinity:         affinity,
		ImagePullSecrets: imagePullSecrets,
		Containers: []corev1.Container{
			{
				Name:  "user-function",
				Image: runtimeImage(fn.Runtime),
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "function-code",
						MountPath: "/var/function",
						ReadOnly:  true,
					},
				},
			},
		},
		Volumes: []corev1.Volume{
			{
				Name: "function-code",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: cmName},
					},
				},
			},
		},
	}
}

func serviceToFunction(svc *servingv1.Service) *model.Function {
	fn := &model.Function{
		Name:       svc.Name[3:], // strip "fn-" prefix
		Runtime:    model.Runtime(svc.Labels[labelRuntime]),
		DeployType: model.DeployType(svc.Labels[labelDeployType]),
		CreatedAt:  svc.CreationTimestamp.Time,
		UpdatedAt:  svc.CreationTimestamp.Time,
	}

	if svc.Status.URL != nil {
		fn.URL = svc.Status.URL.String()
	}

	fn.Status = knativeStatus(svc)

	if fn.DeployType == model.DeployTypeBYOI {
		containers := svc.Spec.Template.Spec.Containers
		if len(containers) > 0 {
			fn.Image = containers[0].Image
		}
	}

	for _, cond := range svc.Status.Conditions {
		if cond.LastTransitionTime.Inner.After(fn.UpdatedAt) {
			fn.UpdatedAt = cond.LastTransitionTime.Inner.Time
		}
	}

	return fn
}

func knativeStatus(svc *servingv1.Service) model.Status {
	for _, cond := range svc.Status.Conditions {
		if cond.Type == "Ready" {
			switch {
			case cond.Status == corev1.ConditionTrue:
				return model.StatusReady
			case cond.Status == corev1.ConditionFalse:
				return model.StatusFailed
			default:
				return model.StatusDeploying
			}
		}
	}
	return model.StatusPending
}

func runtimeImage(rt model.Runtime) string {
	switch rt {
	case model.RuntimePython:
		return runtimeImagePython
	case model.RuntimeNodeJS:
		return runtimeImageNodeJS
	default:
		return ""
	}
}

func serviceName(name string) string {
	return "fn-" + name
}

func configMapName(name string) string {
	return "fn-" + name + "-code"
}

func codeFileName(rt model.Runtime) string {
	switch rt {
	case model.RuntimePython:
		return "handler.py"
	case model.RuntimeNodeJS:
		return "index.js"
	default:
		return "handler"
	}
}

func serviceURL(svc *servingv1.Service) string {
	if svc.Status.URL != nil {
		return svc.Status.URL.String()
	}
	return ""
}
