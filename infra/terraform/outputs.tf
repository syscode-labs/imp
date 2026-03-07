output "homelab_compartment_ocid" {
  value       = oci_identity_compartment.homelab.id
  description = "OCID of the homelab compartment"
}

output "homelab_domain_ocid" {
  value       = oci_identity_domain.homelab.id
  description = "OCID of the homelab identity domain"
}

output "homelab_api_user_ocid" {
  value       = oci_identity_user.homelab_api.id
  description = "OCID of the homelab API IAM user"
}

output "homelab_api_key_fingerprint" {
  value       = oci_identity_api_key.homelab_api_key.fingerprint
  description = "Fingerprint of the uploaded API signing key"
}

output "homelab_api_group_ocid" {
  value       = oci_identity_group.homelab_api_readers.id
  description = "OCID of the homelab API readers group"
}

output "homelab_api_policy_ocid" {
  value       = oci_identity_policy.homelab_api_read_policy.id
  description = "OCID of the homelab read policy"
}
