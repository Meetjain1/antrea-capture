Antrea capture controller demo.

Build image, load into Kind, apply `deploy/daemonset.yaml` and `deploy/test-pod.yaml`, then annotate the test Pod with `tcpdump.antrea.io: "5"` and remove it to stop capture.
