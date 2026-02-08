package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const annotationKey = "tcpdump.antrea.io"

type captureProcess struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	n      int
}

type controller struct {
	nodeName string
	mu       sync.Mutex
	active   map[string]*captureProcess
}

func main() {
	kubeconfig := flag.String("kubeconfig", "", "")
	flag.Parse()

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Fatal("NODE_NAME is required")
	}

	config, err := buildConfig(*kubeconfig)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("client error: %v", err)
	}

	ctrl := &controller{
		nodeName: nodeName,
		active:   map[string]*captureProcess{},
	}

	factory := informers.NewSharedInformerFactoryWithOptions(clientset, 30*time.Second)
	podInformer := factory.Core().V1().Pods().Informer()

	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ctrl.onPod,
		UpdateFunc: func(_, newObj interface{}) { ctrl.onPod(newObj) },
		DeleteFunc: ctrl.onPodDelete,
	})

	stopCh := make(chan struct{})
	factory.Start(stopCh)

	if !cache.WaitForCacheSync(stopCh, podInformer.HasSynced) {
		log.Fatal("cache sync failed")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	close(stopCh)
	ctrl.shutdown()
}

func buildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}

func (c *controller) onPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}

	if pod.Spec.NodeName != c.nodeName {
		return
	}

	if pod.DeletionTimestamp != nil {
		c.stopCapture(pod.Name)
		return
	}

	ann, ok := pod.Annotations[annotationKey]
	if !ok {
		c.stopCapture(pod.Name)
		return
	}

	n, err := strconv.Atoi(ann)
	if err != nil || n <= 0 {
		c.stopCapture(pod.Name)
		return
	}

	c.startOrUpdate(pod.Name, n)
}

func (c *controller) onPodDelete(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if ok {
		c.stopCapture(pod.Name)
		return
	}
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		if pod, ok := tombstone.Obj.(*v1.Pod); ok {
			c.stopCapture(pod.Name)
		}
	}
}

func (c *controller) startOrUpdate(podName string, n int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	current, ok := c.active[podName]
	if ok && current.n == n {
		return
	}
	if ok {
		c.stopCaptureLocked(podName, current)
	}

	for i := 0; i < n; i++ {
		filePath := fmt.Sprintf("/capture-%s.pcap%d", podName, i)
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0666)
		if err == nil {
			_ = os.Chmod(filePath, 0666)
			_ = file.Close()
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	path := fmt.Sprintf("/capture-%s.pcap", podName)
	cmd := exec.CommandContext(ctx, "tcpdump", "-C", "1M", "-W", strconv.Itoa(n), "-w", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		log.Printf("tcpdump start failed for %s: %v", podName, err)
		return
	}

	c.active[podName] = &captureProcess{cmd: cmd, cancel: cancel, n: n}

	go func(name string, command *exec.Cmd) {
		err := command.Wait()
		if err != nil {
			log.Printf("tcpdump exited for %s: %v", name, err)
		}
	}(podName, cmd)
}

func (c *controller) stopCapture(podName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	current, ok := c.active[podName]
	if !ok {
		return
	}

	c.stopCaptureLocked(podName, current)
}

func (c *controller) stopCaptureLocked(podName string, current *captureProcess) {
	delete(c.active, podName)
	current.cancel()
	if current.cmd.Process != nil {
		_ = current.cmd.Process.Signal(syscall.SIGINT)
	}
	time.Sleep(1 * time.Second)
	c.cleanupFiles(podName)
}

func (c *controller) cleanupFiles(podName string) {
	matches, err := filepath.Glob(fmt.Sprintf("/capture-%s.pcap*", podName))
	if err != nil {
		return
	}
	for _, match := range matches {
		_ = os.Remove(match)
	}
}

func (c *controller) shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for name, proc := range c.active {
		proc.cancel()
		if proc.cmd.Process != nil {
			_ = proc.cmd.Process.Signal(syscall.SIGINT)
		}
		c.cleanupFiles(name)
	}
	c.active = map[string]*captureProcess{}
}

