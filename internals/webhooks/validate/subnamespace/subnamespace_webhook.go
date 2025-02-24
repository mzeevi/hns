package webhooks

import (
	"context"
	danav1 "github.com/dana-team/hns/api/v1"
	"github.com/dana-team/hns/internals/namespaceDB"
	"github.com/dana-team/hns/internals/utils"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/types"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type SubnamespaceAnnotator struct {
	Client      client.Client
	Decoder     *admission.Decoder
	NamespaceDB *namespaceDB.NamespaceDB
}

// +kubebuilder:webhook:path=/validate-v1-subnamespace,mutating=false,sideEffects=NoneOnDryRun,failurePolicy=fail,groups="dana.hns.io",resources=subnamespaces,verbs=create;update,versions=v1,name=subnamespace.dana.io,admissionReviewVersions=v1;v1beta1

// Handle implements the validation webhook
func (a *SubnamespaceAnnotator) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx).WithValues("webhook", "Subnamespace Webhook", "Name", req.Name)
	logger.Info("webhook request received")

	snsObject, err := utils.NewObjectContext(ctx, a.Client, types.NamespacedName{}, &danav1.Subnamespace{})
	if err != nil {
		logger.Error(err, "failed to create object context")
		return admission.Errored(http.StatusBadRequest, err)
	}

	if err := a.Decoder.DecodeRaw(req.Object, snsObject.Object); err != nil {
		logger.Error(err, "failed to decode object", "request object", req.Object)
		return admission.Errored(http.StatusBadRequest, err)
	}

	if req.Operation == admissionv1.Create {
		if response := a.handleCreate(snsObject); !response.Allowed {
			return response
		}
	}

	if req.Operation == admissionv1.Update {
		snsOldObject, err := utils.NewObjectContext(ctx, a.Client, types.NamespacedName{}, &danav1.Subnamespace{})
		if err != nil {
			logger.Error(err, "failed to create object context")
			return admission.Errored(http.StatusBadRequest, err)
		}

		if err := a.Decoder.DecodeRaw(req.OldObject, snsOldObject.Object); err != nil {
			logger.Error(err, "failed to decode object", "request object", req.OldObject)
			return admission.Errored(http.StatusBadRequest, err)
		}

		if response := a.handleUpdate(snsObject, snsOldObject); !response.Allowed {
			return response
		}
	}

	return admission.Allowed("all validations passed")
}
