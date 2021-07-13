package globalrolebinding

import (
	"fmt"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rancher/webhook/pkg/auth"
	v3 "github.com/rancher/webhook/pkg/generated/controllers/management.cattle.io/v3"
	objectsv3 "github.com/rancher/webhook/pkg/generated/objects/management.cattle.io/v3"
	"github.com/rancher/wrangler/pkg/webhook"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/utils/trace"
)

func NewValidator(grClient v3.GlobalRoleCache, escalationChecker *auth.EscalationChecker) webhook.Handler {
	return &globalRoleBindingValidator{
		escalationChecker: escalationChecker,
		globalRoles:       grClient,
	}
}

type globalRoleBindingValidator struct {
	escalationChecker *auth.EscalationChecker
	globalRoles       v3.GlobalRoleCache
}

func (grbv *globalRoleBindingValidator) Admit(response *webhook.Response, request *webhook.Request) error {
	listTrace := trace.New("globalRoleBindingValidator Admit", trace.Field{Key: "user", Value: request.UserInfo.Username})
	defer listTrace.LogIfLong(2 * time.Second)

	newGRB, err := objectsv3.GlobalRoleBindingObjectFromRequest(request)
	if err != nil {
		return err
	}

	// Pull the global role to get the rules
	globalRole, err := grbv.globalRoles.Get(newGRB.GlobalRoleName)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		switch request.Operation {
		case admissionv1.Delete: // allow delete operations if the GR is not found
			response.Allowed = true
			return nil
		case admissionv1.Update: // only allow updates to the finalizers if the GR is not found
			if newGRB.DeletionTimestamp != nil {
				response.Allowed = true
				return nil
			}
		}
		// other operations not allowed
		response.Result = &metav1.Status{
			Status:  "Failure",
			Message: fmt.Sprintf("referenced globalRole %s not found, only deletions allowed", newGRB.Name),
			Reason:  metav1.StatusReasonUnauthorized,
			Code:    http.StatusUnauthorized,
		}
		return nil
	}

	return grbv.escalationChecker.ConfirmNoEscalation(response, request, globalRole.Rules, "")
}
