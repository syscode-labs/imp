/*
Copyright 2026.

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

package v1alpha1

import (
	"context"
	"fmt"
	"net"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	sandboxv1alpha1 "github.com/syscode-labs/imp/api/sandbox/v1alpha1"
	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// sandboxOwnerLabel aliases the sandbox add-on's owner label so admission
// and the sandbox controller cannot drift apart.
const sandboxOwnerLabel = sandboxv1alpha1.SandboxOwnerLabel

var impnetworkattachmentlog = logf.Log.WithName("impnetworkattachment-webhook")

// ImpNetworkAttachmentWebhook implements defaulting and validation for
// ImpNetworkAttachment. It stamps requester identity at CREATE time and
// enforces allowlist membership, DHCP policy, static-address correctness,
// subject restrictions, and immutability of spec and audit annotations.
type ImpNetworkAttachmentWebhook struct {
	// Client resolves ClusterImpConfig attachment definitions.
	Client client.Client
}

var (
	_ admission.Defaulter[*impdevv1alpha1.ImpNetworkAttachment] = &ImpNetworkAttachmentWebhook{}
	_ admission.Validator[*impdevv1alpha1.ImpNetworkAttachment] = &ImpNetworkAttachmentWebhook{}
)

// Default implements admission.Defaulter for ImpNetworkAttachment.
func (w *ImpNetworkAttachmentWebhook) Default(ctx context.Context, att *impdevv1alpha1.ImpNetworkAttachment) error {
	if att.Spec.Mode == "" {
		att.Spec.Mode = impdevv1alpha1.AttachmentModeAccess
	}

	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return nil // no request in context (e.g. direct unit test) — nothing to stamp
	}
	if req.Operation != admissionv1.Create {
		return nil
	}
	if att.Annotations == nil {
		att.Annotations = map[string]string{}
	}
	att.Annotations[impdevv1alpha1.AnnotationRequester] = req.UserInfo.Username
	att.Annotations[impdevv1alpha1.AnnotationRequesterGroups] = strings.Join(req.UserInfo.Groups, ",")
	return nil
}

// ValidateCreate implements admission.Validator for ImpNetworkAttachment.
func (w *ImpNetworkAttachmentWebhook) ValidateCreate(ctx context.Context, att *impdevv1alpha1.ImpNetworkAttachment) (admission.Warnings, error) {
	return nil, w.validateSpec(ctx, att).ToAggregate()
}

// ValidateUpdate implements admission.Validator for ImpNetworkAttachment.
// Spec and audit annotations are immutable after creation.
func (w *ImpNetworkAttachmentWebhook) ValidateUpdate(_ context.Context, oldAtt, newAtt *impdevv1alpha1.ImpNetworkAttachment) (admission.Warnings, error) {
	var allErrs field.ErrorList

	if !equality.Semantic.DeepEqual(oldAtt.Spec, newAtt.Spec) {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec"),
			"spec is immutable after creation; delete and recreate the attachment"))
	}
	for _, ann := range []string{impdevv1alpha1.AnnotationRequester, impdevv1alpha1.AnnotationRequesterGroups} {
		if oldAtt.Annotations[ann] != newAtt.Annotations[ann] {
			allErrs = append(allErrs, field.Forbidden(field.NewPath("metadata", "annotations").Key(ann),
				"requester audit annotations are immutable"))
		}
	}
	return nil, allErrs.ToAggregate()
}

// ValidateDelete implements admission.Validator for ImpNetworkAttachment.
// Deletion is denied while the referenced VM is running so host teardown always
// flows through the VM stop path, which owns bridge/subinterface cleanup.
// The imp.dev/force-delete annotation overrides for break-glass use.
func (w *ImpNetworkAttachmentWebhook) ValidateDelete(ctx context.Context, att *impdevv1alpha1.ImpNetworkAttachment) (admission.Warnings, error) {
	if att.Annotations[impdevv1alpha1.AnnotationForceDelete] == "true" || w.Client == nil {
		return nil, nil
	}
	vm := &impdevv1alpha1.ImpVM{}
	if err := w.Client.Get(ctx, client.ObjectKey{
		Namespace: att.Namespace,
		Name:      att.Spec.VMRef.Name,
	}, vm); err != nil {
		return nil, nil // VM gone — nothing left to guard
	}
	switch vm.Status.Phase {
	case impdevv1alpha1.VMPhaseRunning, impdevv1alpha1.VMPhaseStarting:
		return nil, field.Forbidden(field.NewPath("metadata", "name"),
			"ImpVM is running; stop the VM before deleting its attachment")
	}
	return nil, nil
}

// validateSpec checks a CREATE against the allowlisted definitions.
func (w *ImpNetworkAttachmentWebhook) validateSpec(ctx context.Context, att *impdevv1alpha1.ImpNetworkAttachment) field.ErrorList {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	if att.Spec.VMRef.Name == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("vmRef", "name"), "target ImpVM is required"))
	}
	if att.Spec.AttachmentRef == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("attachmentRef"), "attachment definition name is required"))
		return allErrs
	}

	// Sandbox isolation guard: sandbox-owned VMs rely on host egress deny
	// rules scoped to their primary subnet. A second LAN address would let
	// the guest source traffic outside that subnet and bypass the baseline,
	// so such attachments are refused at admission (fail closed on lookup
	// errors other than NotFound).
	if w.Client != nil && att.Spec.VMRef.Name != "" {
		vm := &impdevv1alpha1.ImpVM{}
		err := w.Client.Get(ctx, client.ObjectKey{
			Namespace: att.Namespace,
			Name:      att.Spec.VMRef.Name,
		}, vm)
		switch {
		case err == nil:
			if owner, owned := vm.Labels[sandboxOwnerLabel]; owned {
				allErrs = append(allErrs, field.Forbidden(specPath.Child("vmRef"),
					fmt.Sprintf("ImpVM %q is sandbox-owned (label %q=%q): a privileged LAN interface would give the guest a second source IP outside its sandbox subnet, bypassing the sandbox egress-deny rules",
						att.Spec.VMRef.Name, sandboxOwnerLabel, owner)))
			}
		case apierrors.IsNotFound(err):
			// Target VM absent — definition resolution above already
			// governs whether the attachment can proceed.
		default:
			allErrs = append(allErrs, field.InternalError(specPath.Child("vmRef"), err))
		}
	}

	if att.Spec.Mode != "" && att.Spec.Mode != impdevv1alpha1.AttachmentModeAccess {
		allErrs = append(allErrs, field.NotSupported(specPath.Child("mode"), att.Spec.Mode,
			[]string{string(impdevv1alpha1.AttachmentModeAccess)}))
	}
	if att.Spec.DHCP != nil && att.Spec.DHCP.Enabled && att.Spec.IP != "" {
		allErrs = append(allErrs, field.Invalid(specPath.Child("ip"), att.Spec.IP,
			"ip must not be set when DHCP is requested"))
	}

	def := w.resolveDefinition(ctx, att)
	if def == nil {
		if w.Client == nil {
			allErrs = append(allErrs, field.InternalError(specPath.Child("attachmentRef"),
				fmt.Errorf("webhook has no client to resolve definitions")))
			return allErrs
		}
		allErrs = append(allErrs, field.NotFound(specPath.Child("attachmentRef"), att.Spec.AttachmentRef))
		return allErrs
	}

	requestDHCP := att.Spec.DHCP != nil && att.Spec.DHCP.Enabled
	switch {
	case requestDHCP && !def.AllowDHCP:
		allErrs = append(allErrs, field.Forbidden(specPath.Child("dhcp"),
			"attachment definition does not permit DHCP"))
	case !requestDHCP && att.Spec.IP == "":
		allErrs = append(allErrs, field.Required(specPath.Child("ip"),
			"a static ip inside the definition subnet is required when DHCP is not requested"))
	}
	if !requestDHCP && att.Spec.IP != "" {
		if err := validateIPInSubnet(att.Spec.IP, def.SubnetCIDR); err != nil {
			allErrs = append(allErrs, field.Invalid(specPath.Child("ip"), att.Spec.IP, err.Error()))
		}
	}

	req, err := admission.RequestFromContext(ctx)
	if err == nil && !def.Permits(req.UserInfo.Username, req.UserInfo.Groups) {
		allErrs = append(allErrs, field.Forbidden(specPath.Child("attachmentRef"),
			fmt.Sprintf("subject %q is not allowed to use attachment definition %q",
				req.UserInfo.Username, def.Name)))
	}

	impnetworkattachmentlog.Info("Validated ImpNetworkAttachment creation",
		"name", att.GetName(), "definition", att.Spec.AttachmentRef)
	return allErrs
}

// resolveDefinition fetches the allowlist entry referenced by the attachment.
// Returns nil when the definition cannot be resolved.
func (w *ImpNetworkAttachmentWebhook) resolveDefinition(ctx context.Context, att *impdevv1alpha1.ImpNetworkAttachment) *impdevv1alpha1.LANAttachmentSpec {
	if w.Client == nil || att.Spec.AttachmentRef == "" {
		return nil
	}
	cfg := &impdevv1alpha1.ClusterImpConfig{}
	if err := w.Client.Get(ctx, client.ObjectKey{Name: "cluster"}, cfg); err != nil {
		return nil
	}
	for i := range cfg.Spec.Networking.LANAttachments {
		if cfg.Spec.Networking.LANAttachments[i].Name == att.Spec.AttachmentRef {
			return &cfg.Spec.Networking.LANAttachments[i]
		}
	}
	return nil
}

// validateIPInSubnet verifies ip parses and falls inside cidr.
func validateIPInSubnet(ipStr, cidr string) error {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return fmt.Errorf("%q is not a valid IPv4 address", ipStr)
	}
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("attachment definition subnet %q is invalid: %w", cidr, err)
	}
	if !ipNet.Contains(ip) {
		return fmt.Errorf("%s is outside attachment subnet %s", ipStr, cidr)
	}
	return nil
}
