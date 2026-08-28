package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	workloadsv1alpha1 "github.com/opensoha/soha-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	conditionReady       = "Ready"
	sourceReferenceIndex = "spec.sourceRef"
	managedByLabel       = "app.kubernetes.io/managed-by"
	ownerUIDLabel        = "workloads.soha.io/owner-uid"
)

var (
	errSourceContainerNotFound = errors.New("source container not found")
	errSourceVolumeNotFound    = errors.New("source volume not found")
	errTargetContainerNotFound = errors.New("target container not found")
	errInvalidCronJobSpec      = errors.New("invalid CronJob spec")
	errTargetConflict          = errors.New("target CronJob is not controlled by this WorkloadCronJob")
	errTargetDeleting          = errors.New("target CronJob is being deleted")
)

type observedSource struct {
	UID             string
	ResourceVersion string
	Container       corev1.Container
	Volumes         []corev1.Volume
}

// WorkloadCronJobReconciler reconciles a WorkloadCronJob into one native CronJob.
type WorkloadCronJobReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=workloads.soha.io,resources=workloadcronjobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=workloads.soha.io,resources=workloadcronjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;daemonsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update

func (r *WorkloadCronJobReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	var resource workloadsv1alpha1.WorkloadCronJob
	if err := r.Get(ctx, request.NamespacedName, &resource); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	source, err := r.observeSource(ctx, &resource)
	if err != nil {
		reason, message, retryErr := classifySourceError(err)
		target, suspendErr := r.suspendOwnedCronJob(ctx, &resource)
		if errors.Is(suspendErr, errTargetConflict) {
			reason, message, retryErr, suspendErr = "TargetConflict", suspendErr.Error(), nil, nil
		}
		statusErr := r.updateStatus(ctx, &resource, source, target, metav1.ConditionFalse, reason, message)
		return ctrl.Result{}, errors.Join(retryErr, suspendErr, statusErr)
	}

	desired, err := r.desiredCronJob(&resource, source)
	if err != nil {
		target, suspendErr := r.suspendOwnedCronJob(ctx, &resource)
		reason, message := "InvalidSpec", err.Error()
		if errors.Is(suspendErr, errTargetConflict) {
			reason, message, suspendErr = "TargetConflict", suspendErr.Error(), nil
		}
		statusErr := r.updateStatus(ctx, &resource, source, target, metav1.ConditionFalse, reason, message)
		return ctrl.Result{}, errors.Join(suspendErr, statusErr)
	}

	target, err := r.ensureCronJob(ctx, &resource, desired)
	if err != nil {
		reason, retryErr := "ReconcileError", err
		if errors.Is(err, errTargetConflict) {
			reason, retryErr = "TargetConflict", nil
		}
		if errors.Is(err, errTargetDeleting) {
			reason = "TargetDeleting"
		}
		statusErr := r.updateStatus(ctx, &resource, source, nil, metav1.ConditionFalse, reason, err.Error())
		return ctrl.Result{}, errors.Join(retryErr, statusErr)
	}

	return ctrl.Result{}, r.updateStatus(
		ctx,
		&resource,
		source,
		target,
		metav1.ConditionTrue,
		"Reconciled",
		fmt.Sprintf("CronJob %s/%s follows runtime fields from %s %s/%s container %s", resource.Namespace, resource.Name, resource.Spec.SourceRef.Kind, resource.Namespace, resource.Spec.SourceRef.Name, resource.Spec.SourceRef.Container),
	)
}

func classifySourceError(err error) (reason, message string, retryErr error) {
	switch {
	case apierrors.IsNotFound(err):
		return "SourceNotFound", err.Error(), nil
	case errors.Is(err, errSourceContainerNotFound):
		return "SourceContainerNotFound", err.Error(), nil
	case errors.Is(err, errSourceVolumeNotFound):
		return "SourceVolumeNotFound", err.Error(), nil
	case errors.Is(err, errInvalidCronJobSpec):
		return "InvalidSpec", err.Error(), nil
	default:
		return "ReconcileError", err.Error(), err
	}
}

