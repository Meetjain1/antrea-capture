## Antrea Packet Capture Controller

This repository contains a minimal Kubernetes controller that enables on-demand packet capture for Pods using `tcpdump`.

The controller runs as a DaemonSet and performs packet capture only for Pods scheduled on the same Node.

---

## Approach

* A lightweight Go controller watches Pod events using the Kubernetes API.
* Each controller instance is node-local (runs once per node via DaemonSet).
* Pods are selected using an annotation:

  ```
  tcpdump.antrea.io: "<N>"
  ```

  where `N` defines the maximum number of rotated capture files.
* When the annotation is added or updated, the controller starts `tcpdump` for that Pod.
* When the annotation is removed or the Pod is deleted, the capture is stopped and all related files are cleaned up.

---

## Packet Capture Behavior

* Packet capture is performed using:

  ```
  tcpdump -C 1M -W <N> -w /capture-<pod>.pcap
  ```
* Files are rotated automatically by `tcpdump`.
* Capture runs only while the annotation is present.
* All generated pcap files are deleted during cleanup.

---

## Deployment Model

* Runs as a **DaemonSet**
* Uses **host networking** and **privileged access** to allow packet capture
* Uses a dedicated **ServiceAccount** with minimal RBAC permissions
* Only watches Pods on the local Node

---

## Verification

A simple traffic-generating Pod can be deployed to validate the behavior:

* Add the annotation to start capture
* Confirm pcap files are created and non-empty
* Remove the annotation
* Confirm capture stops and files are deleted

Verification outputs and sample artifacts are included in the `artifacts/` directory.

---

## Repository Structure

* `cmd/capture-controller/` – Go controller source
* `deploy/` – DaemonSet, RBAC, and example Pod manifests
* `artifacts/` – verification outputs
* `Dockerfile` – container image definition
* `kind-config.yaml` – local cluster configuration

---
