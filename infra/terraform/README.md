# Homelab IAM Terraform

This directory captures the OCI IAM/domain setup done manually for `homelab`:

- Compartment: `homelab` under `syscode-labs`
- Identity domain: `homelab`
- IAM user: `homelab-api`
- API key upload for `homelab-api`
- Group: `homelab-api-readers`
- Policy: `homelab-api-read-policy`
- User-group membership for `homelab-api`

## Prerequisites

- Terraform >= 1.5
- OCI provider credentials available under profile `syscode-api` (or override `var.profile`)
- Admin-level identity permissions for create/import operations

## Usage

```bash
cd infra/terraform
terraform init
terraform plan -var "tenancy_ocid=<your-tenancy-ocid>"
terraform apply -var "tenancy_ocid=<your-tenancy-ocid>"
```

## Import Existing Resources

If resources already exist (as in this environment), import them before first apply:

```bash
cd infra/terraform

terraform import -var "tenancy_ocid=<your-tenancy-ocid>" \
  oci_identity_compartment.homelab \
  ocid1.compartment.oc1..aaaaaaaao6uhlquzyyxx5t4h3xnduflnughzm6xasuo2pag3o5hiwkqwcpaq

# Replace <domain-ocid> with the domain OCID from OCI Console if needed.
terraform import -var "tenancy_ocid=<your-tenancy-ocid>" \
  oci_identity_domain.homelab \
  <domain-ocid>

terraform import -var "tenancy_ocid=<your-tenancy-ocid>" \
  oci_identity_user.homelab_api \
  ocid1.user.oc1..aaaaaaaacyoq5itvjlaxn2gejmk6m3il4vj3og4rbrbt7br4oxhrbh4aetrq

terraform import -var "tenancy_ocid=<your-tenancy-ocid>" \
  oci_identity_group.homelab_api_readers \
  ocid1.group.oc1..aaaaaaaav4w5fmoaz5slsmsiihedyaxz42jnkhvcm35ezfqyuc4co4p42ecq

# Replace with policy OCID once created/imported.
terraform import -var "tenancy_ocid=<your-tenancy-ocid>" \
  oci_identity_policy.homelab_api_read_policy \
  <policy-ocid>
```

For `oci_identity_api_key` and `oci_identity_user_group_membership`, import IDs are provider-specific and can be resolved after listing those resources via OCI CLI.
