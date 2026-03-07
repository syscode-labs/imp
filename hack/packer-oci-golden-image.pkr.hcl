packer {
  required_version = ">= 1.9.0"
  required_plugins {
    oracle = {
      source  = "github.com/hashicorp/oracle"
      version = ">= 1.0.0"
    }
  }
}

variable "compartment_ocid" { type = string }
variable "availability_domain" { type = string }
variable "subnet_ocid" { type = string }
variable "base_image_ocid" { type = string }
variable "image_name" { type = string }
variable "shape" {
  type    = string
  default = "VM.Standard.E2.1.Micro"
}
variable "ssh_username" {
  type    = string
  default = "ubuntu"
}
variable "firecracker_version" {
  type    = string
  default = "v1.9.0"
}
variable "required_go" { type = string }
variable "access_cfg_file" { type = string }

source "oracle-oci" "golden" {
  availability_domain = var.availability_domain
  access_cfg_file     = var.access_cfg_file
  base_image_ocid     = var.base_image_ocid
  compartment_ocid    = var.compartment_ocid
  image_name          = var.image_name
  shape               = var.shape
  ssh_username        = var.ssh_username
  subnet_ocid         = var.subnet_ocid
}

build {
  name    = "oci-golden-image-native"
  sources = ["source.oracle-oci.golden"]

  provisioner "shell" {
    environment_vars = [
      "FIRECRACKER_VERSION=${var.firecracker_version}",
      "REQUIRED_GO=${var.required_go}",
    ]
    scripts = ["${path.root}/packer-oci-provision.sh"]
  }
}
