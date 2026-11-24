package controller

import (
	"context"
	"errors"
	"testing"

	workloadsv1alpha1 "github.com/opensoha/soha-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestEnsureCronJobHandlesAPIDefaults(t *testing.T) {
	unsupportedDryRun := apierrors.NewBadRequest(`admission webhook "legacy.example.com" does not support dry run`)
	validationError := apierrors.NewBadRequest("invalid CronJob schedule")
	tests := []struct {
		name        string
		mutate      func(existing, desired *batchv1.CronJob)
		dryRunError error
		updateError error
		admit       func(*batchv1.CronJob)
		wantError   error
		wantDryRuns int
		wantUpdates int
	}{
		{name: "native defaults", wantDryRuns: 1},
		{
			name: "raw equality fast path",
			mutate: func(existing, desired *batchv1.CronJob) {
				existing.Spec = *desired.Spec.DeepCopy()
			},
		},
		{
			name: "source image change", wantDryRuns: 1, wantUpdates: 1,
			mutate: func(_, desired *batchv1.CronJob) {
				desired.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image = "source:v2"
			},
		},
		{
			name: "target image drift", wantDryRuns: 1, wantUpdates: 1,
			mutate: func(existing, _ *batchv1.CronJob) {
				existing.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image = "drift:v1"
			},
		},
		{
			name: "deliberate reset to default", wantDryRuns: 1, wantUpdates: 1,
			mutate: func(existing, _ *batchv1.CronJob) {
				*existing.Spec.SuccessfulJobsHistoryLimit = 9
			},
		},
		{
			name: "metadata drift updates after dry run", wantDryRuns: 1, wantUpdates: 1,
			mutate: func(existing, _ *batchv1.CronJob) {
				existing.Labels["unexpected"] = "drift"
			},
		},
		{
			name: "admission metadata change is persisted", wantDryRuns: 1, wantUpdates: 1,
			admit: func(target *batchv1.CronJob) { target.Annotations["admitted"] = "true" },
		},
		{
			name: "unsupported dry run uses validated update", dryRunError: unsupportedDryRun,
			wantDryRuns: 1, wantUpdates: 1,
		},
		{
			name: "unsupported dry run preserves update validation", dryRunError: unsupportedDryRun,
			updateError: validationError, wantError: validationError, wantDryRuns: 1, wantUpdates: 1,
		},
		{
			name: "dry run validation is not bypassed", dryRunError: validationError,
			wantError: validationError, wantDryRuns: 1,
		},
		{
			name: "other bad request is not bypassed", dryRunError: apierrors.NewBadRequest("dry run failed"),
			wantDryRuns: 1,
		},
		{
			name:        "non-API dry run error is not bypassed",
			dryRunError: errors.New(`admission webhook "legacy.example.com" does not support dry run`),
			wantDryRuns: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := testWorkloadCronJob(workloadsv1alpha1.WorkloadKindDeployment)
			reconciler := newTestReconciler(t, resource)
			desired, err := reconciler.desiredCronJob(resource, observedSource{Container: corev1.Container{Image: "source:v1"}})
			if err != nil {
				t.Fatal(err)
			}
			existing := desired.DeepCopy()
			defaultTestCronJob(existing)
			if tt.mutate != nil {
				tt.mutate(existing, desired)
			}
			if err := reconciler.Create(context.Background(), existing); err != nil {
				t.Fatal(err)
			}
			updates := &defaultingCronJobClient{Client: reconciler.Client, dryRunError: tt.dryRunError, updateError: tt.updateError, admit: tt.admit}
			reconciler.Client = updates
			target, err := reconciler.ensureCronJob(context.Background(), resource, desired.DeepCopy())
			wantError := tt.wantError
			if wantError == nil && tt.dryRunError != nil && tt.dryRunError != unsupportedDryRun {
				wantError = tt.dryRunError
			}
			if !errors.Is(err, wantError) {
				t.Fatalf("error = %v, want %v", err, wantError)
			}
			if updates.dryRuns != tt.wantDryRuns || updates.updates != tt.wantUpdates {
				t.Fatalf("dry runs/updates = %d/%d, want %d/%d", updates.dryRuns, updates.updates, tt.wantDryRuns, tt.wantUpdates)
			}
			var stored batchv1.CronJob
			mustGet(t, reconciler.Client, client.ObjectKeyFromObject(existing), &stored)
			if tt.wantUpdates == 0 || wantError != nil {
				if !equality.Semantic.DeepEqual(existing, &stored) {
					t.Fatal("non-persisting path changed the stored CronJob")
				}
				if wantError == nil && target.ResourceVersion != existing.ResourceVersion {
					t.Fatal("no-op did not return the existing resource version")
				}
			} else {
				expected := desired.DeepCopy()
				defaultTestCronJob(expected)
				if tt.admit != nil {
					tt.admit(expected)
				}
				if !equality.Semantic.DeepEqual(expected.Spec, stored.Spec) ||
					!equality.Semantic.DeepEqual(expected.Labels, stored.Labels) ||
					!equality.Semantic.DeepEqual(expected.Annotations, stored.Annotations) {
					t.Fatal("real change or deliberate reset was not persisted")
				}
			}
			if tt.name == "native defaults" || tt.name == "admission metadata change is persisted" {
				wantBefore := updates.updates
				if _, err := reconciler.ensureCronJob(context.Background(), resource, desired.DeepCopy()); err != nil {
					t.Fatal(err)
				}
				if updates.dryRuns != 2 || updates.updates != wantBefore {
					t.Fatalf("repeated reconciliation persisted an unchanged CronJob: dry runs/updates = %d/%d", updates.dryRuns, updates.updates)
				}
			}
		})
	}
}

type defaultingCronJobClient struct {
	client.Client
	dryRuns, updates         int
	dryRunError, updateError error
	admit                    func(*batchv1.CronJob)
}

func (c *defaultingCronJobClient) Update(ctx context.Context, object client.Object, opts ...client.UpdateOption) error {
	target := object.(*batchv1.CronJob)
	options := (&client.UpdateOptions{}).ApplyOptions(opts)
	if len(options.DryRun) > 0 {
		c.dryRuns++
		if c.dryRunError != nil {
			return c.dryRunError
		}
	} else {
		c.updates++
		if c.updateError != nil {
			return c.updateError
		}
	}
	defaultTestCronJob(target)
	if c.admit != nil {
		c.admit(target)
	}
	if len(options.DryRun) > 0 {
		return nil
	}
	return c.Client.Update(ctx, target, opts...)
}

// These fixture defaults exercise each nested level; the API remains the production defaulting source.
func defaultTestCronJob(target *batchv1.CronJob) {
	if target.Spec.ConcurrencyPolicy == "" {
		target.Spec.ConcurrencyPolicy = batchv1.AllowConcurrent
	}
	if target.Spec.SuccessfulJobsHistoryLimit == nil {
		target.Spec.SuccessfulJobsHistoryLimit = new(int32(3))
	}
	if target.Spec.JobTemplate.Spec.BackoffLimit == nil {
		target.Spec.JobTemplate.Spec.BackoffLimit = new(int32(6))
	}
	pod := &target.Spec.JobTemplate.Spec.Template.Spec
	if pod.DNSPolicy == "" {
		pod.DNSPolicy = corev1.DNSClusterFirst
	}
	if pod.TerminationGracePeriodSeconds == nil {
		pod.TerminationGracePeriodSeconds = new(int64(30))
	}
	for i := range pod.Containers {
		if pod.Containers[i].TerminationMessagePolicy == "" {
			pod.Containers[i].TerminationMessagePolicy = corev1.TerminationMessageReadFile
		}
	}
}
