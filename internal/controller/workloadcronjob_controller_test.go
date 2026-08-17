package controller

import (
	"context"
	"testing"

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
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcileSupportsAllSourceKinds(t *testing.T) {
	tests := []struct {
		kind   workloadsv1alpha1.WorkloadKind
		source client.Object
	}{
		{kind: workloadsv1alpha1.WorkloadKindDeployment, source: testDeployment("source:v2")},
		{kind: workloadsv1alpha1.WorkloadKindStatefulSet, source: testStatefulSet("source:v2")},
		{kind: workloadsv1alpha1.WorkloadKindDaemonSet, source: testDaemonSet("source:v2")},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			resource := testWorkloadCronJob(tt.kind)
			reconciler := newTestReconciler(t, resource, tt.source)
			reconcileOnce(t, reconciler)

			var target batchv1.CronJob
			mustGet(t, reconciler.Client, types.NamespacedName{Namespace: "default", Name: "nightly"}, &target)
			if got := target.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image; got != "source:v2" {
				t.Fatalf("target image = %q, want source:v2", got)
			}
			if !metav1.IsControlledBy(&target, resource) {
				t.Fatal("CronJob is not controlled by WorkloadCronJob")
			}

			var current workloadsv1alpha1.WorkloadCronJob
			mustGet(t, reconciler.Client, client.ObjectKeyFromObject(resource), &current)
			ready := meta.FindStatusCondition(current.Status.Conditions, conditionReady)
			if ready == nil || ready.Status != metav1.ConditionTrue || ready.Reason != "Reconciled" {
				t.Fatalf("Ready condition = %#v", ready)
			}
			if current.Status.SourceImage != "source:v2" || current.Status.CronJobRef == nil {
				t.Fatalf("status = %#v", current.Status)
			}
		})
	}
}

func TestReconcileUpdatesImageWhenSourceChanges(t *testing.T) {
	resource := testWorkloadCronJob(workloadsv1alpha1.WorkloadKindDeployment)
	reconciler := newTestReconciler(t, resource, testDeployment("source:v1"))
	reconcileOnce(t, reconciler)

	var source appsv1.Deployment
	mustGet(t, reconciler.Client, types.NamespacedName{Namespace: "default", Name: "api"}, &source)
	source.Spec.Template.Spec.Containers[0].Image = "source:v2"
	if err := reconciler.Update(context.Background(), &source); err != nil {
		t.Fatalf("update source: %v", err)
	}
	reconcileOnce(t, reconciler)

	var target batchv1.CronJob
	mustGet(t, reconciler.Client, types.NamespacedName{Namespace: "default", Name: "nightly"}, &target)
	if got := target.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image; got != "source:v2" {
		t.Fatalf("target image = %q, want source:v2", got)
	}
}

func TestReconcileUpdatesRuntimeEnvironmentWhenSourceChanges(t *testing.T) {
	resource := testWorkloadCronJob(workloadsv1alpha1.WorkloadKindDeployment)
	podSpec := &resource.Spec.CronJobSpec.JobTemplate.Spec.Template.Spec
	target := &podSpec.Containers[0]
	target.Command = []string{"job-command"}
	target.Args = []string{"job-argument"}
	target.Env = []corev1.EnvVar{{Name: "VERSION", Value: "snapshot"}}
	target.VolumeMounts = []corev1.VolumeMount{{Name: "snapshot", MountPath: "/runtime"}}
	podSpec.Volumes = []corev1.Volume{
		{Name: "snapshot", VolumeSource: configMapVolumeSource("snapshot")},
		{Name: "job-only", VolumeSource: configMapVolumeSource("job-only")},
	}

	source := testDeployment("source:v1")
	setRuntimeEnvironment(&source.Spec.Template, "v1")
	reconciler := newTestReconciler(t, resource, source)
	reconcileOnce(t, reconciler)
	assertRuntimeEnvironment(t, reconciler.Client, "v1")

	var currentSource appsv1.Deployment
	mustGet(t, reconciler.Client, types.NamespacedName{Namespace: "default", Name: "api"}, &currentSource)
	setRuntimeEnvironment(&currentSource.Spec.Template, "v2")
	if err := reconciler.Update(context.Background(), &currentSource); err != nil {
		t.Fatalf("update source: %v", err)
	}
	reconcileOnce(t, reconciler)
	assertRuntimeEnvironment(t, reconciler.Client, "v2")

	mustGet(t, reconciler.Client, types.NamespacedName{Namespace: "default", Name: "api"}, &currentSource)
	currentSource.Spec.Template.Spec.Containers[0].Image = "source:v3"
	currentSource.Spec.Template.Spec.Containers[0].Env = nil
	currentSource.Spec.Template.Spec.Containers[0].EnvFrom = nil
	currentSource.Spec.Template.Spec.Containers[0].VolumeMounts = nil
	currentSource.Spec.Template.Spec.Volumes = nil
	if err := reconciler.Update(context.Background(), &currentSource); err != nil {
		t.Fatalf("clear source runtime: %v", err)
	}
	reconcileOnce(t, reconciler)
	assertRuntimeEnvironmentCleared(t, reconciler.Client)
}

