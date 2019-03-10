package k8scachedaemon

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/haxii/daemon"
	av1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	nv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeconfig = flag.String("kubeconfig", filepath.Join(os.Getenv("HOME"), ".kube", "config"), "absolute path to the kubeconfig file")
	_          = flag.String("s", daemon.UsageDefaultName, daemon.UsageMessage)
)

func run() {
	flag.Parse()

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatal(err)
		panic(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
		panic(err.Error())
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

func main() {
	daemon.Make("-s", "k8s-cache-daemon", "k8s cache daemon").Run(run)
}
