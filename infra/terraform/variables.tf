variable "profile" {
  description = "OCI CLI profile used by Terraform"
  type        = string
  default     = "syscode-api"
}

variable "region" {
  description = "OCI region for identity-domain home and provider operations"
  type        = string
  default     = "uk-london-1"
}

variable "tenancy_ocid" {
  description = "Tenancy OCID (root compartment OCID)"
  type        = string
}

variable "parent_compartment_ocid" {
  description = "Parent compartment OCID for homelab compartment (syscode-labs)"
  type        = string
  default     = "ocid1.compartment.oc1..aaaaaaaajqj5pv4t5fdefradmod4qv2vl2vshnmxbcj6qmcummv7mgs5i3oq"
}

variable "homelab_compartment_name" {
  description = "Compartment name for homelab resources"
  type        = string
  default     = "homelab"
}

variable "homelab_domain_name" {
  description = "Identity domain display name"
  type        = string
  default     = "homelab"
}

variable "homelab_api_user_name" {
  description = "IAM user name for API access"
  type        = string
  default     = "homelab-api"
}

variable "homelab_api_user_email" {
  description = "IAM user email required by domain-enabled tenancies"
  type        = string
  default     = "homelab-api@syscode-labs.com"
}

variable "homelab_api_public_key_path" {
  description = "Path to the public API signing key PEM for homelab-api"
  type        = string
  default     = "~/.oci/homelab-api_public.pem"
}

variable "homelab_api_group_name" {
  description = "IAM group name for homelab read access"
  type        = string
  default     = "homelab-api-readers"
}

variable "homelab_api_policy_name" {
  description = "IAM policy name for homelab read access"
  type        = string
  default     = "homelab-api-read-policy"
}
