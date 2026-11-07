package factorysource

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	productionv1 "factory.local/factory-platform/api/v1alpha1"
	"factory.local/factory-platform/internal/factory"
	"factory.local/factory-platform/internal/workload"
)

const (
	testProcess   = "line-one"
	testProduct   = "board-a"
	testEquipment = "reflow-oven-01"
)

func readyInputs(t *testing.T) (*productionv1.TechProcess, *productionv1.TechProcessTemplate, *appsv1.Deployment) {
	t.Helper()
	p := &productionv1.TechProcess{ObjectMeta: metav1.ObjectMeta{Name: testProcess, Namespace: "factory-demo", UID: "process", Generation: 2},
		Spec: productionv1.TechProcessSpec{TemplateRef: corev1.LocalObjectReference{Name: testProduct}, TargetSelector: metav1.LabelSelector{MatchLabels: map[string]string{"factory.local/line": testProcess}}}}
	tpl := &productionv1.TechProcessTemplate{ObjectMeta: metav1.ObjectMeta{Name: testProduct, Namespace: p.Namespace, UID: "template", Generation: 3}, Spec: productionv1.TechProcessTemplateSpec{Revision: "v1", Components: []productionv1.ComponentTemplate{{Name: testEquipment, Image: "simulator:test", Args: []string{"--product=board-a"}}}}}
	revision, err := workload.Revision(p, tpl)
	if err != nil {
		t.Fatal(err)
	}
	p.Status = productionv1.TechProcessStatus{ObservedGeneration: p.Generation, ObservedTemplateUID: tpl.UID, ObservedTemplateGeneration: tpl.Generation, AppliedRevision: revision, Components: []productionv1.ComponentStatus{{Name: testEquipment, Desired: 1, Ready: 1}}, DesiredWorkloads: 1, ReadyWorkloads: 1, Conditions: []metav1.Condition{{Type: factory.StateReady, Status: metav1.ConditionTrue, ObservedGeneration: p.Generation}}}
	dep := workload.Deployment(p, tpl.Spec.Components[0], revision)
	dep.UID = "deployment"
	dep.Generation = 1
	dep.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(p, productionv1.GroupVersion.WithKind("TechProcess"))}
	dep.Status = appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1}
	return p, tpl, dep
}

func readerFor(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{productionv1.AddToScheme, corev1.AddToScheme, appsv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func TestKubernetesRevisionAndOwnership(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*productionv1.TechProcess, *productionv1.TechProcessTemplate, *appsv1.Deployment)
		state  string
	}{
		{"ready", func(_ *productionv1.TechProcess, _ *productionv1.TechProcessTemplate, _ *appsv1.Deployment) {}, factory.StateReady},
		{"old process", func(p *productionv1.TechProcess, _ *productionv1.TechProcessTemplate, _ *appsv1.Deployment) {
			p.Generation++
		}, factory.StateUnknown},
		{"old template", func(_ *productionv1.TechProcess, p *productionv1.TechProcessTemplate, _ *appsv1.Deployment) {
			p.Generation++
		}, factory.StateUnknown},
		{"recreated template", func(_ *productionv1.TechProcess, p *productionv1.TechProcessTemplate, _ *appsv1.Deployment) {
			p.UID = "replacement"
		}, factory.StateUnknown},
		{"wrong revision", func(p *productionv1.TechProcess, _ *productionv1.TechProcessTemplate, _ *appsv1.Deployment) {
			p.Status.AppliedRevision = "old"
		}, factory.StateUnknown},
		{"stale condition", func(p *productionv1.TechProcess, _ *productionv1.TechProcessTemplate, _ *appsv1.Deployment) {
			p.Status.Conditions[0].ObservedGeneration = 1
		}, factory.StateUnknown},
		{"foreign child", func(_ *productionv1.TechProcess, _ *productionv1.TechProcessTemplate, d *appsv1.Deployment) {
			d.OwnerReferences[0].UID = "other"
		}, factory.StateProgressing},
		{"mixed replicas", func(_ *productionv1.TechProcess, _ *productionv1.TechProcessTemplate, d *appsv1.Deployment) {
			d.Status.Replicas = 2
		}, factory.StateProgressing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, tpl, dep := readyInputs(t)
			tc.change(p, tpl, dep)
			got, err := (Kubernetes{Reader: readerFor(t, p, tpl, dep)}).Read(context.Background(), p.Namespace, p.Name)
			if err != nil {
				t.Fatal(err)
			}
			if got.Process.State != tc.state {
				t.Fatalf("got %#v", got.Process)
			}
		})
	}
}

func TestKubernetesPodOwnerChainAndMissingTemplate(t *testing.T) {
	p, tpl, dep := readyInputs(t)
	labels := map[string]string{workload.LabelProcess: p.Name, workload.LabelComponent: testEquipment, workload.LabelRevision: p.Status.AppliedRevision}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "owned-rs", Namespace: p.Namespace, UID: "rs", Labels: labels, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(dep, appsv1.SchemeGroupVersion.WithKind("Deployment"))}}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "owned-pod", Namespace: p.Namespace, Labels: labels, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(rs, appsv1.SchemeGroupVersion.WithKind("ReplicaSet"))}}, Spec: corev1.PodSpec{NodeName: "edge-one"}}
	foreign := pod.DeepCopy()
	foreign.Name = "foreign-pod"
	foreign.OwnerReferences[0].UID = "foreign"
	got, err := (Kubernetes{Reader: readerFor(t, p, tpl, dep, rs, pod, foreign)}).Read(context.Background(), p.Namespace, p.Name)
	if err != nil {
		t.Fatal(err)
	}
	view := got.Components[testEquipment]
	if len(view.Pods) != 1 || view.Pods[0].Node != "edge-one" || view.Recipe == nil || view.Recipe.Product != testProduct {
		t.Fatalf("component: %#v", view)
	}
	p.Status.ObservedTemplateUID = ""
	p.Status.ObservedTemplateGeneration = 0
	p.Status.Conditions = []metav1.Condition{{Type: factory.StateDegraded, Status: metav1.ConditionTrue, ObservedGeneration: p.Generation, Reason: "TemplateNotFound"}}
	got, err = (Kubernetes{Reader: readerFor(t, p)}).Read(context.Background(), p.Namespace, p.Name)
	if err != nil || got.Process.State != factory.StateDegraded || got.Process.Ready != nil {
		t.Fatalf("missing template: %#v %v", got, err)
	}
}

type changingReader struct {
	client.Reader
	reads int
}

func (r *changingReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if err := r.Reader.Get(ctx, key, obj, opts...); err != nil {
		return err
	}
	if p, ok := obj.(*productionv1.TechProcess); ok {
		r.reads++
		if r.reads > 1 {
			p.Generation++
		}
	}
	return nil
}

func TestKubernetesRejectsConcurrentChange(t *testing.T) {
	p, tpl, dep := readyInputs(t)
	_, err := (Kubernetes{Reader: &changingReader{Reader: readerFor(t, p, tpl, dep)}}).Read(context.Background(), p.Namespace, p.Name)
	if err == nil {
		t.Fatal("mixed input generations accepted")
	}
}

type failingReader struct{ client.Reader }

func (failingReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return errors.New("source unavailable")
}
func TestKubernetesReadError(t *testing.T) {
	_, err := (Kubernetes{Reader: failingReader{}}).Read(context.Background(), "factory-demo", testProcess)
	if err == nil {
		t.Fatal("failure hidden")
	}
}
