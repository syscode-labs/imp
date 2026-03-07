packer {
  required_version = ">= 1.9.0"
}

variable "output_env_file" {
  type    = string
  default = "/tmp/imp-golden.env"
}

source "null" "oci_golden_image" {
  communicator = "none"
}

build {
  name    = "oci-golden-image-via-script"
  sources = ["source.null.oci_golden_image"]

  provisioner "shell-local" {
    inline = [
      "set -euo pipefail",
      "OCI_OUTPUT_ENV_FILE='${var.output_env_file}' '${path.root}/oci-build-golden-image.sh'",
      "echo \"Wrote OCI image metadata to ${var.output_env_file}\"",
    ]
  }
}