func (r *WorkloadCronJobReconciler) observeSource(ctx context.Context, resource *workloadsv1alpha1.WorkloadCronJob) (observedSource, error) {
	key := types.NamespacedName{Namespace: resource.Namespace, Name: resource.Spec.SourceRef.Name}
	switch resource.Spec.SourceRef.Kind {
	case workloadsv1alpha1.WorkloadKindDeployment:
		var source appsv1.Deployment
		if err := r.Get(ctx, key, &source); err != nil {
			return observedSource{}, err
		}
		return observedSourceFrom(source.ObjectMeta, source.Spec.Template.Spec, resource.Spec.SourceRef.Container)
	case workloadsv1alpha1.WorkloadKindStatefulSet:
		var source appsv1.StatefulSet
		if err := r.Get(ctx, key, &source); err != nil {
			return observedSource{}, err
		}
		return observedSourceFrom(source.ObjectMeta, source.Spec.Template.Spec, resource.Spec.SourceRef.Container)
	case workloadsv1alpha1.WorkloadKindDaemonSet:
		var source appsv1.DaemonSet
		if err := r.Get(ctx, key, &source); err != nil {
			return observedSource{}, err
		}
		return observedSourceFrom(source.ObjectMeta, source.Spec.Template.Spec, resource.Spec.SourceRef.Container)
	default:
		return observedSource{}, fmt.Errorf("%w: unsupported source kind %q", errInvalidCronJobSpec, resource.Spec.SourceRef.Kind)
	}
}

func observedSourceFrom(metadata metav1.ObjectMeta, podSpec corev1.PodSpec, containerName string) (observedSource, error) {
	state := observedSource{UID: string(metadata.UID), ResourceVersion: metadata.ResourceVersion}
	for _, container := range podSpec.Containers {
		if container.Name == containerName {
			state.Container = *container.DeepCopy()
			volumes, err := referencedVolumes(podSpec.Volumes, container)
			state.Volumes = volumes
			return state, err
		}
	}
	return state, fmt.Errorf("%w: %q", errSourceContainerNotFound, containerName)
}

func referencedVolumes(volumes []corev1.Volume, container corev1.Container) ([]corev1.Volume, error) {
	required := containerVolumeNames(container)
	available := make(map[string]int, len(volumes))
	for i := range volumes {
		available[volumes[i].Name] = i
	}
	for name := range required {
		if _, ok := available[name]; !ok {
			return nil, fmt.Errorf("%w: %q", errSourceVolumeNotFound, name)
		}
	}
	selected := make([]corev1.Volume, 0, len(required))
	for i := range volumes {
		if _, ok := required[volumes[i].Name]; ok {
			selected = append(selected, *volumes[i].DeepCopy())
		}
	}
	return selected, nil
}

func (r *WorkloadCronJobReconciler) desiredCronJob(resource *workloadsv1alpha1.WorkloadCronJob, source observedSource) (*batchv1.CronJob, error) {
	spec := resource.Spec.CronJobSpec.DeepCopy()
	if strings.TrimSpace(spec.Schedule) == "" {
		return nil, fmt.Errorf("%w: schedule is required", errInvalidCronJobSpec)
	}
	targetIndex := slices.IndexFunc(spec.JobTemplate.Spec.Template.Spec.Containers, func(container corev1.Container) bool {
		return container.Name == resource.Spec.TargetContainer
	})
	if targetIndex < 0 {
		return nil, fmt.Errorf("%w: %q", errTargetContainerNotFound, resource.Spec.TargetContainer)
	}
	podSpec := &spec.JobTemplate.Spec.Template.Spec
	targetContainer := &podSpec.Containers[targetIndex]
	previousVolumeNames := containerVolumeNames(*targetContainer)
	sourceContainer := source.Container.DeepCopy()
	targetContainer.Image = sourceContainer.Image
	targetContainer.Env = sourceContainer.Env
	targetContainer.EnvFrom = sourceContainer.EnvFrom
	targetContainer.VolumeMounts = sourceContainer.VolumeMounts
	volumes, err := mergeRuntimeVolumes(*podSpec, targetIndex, previousVolumeNames, source.Volumes)
	if err != nil {
		return nil, err
	}
	podSpec.Volumes = volumes

	labels := maps.Clone(resource.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	labels[managedByLabel] = "soha-operator"
	if resource.UID != "" {
		labels[ownerUIDLabel] = string(resource.UID)
	}
	annotations := maps.Clone(resource.Annotations)
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["soha.io/source-kind"] = string(resource.Spec.SourceRef.Kind)
	annotations["soha.io/source-namespace"] = resource.Namespace
	annotations["soha.io/source-name"] = resource.Spec.SourceRef.Name
	annotations["soha.io/source-container"] = resource.Spec.SourceRef.Container
	annotations["soha.io/source-uid"] = source.UID

	target := &batchv1.CronJob{
		TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "CronJob"},
		ObjectMeta: metav1.ObjectMeta{
			Name: resource.Name, Namespace: resource.Namespace, Labels: labels, Annotations: annotations,
		},
		Spec: *spec,
	}
	if err := ctrl.SetControllerReference(resource, target, r.Scheme); err != nil {
		return nil, err
	}
	return target, nil
}