func TestReconcileRejectsMissingSourceVolume(t *testing.T) {
	resource := testWorkloadCronJob(workloadsv1alpha1.WorkloadKindDeployment)
	source := testDeployment("source:v1")
	source.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "missing", MountPath: "/runtime"}}
	reconciler := newTestReconciler(t, resource, source)
	reconcileOnce(t, reconciler)

	var target batchv1.CronJob
	if err := reconciler.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "nightly"}, &target); !apierrors.IsNotFound(err) {
		t.Fatalf("target error = %v, want not found", err)
	}
	var current workloadsv1alpha1.WorkloadCronJob
	mustGet(t, reconciler.Client, client.ObjectKeyFromObject(resource), &current)
	ready := meta.FindStatusCondition(current.Status.Conditions, conditionReady)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != "SourceVolumeNotFound" {
		t.Fatalf("Ready condition = %#v", ready)
	}
}

func TestReconcileRejectsSourceVolumeConflictWithSidecar(t *testing.T) {
	resource := testWorkloadCronJob(workloadsv1alpha1.WorkloadKindDeployment)
	podSpec := &resource.Spec.CronJobSpec.JobTemplate.Spec.Template.Spec
	podSpec.Containers = append(podSpec.Containers, corev1.Container{
		Name: "sidecar", Image: "sidecar:v1", VolumeMounts: []corev1.VolumeMount{{Name: "shared", MountPath: "/shared"}},
	})
	podSpec.Volumes = []corev1.Volume{{Name: "shared", VolumeSource: configMapVolumeSource("job-sidecar")}}
	source := testDeployment("source:v1")
	source.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "shared", MountPath: "/runtime"}}
	source.Spec.Template.Spec.Volumes = []corev1.Volume{{Name: "shared", VolumeSource: configMapVolumeSource("source-runtime")}}
	reconciler := newTestReconciler(t, resource, source)
	reconcileOnce(t, reconciler)

	var target batchv1.CronJob
	if err := reconciler.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "nightly"}, &target); !apierrors.IsNotFound(err) {
		t.Fatalf("target error = %v, want not found", err)
	}
	var current workloadsv1alpha1.WorkloadCronJob
	mustGet(t, reconciler.Client, client.ObjectKeyFromObject(resource), &current)
	ready := meta.FindStatusCondition(current.Status.Conditions, conditionReady)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != "InvalidSpec" {
		t.Fatalf("Ready condition = %#v", ready)
	}
}

func TestReconcileSuspendsTargetWhenSourceDisappears(t *testing.T) {
	resource := testWorkloadCronJob(workloadsv1alpha1.WorkloadKindDeployment)
	source := testDeployment("source:v1")
	reconciler := newTestReconciler(t, resource, source)
	reconcileOnce(t, reconciler)
	if err := reconciler.Delete(context.Background(), source); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	reconcileOnce(t, reconciler)

	var target batchv1.CronJob
	mustGet(t, reconciler.Client, types.NamespacedName{Namespace: "default", Name: "nightly"}, &target)
	if target.Spec.Suspend == nil || !*target.Spec.Suspend {
		t.Fatal("CronJob was not suspended after the source disappeared")
	}
	var current workloadsv1alpha1.WorkloadCronJob
	mustGet(t, reconciler.Client, client.ObjectKeyFromObject(resource), &current)
	ready := meta.FindStatusCondition(current.Status.Conditions, conditionReady)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != "SourceNotFound" {
		t.Fatalf("Ready condition = %#v", ready)
	}
}

func TestReconcileDoesNotAdoptForeignCronJob(t *testing.T) {
	resource := testWorkloadCronJob(workloadsv1alpha1.WorkloadKindDeployment)
	foreign := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "default"},
		Spec: batchv1.CronJobSpec{
			Schedule: "0 0 * * *",
			JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers:    []corev1.Container{{Name: "task", Image: "foreign:v1"}},
			}}}},
		},
	}
	reconciler := newTestReconciler(t, resource, testDeployment("source:v2"), foreign)
	reconcileOnce(t, reconciler)

	var target batchv1.CronJob
	mustGet(t, reconciler.Client, client.ObjectKeyFromObject(foreign), &target)
	if got := target.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image; got != "foreign:v1" {
		t.Fatalf("foreign target image changed to %q", got)
	}
	var current workloadsv1alpha1.WorkloadCronJob
	mustGet(t, reconciler.Client, client.ObjectKeyFromObject(resource), &current)
	ready := meta.FindStatusCondition(current.Status.Conditions, conditionReady)
	if ready == nil || ready.Reason != "TargetConflict" {
		t.Fatalf("Ready condition = %#v", ready)
	}
}

