package functional

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/ViaQ/logerr/log"
	"github.com/openshift/cluster-logging-operator/internal/pkg/generator/forwarder"
	logging "github.com/openshift/cluster-logging-operator/pkg/apis/logging/v1"
	"github.com/openshift/cluster-logging-operator/pkg/certificates"
	"github.com/openshift/cluster-logging-operator/pkg/constants"
	"github.com/openshift/cluster-logging-operator/pkg/utils"
	"github.com/openshift/cluster-logging-operator/test/client"
	"github.com/openshift/cluster-logging-operator/test/helpers/oc"
	"github.com/openshift/cluster-logging-operator/test/runtime"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	maxDuration          time.Duration
	defaultRetryInterval time.Duration
)

//FluentdFunctionalFramework deploys stand alone fluentd with the fluent.conf as generated by input ClusterLogForwarder CR
type FluentdFunctionalFramework struct {
	Name              string
	Namespace         string
	Conf              string
	image             string
	labels            map[string]string
	Forwarder         *logging.ClusterLogForwarder
	test              *client.Test
	pod               *corev1.Pod
	fluentContainerId string
}

func init() {
	maxDuration, _ = time.ParseDuration("2m")
	defaultRetryInterval, _ = time.ParseDuration("1ms")
}

func NewFluentdFunctionalFramework() *FluentdFunctionalFramework {
	verbosity := 9
	if level, found := os.LookupEnv("LOG_LEVEL"); found {
		if i, err := strconv.Atoi(level); err == nil {
			verbosity = i
		}
	}

	log.MustInit("fluent-ftf")
	log.SetLogLevel(verbosity)
	t := client.NewTest()
	testName := fmt.Sprintf("test-fluent-%d", rand.Intn(1000))
	framework := &FluentdFunctionalFramework{
		Name:      testName,
		Namespace: t.NS.Name,
		image:     utils.GetComponentImage(constants.FluentdName),
		labels: map[string]string{
			"testtype": "functional",
			"testname": testName,
		},
		test:      t,
		Forwarder: runtime.NewClusterLogForwarder(),
	}
	return framework
}

func (f *FluentdFunctionalFramework) Cleanup() {
	f.test.Close()
}

func (f *FluentdFunctionalFramework) RunCommand(container string, cmd ...string) (string, error) {
	log.V(2).Info("Running", "container", container, "cmd", cmd)
	out, err := runtime.ExecOc(f.pod, container, cmd[0], cmd[1:]...)
	log.V(2).Info("Exec'd", "out", out, "err", err)
	return out, err
}

