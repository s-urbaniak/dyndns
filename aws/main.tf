// variables

variable "extra_tags" {
  type = "map"
}

variable "ssh_key" {
  type = "string"
}

variable "dyndns_version" {
  type = "string"
}

variable "dyndns_zone" {
  type = "string"
}

variable "dyndns_secret" {
  type = "string"
}

variable "dyndns_port" {
  type = "string"
}

variable "cidr_block" {
  type    = "string"
  default = "10.0.0.0/16"
}

// outputs

output "ip" {
  value = "${aws_instance.dyndns.public_ip}"
}

output "base64_secret" {
  value = "${base64encode(var.dyndns_secret)}"
}

// ignition

data "ignition_config" "dyndns" {
  systemd = [
    "${data.ignition_systemd_unit.dyndns.id}",
  ]
}

data "ignition_systemd_unit" "dyndns" {
  name   = "dyndns.service"
  enable = true

  content = <<EOF
[Unit]
Description=dynDNS

[Service]
TimeoutStartSec=0
ExecStartPre=-/usr/bin/docker kill dyndns
ExecStartPre=-/usr/bin/docker rm dyndns
ExecStartPre=/usr/bin/docker pull quay.io/surbaniak/dyndns:${var.dyndns_version}
ExecStart=/usr/bin/docker run \
    --name dyndns \
    -p${var.dyndns_port}:${var.dyndns_port}/udp \
    quay.io/surbaniak/dyndns:${var.dyndns_version} \
    -tsig=${var.dyndns_zone}.:${base64encode(var.dyndns_secret)} \
    -port=${var.dyndns_port}
ExecStop=/usr/bin/docker stop dyndns

[Install]
WantedBy=multi-user.target
EOF
}

// network

resource "aws_security_group" "dyndns" {
  tags = "${merge(map(
    "Name", "sur-dyndns",
    ) ,var.extra_tags)}"

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    self        = true
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    protocol    = "icmp"
    cidr_blocks = ["0.0.0.0/0"]
    from_port   = 0
    to_port     = 0
  }

  ingress {
    protocol    = "tcp"
    from_port   = 22
    to_port     = 22
    self        = true
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    protocol    = "udp"
    from_port   = 53
    to_port     = 53
    self        = true
    cidr_blocks = ["0.0.0.0/0"]
  }
}

// instance

data "aws_ami" "coreos" {
  most_recent = true

  filter {
    name   = "name"
    values = ["CoreOS-stable-*"]
  }

  filter {
    name   = "architecture"
    values = ["x86_64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }

  filter {
    name   = "owner-id"
    values = ["595879546273"]
  }
}

resource "aws_instance" "dyndns" {
  ami                         = "${data.aws_ami.coreos.image_id}"
  instance_type               = "t2.micro"
  associate_public_ip_address = true
  key_name                    = "${var.ssh_key}"
  user_data                   = "${data.ignition_config.dyndns.rendered}"
  vpc_security_group_ids      = ["${aws_security_group.dyndns.id}"]

  lifecycle {
    # Ignore changes in the AMI which force recreation of the resource. This
    # avoids accidental deletion of nodes whenever a new CoreOS Release comes
    # out.
    ignore_changes = ["ami"]
  }

  tags = "${merge(map(
    "Name", "sur-dyndns",
    ) ,var.extra_tags)}"
}