func TestTargetDeleteRequestRecoversFromForeignCronJobConflict(t *testing.T) {
	resource := testWorkloadCronJob(workloadsv1alpha1.WorkloadKindDeployment)
	foreign := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "default"},
		Spec: batchv1.CronJobSpec{
			Schedule: "0 0 * * *",
			JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers:    []corev1.Container{{Name: "task", Image: "foreign:v1"}},
			}}}},
		},
	}
	reconciler := newTestReconciler(t, resource, testDeployment("source:v2"), foreign)
	reconcileOnce(t, reconciler)

	var current workloadsv1alpha1.WorkloadCronJob
	mustGet(t, reconciler.Client, client.ObjectKeyFromObject(resource), &current)
	ready := meta.FindStatusCondition(current.Status.Conditions, conditionReady)
	if ready == nil || ready.Reason != "TargetConflict" {
		t.Fatalf("Ready condition = %#v", ready)
	}

	if err := reconciler.Delete(context.Background(), foreign); err != nil {
		t.Fatalf("delete foreign target: %v", err)
	}
	requests := reconciler.requestsForTarget(context.Background(), foreign)
	if len(requests) != 1 || requests[0].NamespacedName != client.ObjectKeyFromObject(foreign) {
		t.Fatalf("target requests = %#v", requests)
	}
	if _, err := reconciler.Reconcile(context.Background(), requests[0]); err != nil {
		t.Fatalf("reconcile target delete: %v", err)
	}

	var target batchv1.CronJob
	mustGet(t, reconciler.Client, client.ObjectKeyFromObject(foreign), &target)
	if got := target.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image; got != "source:v2" {
		t.Fatalf("target image = %q, want source:v2", got)
	}
	if !metav1.IsControlledBy(&target, resource) {
		t.Fatal("replacement CronJob is not controlled by WorkloadCronJob")
	}
}

func TestSourceRequestsUseIndexedReference(t *testing.T) {
	matching := testWorkloadCronJob(workloadsv1alpha1.WorkloadKindDeployment)
	unrelated := testWorkloadCronJob(workloadsv1alpha1.WorkloadKindDeployment)
	unrelated.Name = "other"
	unrelated.Spec.SourceRef.Name = "other-source"
	reconciler := newTestReconciler(t, matching, unrelated)

	requests := reconciler.requestsForSource(context.Background(), testDeployment("source:v1"))
	if len(requests) != 1 || requests[0].NamespacedName != client.ObjectKeyFromObject(matching) {
		t.Fatalf("source requests = %#v", requests)
	}
	if requests := reconciler.requestsForSource(context.Background(), &corev1.ConfigMap{}); requests != nil {
		t.Fatalf("unsupported source requests = %#v, want nil", requests)
	}
}

func TestTargetRequestsRequireNamespacedName(t *testing.T) {
	reconciler := &WorkloadCronJobReconciler{}
	if requests := reconciler.requestsForTarget(context.Background(), &batchv1.CronJob{}); requests != nil {
		t.Fatalf("target requests = %#v, want nil", requests)
	}
}

func newTestReconciler(t *testing.T, objects ...client.Object) *WorkloadCronJobReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add Kubernetes scheme: %v", err)
	}
	if err := workloadsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add WorkloadCronJob scheme: %v", err)
	}
	return &WorkloadCronJobReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&workloadsv1alpha1.WorkloadCronJob{}).
			WithIndex(&workloadsv1alpha1.WorkloadCronJob{}, sourceReferenceIndex, func(object client.Object) []string {
				resource := object.(*workloadsv1alpha1.WorkloadCronJob)
				return []string{sourceReferenceKey(resource.Namespace, resource.Spec.SourceRef)}
			}).
			WithObjects(objects...).
			Build(),
		Scheme: scheme,
	}
}

func reconcileOnce(t *testing.T, reconciler *WorkloadCronJobReconciler) {
	t.Helper()
	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "nightly"}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

func mustGet(t *testing.T, client client.Client, key types.NamespacedName, object client.Object) {
	t.Helper()
	if err := client.Get(context.Background(), key, object); err != nil {
		t.Fatalf("get %T %s: %v", object, key, err)
	}
}

