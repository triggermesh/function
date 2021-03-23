/*
Copyright 2021 Triggermesh Inc.

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

package function

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"

	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/resolver"
	"knative.dev/pkg/tracker"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
	servingv1client "knative.dev/serving/pkg/client/clientset/versioned"
	servingv1listers "knative.dev/serving/pkg/client/listers/serving/v1"

	functionv1alpha1 "github.com/triggermesh/function/pkg/apis/function/v1alpha1"
	functionreconciler "github.com/triggermesh/function/pkg/client/generated/injection/reconciler/function/v1alpha1/function"
	"github.com/triggermesh/function/pkg/reconciler/function/resources"
)

const (
	eventType     = "io.triggermesh.flow.function"
	klrEntrypoint = "/opt/aws-custom-runtime"
	labelKey      = "flow.trigermesh.io/function"
)

// Reconciler implements addressableservicereconciler.Interface for
// AddressableService resources.
type Reconciler struct {
	// Tracker builds an index of what resources are watching other resources
	// so that we can immediately react to changes tracked resources.
	Tracker tracker.Interface

	// Listers index properties about resources
	knServiceLister    servingv1listers.ServiceLister
	knServingClientSet servingv1client.Interface

	cmLister      corev1listers.ConfigMapLister
	coreClientSet kubernetes.Interface

	sinkResolver *resolver.URIResolver

	// runtime names and container URIs
	runtimes map[string]string
}

// Check that our Reconciler implements Interface
var _ functionreconciler.Interface = (*Reconciler)(nil)

// newReconciledNormal makes a new reconciler event with event type Normal, and
// reason AddressableServiceReconciled.
func newReconciledNormal(namespace, name string) reconciler.Event {
	return reconciler.NewEvent(corev1.EventTypeNormal, "FunctionReconciled", "Function reconciled: \"%s/%s\"", namespace, name)
}

// ReconcileKind implements Interface.ReconcileKind.
func (r *Reconciler) ReconcileKind(ctx context.Context, o *functionv1alpha1.Function) reconciler.Event {
	logger := logging.FromContext(ctx)

	// Reconcile Transformation Adapter
	cm, err := r.reconcileConfigmap(ctx, o)
	if err != nil {
		logger.Error("Error reconciling Configmap", zap.Error(err))
		o.Status.MarkConfigmapUnavailable(o.Name)
		return err
	}
	if err := r.Tracker.TrackReference(tracker.Reference{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       cm.Name,
		Namespace:  cm.Namespace,
	}, o); err != nil {
		logger.Errorf("Error tracking configmap %s: %v", o.Name, err)
		return err
	}
	o.Status.MarkConfigmapAvailable()

	// Reconcile Transformation Adapter
	ksvc, err := r.reconcileKnService(ctx, o, cm)
	if err != nil {
		logger.Error("Error reconciling Kn Service", zap.Error(err))
		o.Status.MarkServiceUnavailable(o.Name)
		return err
	}

	if err := r.Tracker.TrackReference(tracker.Reference{
		APIVersion: "serving.knative.dev/v1",
		Kind:       "Service",
		Name:       ksvc.Name,
		Namespace:  ksvc.Namespace,
	}, o); err != nil {
		logger.Errorf("Error tracking Kn service %s: %v", o.Name, err)
		return err
	}

	if !ksvc.IsReady() {
		o.Status.MarkServiceUnavailable(o.Name)
		return nil
	}

	o.Status.Address = &duckv1.Addressable{
		URL: &apis.URL{
			Scheme: ksvc.Status.Address.URL.Scheme,
			Host:   ksvc.Status.Address.URL.Host,
		},
	}
	o.Status.MarkServiceAvailable()

	o.Status.CloudEventAttributes = r.createCloudEventAttributes(o.SelfLink)
	o.Status.MarkSinkAvailable()

	logger.Debug("Transformation reconciled")
	return newReconciledNormal(o.Namespace, o.Name)
}

func (r *Reconciler) reconcileConfigmap(ctx context.Context, f *functionv1alpha1.Function) (*corev1.ConfigMap, error) {
	logger := logging.FromContext(ctx)

	expectedCm := resources.NewConfigmap(f.Name+"-"+rand.String(6), f.Namespace,
		resources.CmOwner(f),
		resources.CmLabel(map[string]string{labelKey: f.Name}),
		resources.CmData(f.Spec.Code),
	)

	cmList, err := r.coreClientSet.CoreV1().ConfigMaps(f.Namespace).List(ctx, v1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", labelKey, f.Name),
	})
	if err != nil {
		return nil, err
	}
	if len(cmList.Items) == 0 {
		logger.Infof("Creating configmap %q", f.Name)
		return r.coreClientSet.CoreV1().ConfigMaps(f.Namespace).Create(ctx, expectedCm, v1.CreateOptions{})
	}
	actualCm := &cmList.Items[0]

	if !reflect.DeepEqual(actualCm.Data, expectedCm.Data) {
		actualCm.Data = expectedCm.Data
		return r.coreClientSet.CoreV1().ConfigMaps(f.Namespace).Update(ctx, actualCm, v1.UpdateOptions{})
	}

	return actualCm, nil
}

func (r *Reconciler) reconcileKnService(ctx context.Context, f *functionv1alpha1.Function, cm *corev1.ConfigMap) (*servingv1.Service, error) {
	logger := logging.FromContext(ctx)

	image, err := r.lookupRuntimeImage(f.Spec.Runtime)
	if err != nil {
		return nil, err
	}

	dest := f.Spec.Sink.DeepCopy()
	if dest.Ref != nil {
		if dest.Ref.Namespace == "" {
			dest.Ref.Namespace = f.GetNamespace()
		}
	}

	var sink string
	uri, err := r.sinkResolver.URIFromDestinationV1(ctx, *dest, f)
	if err != nil {
		logger.Infof("Sink resolution error: %v", err)
	} else {
		f.Status.SinkURI = uri
		sink = uri.String()
	}

	filename := fmt.Sprintf("source.%s", fileExtension(f.Spec.Runtime))
	handler := fmt.Sprintf("source.%s", f.Spec.Entrypoint)

	expectedKsvc := resources.NewKnService(f.Name+"-"+rand.String(6), f.Namespace,
		resources.KnSvcImage(image),
		resources.KnSvcMountCm(cm.Name, filename),
		resources.KnSvcEntrypoint(klrEntrypoint),
		resources.KnSvcEnvVar("K_SINK", sink),
		resources.KnSvcEnvVar("_HANDLER", handler),
		resources.KnSvcEnvVar("RESPONSE_WRAPPER", "CLOUDEVENTS"),
		resources.KnSvcEnvVar("CE_TYPE", eventType),
		resources.KnSvcEnvVar("CE_K_SERVICE", f.SelfLink),
		resources.KnSvcEnvVar("CE_SUBJECT", handler),
		resources.KnSvcLabel(map[string]string{labelKey: f.Name}),
		resources.KnSvcOwner(f),
	)

	ksvcList, err := r.knServiceLister.Services(f.Namespace).List(labels.SelectorFromSet(labels.Set{labelKey: f.Name}))
	if err != nil {
		return nil, err
	}
	if len(ksvcList) == 0 {
		logger.Infof("Creating Kn Service %q", f.Name)
		return r.knServingClientSet.ServingV1().Services(f.Namespace).Create(ctx, expectedKsvc, v1.CreateOptions{})
	}
	actualKsvc := ksvcList[0]

	if !reflect.DeepEqual(actualKsvc.Spec.ConfigurationSpec.Template.Spec,
		expectedKsvc.Spec.ConfigurationSpec.Template.Spec) {
		actualKsvc.Spec = expectedKsvc.Spec
		return r.knServingClientSet.ServingV1().Services(f.Namespace).Update(ctx, actualKsvc, v1.UpdateOptions{})
	}
	return actualKsvc, nil
}

func (r *Reconciler) createCloudEventAttributes(source string) []duckv1.CloudEventAttributes {
	return []duckv1.CloudEventAttributes{
		{
			Type:   eventType,
			Source: source,
		},
	}
}

func (r *Reconciler) lookupRuntimeImage(runtime string) (string, error) {
	rn := strings.ToLower(runtime)

	for name, uri := range r.runtimes {
		name = strings.ToLower(name)
		if strings.Contains(name, rn) {
			return uri, nil
		}
	}
	return "", fmt.Errorf("runtime %q not registered in the controller env", runtime)
}

// Lambda runtimes require file extensions to match the language,
// i.e. source file for Python runtime must have ".py" prefix, JavaScript - ".js", etc.
// It would be more correct to declare these extensions explicitly,
// along with the runtime container URIs, but since we manage the
// available runtimes list, this also works.
func fileExtension(runtime string) string {
	runtime = strings.ToLower(runtime)
	switch {
	case strings.Contains(runtime, "python"):
		return "py"
	case strings.Contains(runtime, "node") ||
		strings.Contains(runtime, "js"):
		return "js"
	case strings.Contains(runtime, "ruby"):
		return "rb"
	case strings.Contains(runtime, "sh"):
		return "sh"
	}
	return "txt"
}
