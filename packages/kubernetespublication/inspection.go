// Package kubernetespublication evaluates the concrete Gateway API consumption
// contract. It neither authorizes product use nor prepares infrastructure.
package kubernetespublication

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
)

// Reader deliberately exposes no mutation or Secret inspection capability.
type Reader interface {
	Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error
}

type Facts struct {
	GatewayUID        string
	GatewayGeneration int64
	Conditions        []metav1.Condition
}

// Target contains only the stable Kubernetes coordinates required for a
// read-only inspection. Product binding identity and revision belong to the
// authorization and execution contracts, not to this observer.
type Target struct {
	GatewayNamespace  string
	GatewayName       string
	SectionName       string
	Hostname          string
	ConsumerNamespace string
}

// Inspect consumes an existing Gateway without recipe, Helm, or Secret access.
func Inspect(ctx context.Context, reader Reader, target Target) (Facts, error) {
	validationTarget := kubernetesbinding.HTTPDestination{BindingID: "inspection", BindingRevision: 1, SchemaVersion: kubernetesbinding.HTTPBindingSchemaVersion, GatewayNamespace: target.GatewayNamespace, GatewayName: target.GatewayName, SectionName: target.SectionName}
	if err := validationTarget.Validate(); err != nil || target.Hostname == "" || target.ConsumerNamespace == "" {
		if err == nil {
			err = errors.New("publication_inspection_target_invalid")
		}
		return Facts{}, err
	}
	gateway := &gatewayv1.Gateway{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: target.GatewayNamespace, Name: target.GatewayName}, gateway); err != nil {
		return Facts{}, err
	}
	namespace := &corev1.Namespace{}
	namespaceErr := reader.Get(ctx, client.ObjectKey{Name: target.ConsumerNamespace}, namespace)
	class := &gatewayv1.GatewayClass{}
	classErr := reader.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, class)
	if classErr != nil {
		class = nil
	}
	if namespaceErr != nil {
		namespace = nil
	}
	facts := Evaluate(gateway, class, namespace, target.SectionName, target.Hostname)
	return facts, errors.Join(classErr, namespaceErr)
}

func Evaluate(gateway *gatewayv1.Gateway, class *gatewayv1.GatewayClass, namespace *corev1.Namespace, section, hostname string) Facts {
	f := Facts{}
	add := func(kind string, status metav1.ConditionStatus, reason string) {
		f.Conditions = append(f.Conditions, metav1.Condition{Type: kind, Status: status, Reason: reason, ObservedGeneration: f.GatewayGeneration, LastTransitionTime: metav1.Now()})
	}
	if gateway == nil {
		add("GatewayProgrammed", metav1.ConditionUnknown, "NotObserved")
		return f
	}
	f.GatewayUID = string(gateway.UID)
	f.GatewayGeneration = gateway.Generation
	copyCondition := func(kind string, conditions []metav1.Condition, conditionType string, generation int64) {
		c := meta.FindStatusCondition(conditions, conditionType)
		if c == nil || c.ObservedGeneration != generation {
			add(kind, metav1.ConditionUnknown, "NotObserved")
		} else {
			add(kind, c.Status, c.Reason)
		}
	}
	copyCondition("GatewayProgrammed", gateway.Status.Conditions, "Programmed", gateway.Generation)
	if class == nil {
		add("GatewayClassAccepted", metav1.ConditionUnknown, "NotObserved")
	} else {
		copyCondition("GatewayClassAccepted", class.Status.Conditions, "Accepted", class.Generation)
	}
	var listener *gatewayv1.Listener
	for i := range gateway.Spec.Listeners {
		if string(gateway.Spec.Listeners[i].Name) == section {
			listener = &gateway.Spec.Listeners[i]
			break
		}
	}
	if listener == nil {
		add("ListenerCompatible", metav1.ConditionFalse, "ListenerMissing")
		return f
	}
	compatible := listener.Protocol == gatewayv1.HTTPSProtocolType && listener.TLS != nil && (listener.TLS.Mode == nil || *listener.TLS.Mode == gatewayv1.TLSModeTerminate) && (listener.Hostname == nil || kubernetesbinding.MatchesHostname(string(*listener.Hostname), hostname))
	if compatible {
		add("ListenerCompatible", metav1.ConditionTrue, "Compatible")
	} else {
		add("ListenerCompatible", metav1.ConditionFalse, "UnsupportedListener")
	}
	attached := false
	if namespace == nil {
		add("AttachmentAllowed", metav1.ConditionUnknown, "NamespaceNotObserved")
	} else {
		from := gatewayv1.NamespacesFromSame
		allowed := listener.AllowedRoutes
		kinds := true
		if allowed != nil && len(allowed.Kinds) > 0 {
			kinds = false
			for _, k := range allowed.Kinds {
				if k.Kind == "HTTPRoute" && (k.Group == nil || *k.Group == gatewayv1.GroupName) {
					kinds = true
				}
			}
		}
		if allowed != nil && allowed.Namespaces != nil && allowed.Namespaces.From != nil {
			from = *allowed.Namespaces.From
		}
		switch from {
		case gatewayv1.NamespacesFromSame:
			attached = namespace.Name == gateway.Namespace
		case gatewayv1.NamespacesFromAll:
			attached = true
		case gatewayv1.NamespacesFromSelector:
			if allowed != nil && allowed.Namespaces != nil && allowed.Namespaces.Selector != nil {
				selector, err := metav1.LabelSelectorAsSelector(allowed.Namespaces.Selector)
				attached = err == nil && selector.Matches(labels.Set(namespace.Labels))
			}
		}
		if attached && kinds {
			add("AttachmentAllowed", metav1.ConditionTrue, "Allowed")
		} else {
			add("AttachmentAllowed", metav1.ConditionFalse, "NotAllowed")
		}
	}
	var listenerStatus *gatewayv1.ListenerStatus
	for i := range gateway.Status.Listeners {
		if gateway.Status.Listeners[i].Name == listener.Name {
			listenerStatus = &gateway.Status.Listeners[i]
			break
		}
	}
	for _, kind := range []string{"Accepted", "Programmed", "ResolvedRefs"} {
		if listenerStatus == nil {
			add("Listener"+kind, metav1.ConditionUnknown, "NotObserved")
		} else {
			copyCondition("Listener"+kind, listenerStatus.Conditions, kind, gateway.Generation)
		}
	}
	supported := false
	if listenerStatus != nil {
		for _, k := range listenerStatus.SupportedKinds {
			if k.Kind == "HTTPRoute" && (k.Group == nil || *k.Group == gatewayv1.GroupName) {
				supported = true
			}
		}
	}
	if supported {
		add("HTTPRouteSupported", metav1.ConditionTrue, "Supported")
	} else {
		add("HTTPRouteSupported", metav1.ConditionUnknown, "NotObserved")
	}
	add("CertificateMaterialInspected", metav1.ConditionUnknown, "NotInspected")
	add("ServedTLSVerified", metav1.ConditionUnknown, "NotInspected")
	return f
}