func testWorkloadCronJob(kind workloadsv1alpha1.WorkloadKind) *workloadsv1alpha1.WorkloadCronJob {
	return &workloadsv1alpha1.WorkloadCronJob{
		TypeMeta: metav1.TypeMeta{APIVersion: workloadsv1alpha1.GroupVersion.String(), Kind: "WorkloadCronJob"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "nightly", Namespace: "default", UID: types.UID("workload-cronjob-uid"), Generation: 1,
		},
		Spec: workloadsv1alpha1.WorkloadCronJobSpec{
			SourceRef:       workloadsv1alpha1.WorkloadReference{Kind: kind, Name: "api", Container: "app"},
			TargetContainer: "task",
			CronJobSpec: batchv1.CronJobSpec{
				Schedule: "*/5 * * * *",
				JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers:    []corev1.Container{{Name: "task", Image: "snapshot:v1"}},
				}}}},
			},
		},
	}
}

func testDeployment(image string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", UID: types.UID("source-uid")},
		Spec:       appsv1.DeploymentSpec{Template: testPodTemplate(image)},
	}
}

func testStatefulSet(image string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", UID: types.UID("source-uid")},
		Spec:       appsv1.StatefulSetSpec{Template: testPodTemplate(image)},
	}
}

func testDaemonSet(image string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", UID: types.UID("source-uid")},
		Spec:       appsv1.DaemonSetSpec{Template: testPodTemplate(image)},
	}
}

func testPodTemplate(image string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: image}}}}
}

func setRuntimeEnvironment(template *corev1.PodTemplateSpec, version string) {
	configMapName := "runtime-" + version
	container := &template.Spec.Containers[0]
	container.Image = "source:" + version
	container.Env = []corev1.EnvVar{{Name: "VERSION", Value: version}}
	container.EnvFrom = []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: configMapName}}}}
	container.VolumeMounts = []corev1.VolumeMount{{Name: configMapName, MountPath: "/runtime", ReadOnly: true}}
	template.Spec.Volumes = []corev1.Volume{{Name: configMapName, VolumeSource: configMapVolumeSource(configMapName)}}
}

func configMapVolumeSource(name string) corev1.VolumeSource {
	return corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: name}}}
}

func assertRuntimeEnvironment(t *testing.T, client client.Client, version string) {
	t.Helper()
	var target batchv1.CronJob
	mustGet(t, client, types.NamespacedName{Namespace: "default", Name: "nightly"}, &target)
	container := target.Spec.JobTemplate.Spec.Template.Spec.Containers[0]
	configMapName := "runtime-" + version
	if container.Image != "source:"+version || !equality.Semantic.DeepEqual(container.Env, []corev1.EnvVar{{Name: "VERSION", Value: version}}) {
		t.Fatalf("target runtime = %#v", container)
	}
	if len(container.EnvFrom) != 1 || container.EnvFrom[0].ConfigMapRef == nil || container.EnvFrom[0].ConfigMapRef.Name != configMapName {
		t.Fatalf("target envFrom = %#v", container.EnvFrom)
	}
	if !equality.Semantic.DeepEqual(container.VolumeMounts, []corev1.VolumeMount{{Name: configMapName, MountPath: "/runtime", ReadOnly: true}}) {
		t.Fatalf("target volumeMounts = %#v", container.VolumeMounts)
	}
	if !equality.Semantic.DeepEqual(container.Command, []string{"job-command"}) || !equality.Semantic.DeepEqual(container.Args, []string{"job-argument"}) {
		t.Fatalf("job command changed: command=%v args=%v", container.Command, container.Args)
	}
	volumes := target.Spec.JobTemplate.Spec.Template.Spec.Volumes
	if len(volumes) != 2 || volumes[0].Name != "job-only" || volumes[1].Name != configMapName || volumes[1].ConfigMap == nil || volumes[1].ConfigMap.Name != configMapName {
		t.Fatalf("target volumes = %#v", volumes)
	}
}

func assertRuntimeEnvironmentCleared(t *testing.T, client client.Client) {
	t.Helper()
	var target batchv1.CronJob
	mustGet(t, client, types.NamespacedName{Namespace: "default", Name: "nightly"}, &target)
	container := target.Spec.JobTemplate.Spec.Template.Spec.Containers[0]
	if container.Image != "source:v3" || len(container.Env) != 0 || len(container.EnvFrom) != 0 || len(container.VolumeMounts) != 0 {
		t.Fatalf("target runtime was not cleared: %#v", container)
	}
	if !equality.Semantic.DeepEqual(container.Command, []string{"job-command"}) || !equality.Semantic.DeepEqual(container.Args, []string{"job-argument"}) {
		t.Fatalf("job command changed: command=%v args=%v", container.Command, container.Args)
	}
	volumes := target.Spec.JobTemplate.Spec.Template.Spec.Volumes
	if len(volumes) != 1 || volumes[0].Name != "job-only" {
		t.Fatalf("target volumes = %#v", volumes)
	}
}
