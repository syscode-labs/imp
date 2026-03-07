terraform {
  required_version = ">= 1.5.0"
}

provider "oci" {
  region  = var.region
  profile = var.profile
}

resource "oci_identity_compartment" "homelab" {
  compartment_id = var.parent_compartment_ocid
  name           = var.homelab_compartment_name
  description    = "homelab compartment"
}

resource "oci_identity_domain" "homelab" {
  compartment_id = oci_identity_compartment.homelab.id
  display_name   = var.homelab_domain_name
  description    = "homelab identity domain"
  home_region    = var.region
  license_type   = "free"
}

resource "oci_identity_user" "homelab_api" {
  compartment_id = var.tenancy_ocid
  name           = var.homelab_api_user_name
  description    = "API user for homelab automation"
  email          = var.homelab_api_user_email
}

resource "oci_identity_api_key" "homelab_api_key" {
  user_id   = oci_identity_user.homelab_api.id
  key_value = file(pathexpand(var.homelab_api_public_key_path))
}

resource "oci_identity_group" "homelab_api_readers" {
  compartment_id = var.tenancy_ocid
  name           = var.homelab_api_group_name
  description    = "Read access for homelab automation user"
}

resource "oci_identity_user_group_membership" "homelab_api_membership" {
  user_id  = oci_identity_user.homelab_api.id
  group_id = oci_identity_group.homelab_api_readers.id
}

resource "oci_identity_policy" "homelab_api_read_policy" {
  compartment_id = var.tenancy_ocid
  name           = var.homelab_api_policy_name
  description    = "Read policy for homelab API user"
  statements = [
    "Allow group ${var.homelab_api_group_name} to inspect compartments in tenancy",
    "Allow group ${var.homelab_api_group_name} to read all-resources in compartment ${var.homelab_compartment_name}",
  ]
}
