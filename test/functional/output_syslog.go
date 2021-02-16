package functional

import (
	"strings"

	"github.com/ViaQ/logerr/log"
	logging "github.com/openshift/cluster-logging-operator/pkg/apis/logging/v1"
	"github.com/openshift/cluster-logging-operator/test/helpers"
	"github.com/openshift/cluster-logging-operator/test/runtime"
)

const ImageRemoteSyslog = "quay.io/openshift/origin-logging-rsyslog:latest"

func (f *FluentdFunctionalFramework) addSyslogOutput(b *runtime.PodBuilder, output logging.OutputSpec) error {
	log.V(2).Info("Adding syslog output", "name", output.Name)
	name := strings.ToLower(output.Name)
	// using unsecure rsyslog conf
	rsyslogConf := helpers.GenerateRsyslogConf(helpers.TcpSyslogInput, helpers.RFC5424)
	config := runtime.NewConfigMap(b.Pod.Namespace, name, map[string]string{
		"rsyslog.conf": rsyslogConf,
	})
	log.V(2).Info("Creating configmap", "namespace", config.Namespace, "name", config.Name, "rsyslog.conf", rsyslogConf)
	if err := f.test.Client.Create(config); err != nil {
		return err
	}

	log.V(2).Info("Adding container", "name", name)
	b.AddContainer(name, ImageRemoteSyslog).
		AddVolumeMount(config.Name, "/rsyslog/etc", "", false).
		WithCmdArgs([]string{"rsyslogd", "-n", "-f", "/rsyslog/etc/rsyslog.conf"}).
		WithPrivilege().
		End().
		AddConfigMapVolume(config.Name, config.Name)
	return nil
}
