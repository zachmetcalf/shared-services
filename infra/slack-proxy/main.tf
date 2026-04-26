# Copyright (c) 2026 Zach Metcalf. All Rights Reserved.

locals {
  network_tag = "slack-proxy"

  startup_script = <<-EOT
    #!/usr/bin/env bash
    set -euo pipefail

    export DEBIAN_FRONTEND=noninteractive

    apt-get update
    apt-get install -y docker.io caddy

    systemctl enable --now docker
    systemctl enable --now caddy

    mkdir -p /var/lib/slack-proxy /etc/slack-proxy
    chown 65532:65532 /var/lib/slack-proxy
    chmod 700 /var/lib/slack-proxy
    chmod 755 /etc/slack-proxy
  EOT
}

data "google_compute_image" "ubuntu" {
  family  = var.boot_image_family
  project = "ubuntu-os-cloud"
}

resource "google_compute_address" "slack_proxy" {
  name   = "${var.name}-ip"
  region = var.region
}

resource "google_compute_firewall" "slack_proxy_ssh" {
  name    = "${var.name}-allow-ssh"
  network = var.network

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = var.ssh_source_ranges
  target_tags   = [local.network_tag]
}

resource "google_compute_firewall" "slack_proxy_web" {
  name    = "${var.name}-allow-web"
  network = var.network

  allow {
    protocol = "tcp"
    ports    = ["80", "443"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = [local.network_tag]
}

resource "google_compute_instance" "slack_proxy" {
  name         = var.name
  machine_type = var.machine_type
  tags         = [local.network_tag]

  boot_disk {
    initialize_params {
      image = data.google_compute_image.ubuntu.self_link
      size  = var.boot_disk_size_gb
      type  = "pd-balanced"
    }
  }

  metadata = {
    "enable-oslogin" = "FALSE"
    "ssh-keys"       = "${var.deploy_user}:${trimspace(file(pathexpand(var.deploy_public_key_path)))}"
  }

  metadata_startup_script = local.startup_script

  network_interface {
    network = var.network

    access_config {
      nat_ip = google_compute_address.slack_proxy.address
    }
  }
}