func containerVolumeNames(container corev1.Container) map[string]struct{} {
	names := make(map[string]struct{}, len(container.VolumeMounts))
	for _, mount := range container.VolumeMounts {
		names[mount.Name] = struct{}{}
	}
	return names
}

func mergeRuntimeVolumes(podSpec corev1.PodSpec, targetIndex int, previousNames map[string]struct{}, sourceVolumes []corev1.Volume) ([]corev1.Volume, error) {
	sourceNames := make(map[string]struct{}, len(sourceVolumes))
	for _, volume := range sourceVolumes {
		sourceNames[volume.Name] = struct{}{}
		if existing, ok := volumeByName(podSpec.Volumes, volume.Name); ok &&
			volumeUsedOutsideTarget(podSpec, targetIndex, volume.Name) &&
			!equality.Semantic.DeepEqual(existing.VolumeSource, volume.VolumeSource) {
			return nil, fmt.Errorf("%w: volume %q conflicts with another CronJob container", errInvalidCronJobSpec, volume.Name)
		}
	}
	volumes := make([]corev1.Volume, 0, len(podSpec.Volumes)+len(sourceVolumes))
	for _, volume := range podSpec.Volumes {
		_, replaced := sourceNames[volume.Name]
		_, previouslyManaged := previousNames[volume.Name]
		if replaced || previouslyManaged && !volumeUsedOutsideTarget(podSpec, targetIndex, volume.Name) {
			continue
		}
		volumes = append(volumes, volume)
	}
	for i := range sourceVolumes {
		volumes = append(volumes, *sourceVolumes[i].DeepCopy())
	}
	if len(volumes) == 0 {
		return nil, nil
	}
	return volumes, nil
}

func volumeByName(volumes []corev1.Volume, name string) (corev1.Volume, bool) {
	index := slices.IndexFunc(volumes, func(volume corev1.Volume) bool { return volume.Name == name })
	if index < 0 {
		return corev1.Volume{}, false
	}
	return volumes[index], true
}

func volumeUsedOutsideTarget(podSpec corev1.PodSpec, targetIndex int, name string) bool {
	for i, container := range podSpec.Containers {
		if i != targetIndex && containerUsesVolume(container, name) {
			return true
		}
	}
	for _, container := range podSpec.InitContainers {
		if containerUsesVolume(container, name) {
			return true
		}
	}
	return false
}

func containerUsesVolume(container corev1.Container, name string) bool {
	return slices.ContainsFunc(container.VolumeMounts, func(mount corev1.VolumeMount) bool { return mount.Name == name })
}

func (r *WorkloadCronJobReconciler) ensureCronJob(ctx context.Context, owner *workloadsv1alpha1.WorkloadCronJob, desired *batchv1.CronJob) (*batchv1.CronJob, error) {
	var existing batchv1.CronJob
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), &existing)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
			return nil, err
		}
		return desired, nil
	}
	if err != nil {
		return nil, err
	}
	if !metav1.IsControlledBy(&existing, owner) {
		return nil, errTargetConflict
	}
	if existing.DeletionTimestamp != nil {
		return nil, errTargetDeleting
	}

	desired.ResourceVersion = existing.ResourceVersion
	desired.UID = existing.UID
	desired.CreationTimestamp = existing.CreationTimestamp
	desired.Finalizers = slices.Clone(existing.Finalizers)
	if equality.Semantic.DeepEqual(existing.Spec, desired.Spec) &&
		equality.Semantic.DeepEqual(existing.Labels, desired.Labels) &&
		equality.Semantic.DeepEqual(existing.Annotations, desired.Annotations) &&
		equality.Semantic.DeepEqual(existing.OwnerReferences, desired.OwnerReferences) {
		return &existing, nil
	}
	if err := r.Update(ctx, desired); err != nil {
		return nil, err
	}
	return desired, nil
}

func (r *WorkloadCronJobReconciler) suspendOwnedCronJob(ctx context.Context, owner *workloadsv1alpha1.WorkloadCronJob) (*batchv1.CronJob, error) {
	var target batchv1.CronJob
	if err := r.Get(ctx, client.ObjectKeyFromObject(owner), &target); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(&target, owner) {
		return nil, errTargetConflict
	}
	if target.DeletionTimestamp != nil {
		return &target, errTargetDeleting
	}
	if target.Spec.Suspend != nil && *target.Spec.Suspend {
		return &target, nil
	}
	suspend := true
	target.Spec.Suspend = &suspend
	if err := r.Update(ctx, &target); err != nil {
		return nil, err
	}
	return &target, nil
}

