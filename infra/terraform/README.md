# Homelab IAM Terraform

This directory captures the OCI IAM/domain setup done manually for `homelab`:

- Compartment: `homelab` under `syscode-labs`
- Identity domain: `homelab`
- IAM user: `homelab-api`
- API key upload for `homelab-api`
- Group: `homelab-api-readers`
- Policy: `homelab-api-read-policy`
- User-group membership for `homelab-api`

Current live policy statements:
- `Allow group homelab-api-readers to inspect compartments in tenancy`
- `Allow group homelab-api-readers to inspect limits in tenancy`
- `Allow group homelab-api-readers to manage instance-family in compartment syscode-labs:homelab`
- `Allow group homelab-api-readers to manage virtual-network-family in compartment syscode-labs:homelab`
- `Allow group homelab-api-readers to manage volume-family in compartment syscode-labs:homelab`

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

terraform import -var "tenancy_ocid=<your-tenancy-ocid>" \
  oci_identity_domain.homelab \
  ocid1.domain.oc1..aaaaaaaa3vt4s62f6ozzvbl4dxzro6nfwdwzbd54vrlufg5me7zc7enrheba

terraform import -var "tenancy_ocid=<your-tenancy-ocid>" \
  oci_identity_user.homelab_api \
  ocid1.user.oc1..aaaaaaaacyoq5itvjlaxn2gejmk6m3il4vj3og4rbrbt7br4oxhrbh4aetrq

terraform import -var "tenancy_ocid=<your-tenancy-ocid>" \
  oci_identity_group.homelab_api_readers \
  ocid1.group.oc1..aaaaaaaav4w5fmoaz5slsmsiihedyaxz42jnkhvcm35ezfqyuc4co4p42ecq

terraform import -var "tenancy_ocid=<your-tenancy-ocid>" \
  oci_identity_policy.homelab_api_read_policy \
  ocid1.policy.oc1..aaaaaaaa7ysr6kbvussx64mtrh4zvzc4p3x4ecm5t2dle5gx5mmostwayziq
```

For `oci_identity_api_key` and `oci_identity_user_group_membership`, import IDs are provider-specific and can be resolved after listing those resources via OCI CLI.
