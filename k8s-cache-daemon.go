package main

import (
	"flag"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/haxii/daemon"
	"github.com/rjeczalik/notify"
	av1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	nv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	configDir = flag.String("configDir", "", "absolute path to the dir containing configurations")
	_         = flag.String("s", daemon.UsageDefaultName, daemon.UsageMessage)
)

type fakeEvent struct{ path string }

func (e *fakeEvent) Event() notify.Event { return notify.Create }
func (e *fakeEvent) Path() string        { return e.path }
func (e *fakeEvent) Sys() interface{}    { return nil }

func loadExistingDirs(dirWatcher chan notify.EventInfo) {
	err := filepath.Walk(*configDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, "kubeconfig") {
			dirWatcher <- &fakeEvent{path}
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	flag.Parse()
	dirWatcher := make(chan notify.EventInfo, 100)
	go loadExistingDirs(dirWatcher)
	if err := notify.Watch(*configDir, dirWatcher, notify.Create); err != nil {
		log.Fatal(err)
	}
	for {
		e := <-dirWatcher
		fi, err := os.Stat(e.Path())
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("event for %s", e.Path())
		if fi.Mode().IsDir() {
			if path.Dir(e.Path()) == *configDir {
				log.Printf("watching subdir %s", e.Path())
				if err := notify.Watch(e.Path(), dirWatcher, notify.Create); err != nil {
					log.Fatal(err)
				}
			}
		} else if strings.HasSuffix(e.Path(), "kubeconfig") {
			log.Printf("new kubeconfig in %s", e.Path())
			go watch(e.Path())
		}
	}
}

func watch(kubeconfig string) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Fatal(err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	pods, _ := clientset.CoreV1().Pods("").Watch(metav1.ListOptions{})
	netpol, _ := clientset.NetworkingV1().NetworkPolicies("").Watch(metav1.ListOptions{})
	deploys, _ := clientset.AppsV1().Deployments("").Watch(metav1.ListOptions{})

	podChan := pods.ResultChan()
	netChan := netpol.ResultChan()
	deployChan := deploys.ResultChan()

	for {
		select {
		case e := <-podChan:
			o := e.Object.(*v1.Pod)
			if o.Namespace == "kube-system" {
				continue
			}
			log.Printf("pod %s for %s in %s (%s)\n", e.Type, o.Name, o.Status.Phase, o.Namespace)
		case e := <-netChan:
			o := e.Object.(*nv1.NetworkPolicy)
			if o.Namespace == "kube-system" {
				continue
			}
			log.Printf("nc %s %s (%s)\n", e.Type, o.Name, o.Namespace)
		case e := <-deployChan:
			o := e.Object.(*av1.Deployment)
			if o.Namespace == "kube-system" {
				continue
			}
			log.Printf("deploy %s %s (%s)\n", e.Type, o.Name, o.Namespace)
		}

	}

}

/*
func main() {
	daemon.Make("-s", "k8s-cache-daemon", "k8s cache daemon").Run(run)
}*/