func (r *WorkloadCronJobReconciler) updateStatus(
	ctx context.Context,
	resource *workloadsv1alpha1.WorkloadCronJob,
	source observedSource,
	target *batchv1.CronJob,
	conditionStatus metav1.ConditionStatus,
	reason string,
	message string,
) error {
	next := workloadsv1alpha1.WorkloadCronJobStatus{
		ObservedGeneration:    resource.Generation,
		SourceUID:             source.UID,
		SourceResourceVersion: source.ResourceVersion,
		SourceImage:           source.Container.Image,
		LastSyncTime:          resource.Status.LastSyncTime,
		Conditions:            slices.Clone(resource.Status.Conditions),
	}
	if target != nil {
		next.CronJobRef = &corev1.ObjectReference{
			APIVersion: "batch/v1", Kind: "CronJob", Namespace: target.Namespace, Name: target.Name, UID: target.UID,
		}
	}
	meta.SetStatusCondition(&next.Conditions, metav1.Condition{
		Type: conditionReady, Status: conditionStatus, Reason: reason, Message: message, ObservedGeneration: resource.Generation,
	})
	if equality.Semantic.DeepEqual(resource.Status, next) {
		return nil
	}
	now := metav1.Now()
	next.LastSyncTime = &now
	resource.Status = next
	return r.Status().Update(ctx, resource)
}

func sourceReferenceKey(namespace string, reference workloadsv1alpha1.WorkloadReference) string {
	return fmt.Sprintf("%s/%s/%s", reference.Kind, namespace, reference.Name)
}

func (r *WorkloadCronJobReconciler) requestsForSource(ctx context.Context, source client.Object) []reconcile.Request {
	var kind workloadsv1alpha1.WorkloadKind
	switch source.(type) {
	case *appsv1.Deployment:
		kind = workloadsv1alpha1.WorkloadKindDeployment
	case *appsv1.StatefulSet:
		kind = workloadsv1alpha1.WorkloadKindStatefulSet
	case *appsv1.DaemonSet:
		kind = workloadsv1alpha1.WorkloadKindDaemonSet
	default:
		return nil
	}
	var resources workloadsv1alpha1.WorkloadCronJobList
	key := sourceReferenceKey(source.GetNamespace(), workloadsv1alpha1.WorkloadReference{Kind: kind, Name: source.GetName()})
	if err := r.List(ctx, &resources, client.InNamespace(source.GetNamespace()), client.MatchingFields{sourceReferenceIndex: key}); err != nil {
		log.FromContext(ctx).Error(err, "unable to list WorkloadCronJobs for source", "event", "workloadcronjob.source_list_failed", "kind", kind, "namespace", source.GetNamespace(), "name", source.GetName())
		return nil
	}
	requests := make([]reconcile.Request, 0, len(resources.Items))
	for i := range resources.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&resources.Items[i])})
	}
	return requests
}

func (r *WorkloadCronJobReconciler) requestsForTarget(_ context.Context, target client.Object) []reconcile.Request {
	key := client.ObjectKeyFromObject(target)
	if key.Namespace == "" || key.Name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: key}}
}

func (r *WorkloadCronJobReconciler) SetupWithManager(manager ctrl.Manager) error {
	if err := manager.GetFieldIndexer().IndexField(context.Background(), &workloadsv1alpha1.WorkloadCronJob{}, sourceReferenceIndex, func(object client.Object) []string {
		resource, ok := object.(*workloadsv1alpha1.WorkloadCronJob)
		if !ok || resource.Spec.SourceRef.Name == "" {
			return nil
		}
		return []string{sourceReferenceKey(resource.Namespace, resource.Spec.SourceRef)}
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(manager).
		For(&workloadsv1alpha1.WorkloadCronJob{}).
		Watches(&batchv1.CronJob{}, handler.EnqueueRequestsFromMapFunc(r.requestsForTarget)).
		Watches(&appsv1.Deployment{}, handler.EnqueueRequestsFromMapFunc(r.requestsForSource)).
		Watches(&appsv1.StatefulSet{}, handler.EnqueueRequestsFromMapFunc(r.requestsForSource)).
		Watches(&appsv1.DaemonSet{}, handler.EnqueueRequestsFromMapFunc(r.requestsForSource)).
		Complete(r)
}
