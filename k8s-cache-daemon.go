package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	cacheDir  = flag.String("cacheDir", "", "absolute path to the dir containing caches")
	_         = flag.String("s", daemon.UsageDefaultName, daemon.UsageMessage)
)

type fakeEvent struct{ path string }

func (e *fakeEvent) Event() notify.Event { return notify.Create }
func (e *fakeEvent) Path() string        { return e.path }
func (e *fakeEvent) Sys() interface{}    { return nil }

func loadDir(dir string, dirWatcher chan notify.EventInfo) {
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, "kubeconfig") {
			dirWatcher <- &fakeEvent{path}
		}
		return nil
	})
	if err != nil {
		log.Printf("error loading existing dir %s", err)
	}
}

func main() {
	flag.Parse()
	dirWatcher := make(chan notify.EventInfo, 100)
	go loadDir(*configDir, dirWatcher)
	if err := notify.Watch(*configDir, dirWatcher, notify.Create); err != nil {
		log.Printf("error watching config dir %s %s", *configDir, err)
	}
	for {
		e := <-dirWatcher
		fi, err := os.Stat(e.Path())
		if err != nil {
			log.Printf("error receiving event %s", err)
			continue
		}
		if fi.Mode().IsDir() {
			log.Printf("loading subdir %s", e.Path())
			time.Sleep(1000 * time.Millisecond)
			loadDir(e.Path(), dirWatcher)
		} else if strings.HasSuffix(e.Path(), "kubeconfig") {
			log.Printf("new kubeconfig in %s", e.Path())
			go watch(e.Path())
		}
	}
}

func watch(kubeconfig string) error {
	log.Printf("attempting to watch %s", kubeconfig)
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Printf("%s %s", kubeconfig, err)
		return err
	}
	configfile, err := clientcmd.LoadFromFile(kubeconfig)
	context := "nil"
	for key := range configfile.Contexts {
		context = key
		break
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf("%s %s", kubeconfig, err)
		return err
	}
	pods, err := clientset.CoreV1().Pods("").Watch(metav1.ListOptions{})
	if err != nil {
		log.Printf("%s %s", kubeconfig, err)
		return err
	}
	netpol, err := clientset.NetworkingV1().NetworkPolicies("").Watch(metav1.ListOptions{})
	if err != nil {
		log.Printf("%s %s", kubeconfig, err)
		return err
	}
	deploys, err := clientset.AppsV1().Deployments("").Watch(metav1.ListOptions{})
	if err != nil {
		log.Printf("%s %s", kubeconfig, err)
		return err
	}

	podChan := pods.ResultChan()
	netChan := netpol.ResultChan()
	deployChan := deploys.ResultChan()

	for {
		select {
		case e := <-podChan:
			if e.Type == "ERROR" || e.Object == nil {
				log.Printf("%s %s", kubeconfig, e)
				return errors.New("nil from k8s client")
			}
			o := e.Object.(*v1.Pod)
			if o.Namespace == "kube-system" || e.Type == "MODIFIED" {
				continue
			}
			filename := filepath.Join(*cacheDir, fmt.Sprintf(context, "_pod"))
			file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
			if err != nil {
				log.Printf("failed to open file %s %s", filename, err)
				return err
			}
			_, err = file.WriteString(fmt.Sprintf("pod %s for %s in %s (%s)\n", e.Type, o.Name, context, o.Namespace))
			if err != nil {
				log.Printf("failed to write file %s %s", filename, err)
				return err
			}
			err = file.Close()
			if err != nil {
				log.Printf("failed to close file %s %s", filename, err)
				return err
			}
		case e := <-netChan:
			if e.Type == "ERROR" || e.Object == nil {
				log.Printf("%s %s", kubeconfig, e)
				return errors.New("nil from k8s client")
			}
			o := e.Object.(*nv1.NetworkPolicy)
			if o.Namespace == "kube-system" || e.Type != "MODIFIED" {
				continue
			}
			log.Printf("nc %s %s (%s)\n", e.Type, o.Name, o.Namespace)
		case e := <-deployChan:
			if e.Type == "ERROR" || e.Object == nil {
				log.Printf("%s %s", kubeconfig, e)
				return errors.New("nil from k8s client")
			}
			o := e.Object.(*av1.Deployment)
			if o.Namespace == "kube-system" || e.Type != "MODIFIED" {
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
