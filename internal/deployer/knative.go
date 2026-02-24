package deployer

import (
	"bytes"
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
)

type KnativeDeployer struct {
	kubeClient    kubernetes.Interface
	servingClient servingclient.Interface
}

func NewKnativeDeployer(kubeClient kubernetes.Interface, servingClient servingclient.Interface) *KnativeDeployer {
	return &KnativeDeployer{
		kubeClient:    kubeClient,
		servingClient: servingClient,
	}
}

func (d *KnativeDeployer) EnsureNamespace(ctx context.Context, namespace string) error {
	_, err := d.kubeClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("check namespace: %w", err)
	}

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
			Labels: map[string]string{
				labelManagedBy: "serverless-platform",
			},
		},
	}
	_, err = d.kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	return err
}

func (d *KnativeDeployer) EnsureNamespaceRBAC(ctx context.Context, namespace string) error {
	roleName := "serverless-platform-deployer"

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"serving.knative.dev"},
				Resources: []string{"services"},
				Verbs:     []string{"get", "list", "create", "update", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"get", "list", "create", "update", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods/log"},
				Verbs:     []string{"get"},
			},
		},
	}

	_, err := d.kubeClient.RbacV1().Roles(namespace).Get(ctx, roleName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		if _, err := d.kubeClient.RbacV1().Roles(namespace).Create(ctx, role, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create role: %w", err)
		}
	}

	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: namespace,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     roleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "serverless-platform",
				Namespace: "serverless-platform",
			},
		},
	}

	_, err = d.kubeClient.RbacV1().RoleBindings(namespace).Get(ctx, roleName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		if _, err := d.kubeClient.RbacV1().RoleBindings(namespace).Create(ctx, binding, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create role binding: %w", err)
		}
	}

	return nil
}

func (d *KnativeDeployer) Deploy(ctx context.Context, namespace string, fn *model.Function) (string, error) {
	svcName := serviceName(fn.Name)

	if fn.DeployType == model.DeployTypeManaged {
		if err := d.ensureConfigMap(ctx, namespace, fn); err != nil {
			return "", fmt.Errorf("create configmap: %w", err)
		}
	}

	ksvc := d.buildService(namespace, fn, svcName)

	existing, err := d.servingClient.ServingV1().Services(namespace).Get(ctx, svcName, metav1.GetOptions{})
	if err == nil {
		existing.Spec = ksvc.Spec
		existing.Labels = ksvc.Labels
		if existing.Annotations == nil {
			existing.Annotations = map[string]string{}
		}
		for k, v := range ksvc.Annotations {
			existing.Annotations[k] = v
		}
		updated, err := d.servingClient.ServingV1().Services(namespace).Update(ctx, existing, metav1.UpdateOptions{})
		if err != nil {
			return "", fmt.Errorf("update knative service: %w", err)
		}
		return serviceURL(updated), nil
	}

	created, err := d.servingClient.ServingV1().Services(namespace).Create(ctx, ksvc, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("create knative service: %w", err)
	}
	return serviceURL(created), nil
}

func (d *KnativeDeployer) Get(ctx context.Context, namespace, name string) (*model.Function, error) {
	svc, err := d.servingClient.ServingV1().Services(namespace).Get(ctx, serviceName(name), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get knative service: %w", err)
	}
	return serviceToFunction(svc), nil
}

func (d *KnativeDeployer) List(ctx context.Context, namespace string) ([]*model.Function, error) {
	list, err := d.servingClient.ServingV1().Services(namespace).List(ctx, metav1.ListOptions{
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

func (d *KnativeDeployer) Delete(ctx context.Context, namespace, name string) error {
	svcName := serviceName(name)

	svc, err := d.servingClient.ServingV1().Services(namespace).Get(ctx, svcName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("function not found")
		}
		return fmt.Errorf("get knative service: %w", err)
	}

	if err := d.servingClient.ServingV1().Services(namespace).Delete(ctx, svcName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete knative service: %w", err)
	}

	if svc.Labels[labelDeployType] == string(model.DeployTypeManaged) {
		_ = d.kubeClient.CoreV1().ConfigMaps(namespace).Delete(ctx, configMapName(name), metav1.DeleteOptions{})
	}

	return nil
}

func (d *KnativeDeployer) InvokeURL(namespace, name string) string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local", serviceName(name), namespace)
}

func (d *KnativeDeployer) Logs(ctx context.Context, namespace, name string, tail int64) (string, error) {
	pods, err := d.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "serving.knative.dev/service=" + serviceName(name),
	})
	if err != nil {
		return "", fmt.Errorf("list pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return "", nil
	}

	latest := pods.Items[0]
	for _, p := range pods.Items[1:] {
		if p.CreationTimestamp.After(latest.CreationTimestamp.Time) {
			latest = p
		}
	}

	req := d.kubeClient.CoreV1().Pods(namespace).GetLogs(latest.Name, &corev1.PodLogOptions{
		Container: "user-function",
		TailLines: &tail,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("stream logs: %w", err)
	}
	defer func() { _ = stream.Close() }()

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(stream); err != nil {
		return "", fmt.Errorf("read logs: %w", err)
	}
	return buf.String(), nil
}

func (d *KnativeDeployer) ensureConfigMap(ctx context.Context, namespace string, fn *model.Function) error {
	cmName := configMapName(fn.Name)
	fileName := codeFileName(fn.Runtime)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: namespace,
			Labels: map[string]string{
				labelManagedBy: "serverless-platform",
			},
		},
		Data: map[string]string{
			fileName: fn.Code,
		},
	}

	existing, err := d.kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, cmName, metav1.GetOptions{})
	if err == nil {
		existing.Data = cm.Data
		_, err = d.kubeClient.CoreV1().ConfigMaps(namespace).Update(ctx, existing, metav1.UpdateOptions{})
		return err
	}

	_, err = d.kubeClient.CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{})
	return err
}

func (d *KnativeDeployer) buildService(namespace string, fn *model.Function, svcName string) *servingv1.Service {
	podSpec := d.basePodSpec(fn)

	labels := map[string]string{
		labelManagedBy:  "serverless-platform",
		labelRuntime:    string(fn.Runtime),
		labelDeployType: string(fn.DeployType),
	}

	return &servingv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: namespace,
			Labels:    labels,
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
