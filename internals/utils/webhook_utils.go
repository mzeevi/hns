package utils

import (
	"context"
	"fmt"
	danav1 "github.com/dana-team/hns/api/v1"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// ValidateNamespaceExist validates that a namespace exists
func ValidateNamespaceExist(ns *ObjectContext) admission.Response {
	if !(ns.IsPresent()) {
		message := fmt.Sprintf("namespace %q does not exist", ns.Object.GetName())
		return admission.Denied(message)
	}

	return admission.Allowed("")
}

// ValidateToNamespaceName validates that a namespace is not trying to be migrated
// to be under the same namespace it's already in
func ValidateToNamespaceName(ns *ObjectContext, toNSName string) admission.Response {
	currentParent := GetNamespaceParent(ns.Object)

	if toNSName == currentParent {
		message := fmt.Sprintf("%q is already under %q", ns.Object.GetName(), toNSName)
		return admission.Denied(message)
	}

	return admission.Allowed("")
}

// ValidateSecondaryRoot denies if trying to perform UpdateQuota involving namesapces from different secondary root namespaces
// a secondary root is the first subnamespace after the root namespace in the hierarchy of a subnamespace
func ValidateSecondaryRoot(ctx context.Context, c client.Client, aNSArray, bNSArray []string) admission.Response {
	logger := log.FromContext(ctx)

	aNSSecondaryRootName := aNSArray[1]
	bNSSecondaryRootName := bNSArray[1]

	if aNSSecondaryRootName == "" || bNSSecondaryRootName == "" {
		message := fmt.Sprintf("it is forbidden to do operations on subnamespaces without a set display-name")
		return admission.Denied(message)
	}

	aNSSecondaryRoot, err := NewObjectContext(ctx, c, client.ObjectKey{Name: aNSSecondaryRootName}, &corev1.Namespace{})
	if err != nil {
		logger.Error(err, "failed to create object", "sourceNSSecondaryRoot", aNSSecondaryRootName)
		return admission.Errored(http.StatusBadRequest, err)
	}

	bNSSecondaryRoot, err := NewObjectContext(ctx, c, client.ObjectKey{Name: bNSSecondaryRootName}, &corev1.Namespace{})
	if err != nil {
		logger.Error(err, "failed to create object", "destNSSecondaryRoot", bNSSecondaryRootName)
		return admission.Errored(http.StatusBadRequest, err)
	}

	if IsSecondaryRootNamespace(aNSSecondaryRoot.Object) || IsSecondaryRootNamespace(bNSSecondaryRoot.Object) {
		if aNSSecondaryRootName != bNSSecondaryRootName {
			message := fmt.Sprintf("it is forbidden to perform operations between subnamespaces from hierarchy %q and "+
				"subnamespaces from hierarchy %q", aNSSecondaryRootName, bNSSecondaryRootName)
			return admission.Denied(message)
		}
	}

	return admission.Allowed("")
}

// ValidatePermissions checks if a reqistered user has the needed permissions on the namespaces and denies otherwise
// there are 3 scenarios in which things are allowed: if the user has the needed permissions on the Ancestor
// of the two namespaces; if the user has the needed permissions on both namespaces; if the user has the needed
// permissions on the namespace from which resources are moved and both namespaces are in the same branch
// (only checked when the branch flag is true)
func ValidatePermissions(ctx context.Context, aNS []string, aNSName, bNSName, ancestorNSName, reqUser string, branch bool) admission.Response {
	hasSourcePermissions := PermissionsExist(ctx, reqUser, aNSName)
	hasDestPermissions := PermissionsExist(ctx, reqUser, bNSName)
	hasAncestorPermissions := PermissionsExist(ctx, reqUser, ancestorNSName)

	inBranch := ContainsString(aNS, bNSName)

	if branch {
		if !hasAncestorPermissions && !(hasSourcePermissions && hasDestPermissions) && !(hasSourcePermissions && inBranch) {
			message := fmt.Sprintf("you must have permissions on: %q and %q, or permissions on %q, to perform "+
				"this operation. Having permissions only on %q, is enough just when resources are moved in the same branch of the hierarchy",
				aNSName, bNSName, ancestorNSName, aNSName)
			return admission.Denied(message)
		}
	} else {
		if !hasAncestorPermissions && !(hasSourcePermissions && hasDestPermissions) {
			message := fmt.Sprintf("you must have permissions on: %q and %q, or permissions on %q, to perform "+
				"this operation", aNSName, bNSName, ancestorNSName)
			return admission.Denied(message)
		}
	}

	return admission.Allowed("")
}

// PermissionsExist checks if a user has permission to create a pod in a given namespace.
// It impersonates the reqUser and uses SelfSubjectAccessReview API to check if the action is allowed or denied.
// It returns a boolean value indicating whether the user has permission to create the pod or not
func PermissionsExist(ctx context.Context, reqUser, namespace string) bool {
	if reqUser == fmt.Sprintf("system:serviceaccount:%s:%s", danav1.SNSNamespace, danav1.SNSServiceAccount) {
		return true
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	// set the user to impersonate in the configuration
	config.Impersonate = rest.ImpersonationConfig{
		UserName: reqUser,
	}

	// create a new Kubernetes client using the configuration
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	// create a new SelfSubjectAccessReview API object for checking permissions
	action := authv1.ResourceAttributes{
		Namespace: namespace,
		Verb:      "create",
		Resource:  "pods",
	}

	selfCheck := authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &action,
		},
	}

	// check the permissions for the user
	resp, err := clientSet.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &selfCheck, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	// check the response status to determine whether the user has permission to create the pod or not
	if resp.Status.Denied {
		return false
	}
	if resp.Status.Allowed {
		return true
	}
	return false
}
