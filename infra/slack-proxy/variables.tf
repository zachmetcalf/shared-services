# Copyright (c) 2026 Zach Metcalf. All Rights Reserved.

variable "project_id" {
  description = "Google Cloud project ID"
  type        = string
}

variable "region" {
  description = "Google Cloud region"
  type        = string
  default     = "us-west1"
}

variable "zone" {
  description = "Google Cloud zone"
  type        = string
  default     = "us-west1-a"
}

variable "name" {
  description = "Base resource name"
  type        = string
  default     = "slack-proxy"
}

variable "machine_type" {
  description = "Compute Engine machine type"
  type        = string
  default     = "e2-micro"
}

variable "network" {
  description = "VPC network name"
  type        = string
  default     = "default"
}

variable "boot_disk_size_gb" {
  description = "Boot disk size in GB"
  type        = number
  default     = 10
}

variable "boot_image_family" {
  description = "Ubuntu image family"
  type        = string
  default     = "ubuntu-2404-lts-amd64"
}

variable "deploy_user" {
  description = "Deploy SSH user"
  type        = string
  default     = "slack-proxy"
}

variable "deploy_public_key_path" {
  description = "Deploy public key path"
  type        = string
  default     = "~/.ssh/slack_proxy_deploy.pub"
}

variable "deploy_private_key_path" {
  description = "Deploy private key path"
  type        = string
  default     = "~/.ssh/slack_proxy_deploy"
}

variable "ssh_source_ranges" {
  description = "Allowed SSH source CIDR ranges"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}
