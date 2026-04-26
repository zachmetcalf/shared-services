# Copyright (c) 2026 Zach Metcalf. All Rights Reserved.

output "gce_host" {
  description = "GCE host"
  value       = google_compute_address.slack_proxy.address
}

output "gce_user" {
  description = "GCE user"
  value       = var.deploy_user
}

output "ssh_command" {
  description = "SSH command"
  value       = "ssh -i ${var.deploy_private_key_path} ${var.deploy_user}@${google_compute_address.slack_proxy.address}"
}

output "fingerprint_command" {
  description = "SSH fingerprint command"
  value       = "ssh-keyscan -t ed25519 ${google_compute_address.slack_proxy.address} | ssh-keygen -lf -"
}