//Deploy the objects needed to functional test
func (f *FluentdFunctionalFramework) Deploy() (err error) {
	log.V(2).Info("Generating config", "forwarder", f.Forwarder)
	clfYaml, _ := yaml.Marshal(f.Forwarder)
	if f.Conf, err = forwarder.Generate(string(clfYaml), false); err != nil {
		return err
	}
	log.V(2).Info("Generating Certificates")
	if err, _ = certificates.GenerateCertificates(f.test.NS.Name,
		utils.GetScriptsDir(), "elasticsearch",
		utils.DefaultWorkingDir); err != nil {
		return err
	}
	log.V(2).Info("Creating config configmap")
	configmap := runtime.NewConfigMap(f.test.NS.Name, f.Name, map[string]string{})
	runtime.NewConfigMapBuilder(configmap).
		Add("fluent.conf", f.Conf).
		Add("run.sh", string(utils.GetFileContents(utils.GetShareDir()+"/fluentd/run.sh")))
	if err = f.test.Client.Create(configmap); err != nil {
		return err
	}

	log.V(2).Info("Creating certs configmap")
	certsName := "certs-" + f.Name
	certs := runtime.NewConfigMap(f.test.NS.Name, certsName, map[string]string{})
	runtime.NewConfigMapBuilder(certs).
		Add("tls.key", string(utils.GetWorkingDirFileContents("system.logging.fluentd.key"))).
		Add("tls.crt", string(utils.GetWorkingDirFileContents("system.logging.fluentd.crt")))
	if err = f.test.Client.Create(certs); err != nil {
		return err
	}

	log.V(2).Info("Creating service")
	service := runtime.NewService(f.test.NS.Name, f.Name)
	runtime.NewServiceBuilder(service).
		AddServicePort(24231, 24231).
		WithSelector(f.labels)
	if err = f.test.Client.Create(service); err != nil {
		return err
	}

	log.V(2).Info("Defining pod...")
	var containers []corev1.Container
	f.pod = runtime.NewPod(f.test.NS.Name, f.Name, containers...)
	b := runtime.NewPodBuilder(f.pod).
		WithLabels(f.labels).
		AddConfigMapVolume("config", f.Name).
		AddConfigMapVolume("entrypoint", f.Name).
		AddConfigMapVolume("certs", certsName).
		AddContainer(constants.FluentdName, f.image).
		AddEnvVar("LOG_LEVEL", "debug").
		AddEnvVarFromFieldRef("POD_IP", "status.podIP").
		AddVolumeMount("config", "/etc/fluent/configs.d/user", "", true).
		AddVolumeMount("entrypoint", "/opt/app-root/src/run.sh", "run.sh", true).
		AddVolumeMount("certs", "/etc/fluent/metrics", "", true).
		End()
	if err = f.addOutputContainers(b, f.Forwarder.Spec.Outputs); err != nil {
		return err
	}
	log.V(2).Info("Creating pod", "pod", f.pod)
	if err = f.test.Client.Create(f.pod); err != nil {
		return err
	}

	log.V(2).Info("waiting for pod to be ready")
	if err = oc.Literal().From(fmt.Sprintf("oc wait -n %s pod/%s --timeout=120s --for=condition=Ready", f.test.NS.Name, f.Name)).Output(); err != nil {
		return err
	}
	if err = f.test.Client.Get(f.pod); err != nil {
		return err
	}
	log.V(2).Info("waiting for service endpoints to be ready")
	err = wait.PollImmediate(time.Second*2, time.Second*10, func() (bool, error) {
		ips, err := oc.Get().WithNamespace(f.test.NS.Name).Resource("endpoints", f.Name).OutputJsonpath("{.subsets[*].addresses[*].ip}").Run()
		if err != nil {
			return false, nil
		}
		// if there are IPs in the service endpoint, the service is available
		if strings.TrimSpace(ips) != "" {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("service could not be started")
	}
	log.V(2).Info("waiting for fluentd to be ready")
	err = wait.PollImmediate(time.Second*2, time.Second*30, func() (bool, error) {
		output, err := oc.Literal().From(fmt.Sprintf("oc logs -n %s pod/%s %s", f.test.NS.Name, f.Name, constants.FluentdName )).Run()
		if err != nil {
			return false, nil
		}

		// if fluentd started successfully return success
		if strings.Contains(output, "flush_thread actually running") {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("fluentd did not start in the container")
	}
	for _, cs := range f.pod.Status.ContainerStatuses {
		if cs.Name == constants.FluentdName {
			f.fluentContainerId = strings.TrimPrefix(cs.ContainerID, "cri-o://")
			break
		}
	}
	return nil
}

func (f *FluentdFunctionalFramework) addOutputContainers(b *runtime.PodBuilder, outputs []logging.OutputSpec) error {
	log.V(2).Info("Adding outputs", "outputs", outputs)
	for _, output := range outputs {
		if output.Type == logging.OutputTypeFluentdForward {
			if err := f.addForwardOutput(b, output); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *FluentdFunctionalFramework) WritesApplicationLogs(numOfLogs int) error {
	msg := "2020-11-04T18:13:59.061892999+00:00 stdout F Functional test message $n"
	return f.WritesMessageToApplicationLogs(msg, numOfLogs)
}

func (f *FluentdFunctionalFramework) WritesMessageToApplicationLogs(msg string, numOfLogs int) error {
	filepath := fmt.Sprintf("/var/log/containers/%s_%s_%s-%s.log", f.pod.Name, f.pod.Namespace, constants.FluentdName, f.fluentContainerId)
	result, err := f.RunCommand(constants.FluentdName, "bash", "-c", fmt.Sprintf("bash -c 'mkdir -p /var/log/containers;for n in {1..%d};do echo %s > %s; done'", numOfLogs, msg, filepath))
	log.V(3).Info("FluentdFunctionalFramework.WritesApplicationLogs", "result", result, "err", err)
	return err
}

func (f *FluentdFunctionalFramework) ReadApplicationLogsFrom(outputName string) (result string, err error) {
	file := "/tmp/app-logs"
	err = wait.PollImmediate(defaultRetryInterval, maxDuration, func() (done bool, err error) {
		result, err = f.RunCommand(outputName, "cat", file)
		if err == nil {
			return true, nil
		}
		log.V(4).Error(err, "Polling application logs")
		return false, nil
	})
	if err == nil {
		result = fmt.Sprintf("[%s]", strings.Join(strings.Split(strings.TrimSpace(result), "\n"), ","))
	}
	log.V(4).Info("Returning", "logs", result)
	return result, err
}
