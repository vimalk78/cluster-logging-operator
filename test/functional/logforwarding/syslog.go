package logforwarding

import (
	"encoding/json"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	logging "github.com/openshift/cluster-logging-operator/pkg/apis/logging/v1"
	"github.com/openshift/cluster-logging-operator/test/functional"
	//. "github.com/openshift/cluster-logging-operator/test/matchers"
)

var (
	JSONApplicationLogs = []string{
		`{"appname_key":"rec_appname","msgcontent":"My life is my message","msgid_key":"rec_msgid","procid_key":"rec_procid","timestamp":"2021-02-16 18:54:52"}`,
		/**/
		`{"appname_key":"rec_appname","msgcontent":"My life is my message","msgid_key":"rec_msgid","procid_key":"rec_procid","timestamp":"2021-02-16 18:54:53"}`,
		`{"appname_key":"rec_appname","msgcontent":"My life is my message","msgid_key":"rec_msgid","procid_key":"rec_procid","timestamp":"2021-02-16 18:54:54"}`,
		`{"appname_key":"rec_appname","msgcontent":"My life is my message","msgid_key":"rec_msgid","procid_key":"rec_procid","timestamp":"2021-02-16 18:54:55"}`,
		`{"appname_key":"rec_appname","msgcontent":"My life is my message","msgid_key":"rec_msgid","procid_key":"rec_procid","timestamp":"2021-02-16 18:54:56"}`,
		`{"appname_key":"rec_appname","msgcontent":"My life is my message","msgid_key":"rec_msgid","procid_key":"rec_procid","timestamp":"2021-02-16 18:54:57"}`,
		`{"appname_key":"rec_appname","msgcontent":"My life is my message","msgid_key":"rec_msgid","procid_key":"rec_procid","timestamp":"2021-02-16 18:54:58"}`,
		`{"appname_key":"rec_appname","msgcontent":"My life is my message","msgid_key":"rec_msgid","procid_key":"rec_procid","timestamp":"2021-02-16 18:54:59"}`,
		`{"appname_key":"rec_appname","msgcontent":"My life is my message","msgid_key":"rec_msgid","procid_key":"rec_procid","timestamp":"2021-02-16 18:55:00"}`,
		`{"appname_key":"rec_appname","msgcontent":"My life is my message","msgid_key":"rec_msgid","procid_key":"rec_procid","timestamp":"2021-02-16 18:55:01"}`,
	}

	NonJsonAppLogs = []string{
		`2021-02-17 17:46:27 "hello world"`,
		`2021-02-17 17:46:28 "hello world"`,
		`2021-02-17 17:46:29 "hello world"`,
		`2021-02-17 17:46:30 "hello world"`,
		`2021-02-17 17:46:31 "hello world"`,
		`2021-02-17 17:46:32 "hello world"`,
		`2021-02-17 17:46:33 "hello world"`,
		`2021-02-17 17:46:34 "hello world"`,
		`2021-02-17 17:46:35 "hello world"`,
		`2021-02-17 17:46:36 "hello world"`,
	}

	K8sAuditLogs = []string{}

	OpenshiftAuditLogs = []string{}
)

var _ = Describe("[LogForwarding][Syslog] Functional tests", func() {

	var (
		framework *functional.FluentdFunctionalFramework
	)

	BeforeEach(func() {
		framework = functional.NewFluentdFunctionalFramework()
	})
	AfterEach(func() {
		framework.Cleanup()
	})

	setDefaultValues := func(spec *logging.OutputSpec) {
		spec.Syslog = &logging.Syslog{
			Facility: "user",
			Severity: "debug",
			AppName:  "myapp",
			ProcID:   "myproc",
			MsgID:    "mymsg",
			RFC:      "RFC5424",
		}
	}

	join := func(
		f1 func(spec *logging.OutputSpec),
		f2 func(spec *logging.OutputSpec)) func(*logging.OutputSpec) {
		return func(s *logging.OutputSpec) {
			f1(s)
			f2(s)
		}
	}

	getAppName := func(fields []string) string {
		return fields[3]
	}
	getProcID := func(fields []string) string {
		return fields[4]
	}
	getMsgID := func(fields []string) string {
		return fields[5]
	}

	It("should send NonJson App logs to syslog", func() {
		functional.NewClusterLogForwarderBuilder(framework.Forwarder).
			FromInput(logging.InputNameApplication).
			ToSyslogOutputWithVisitor(setDefaultValues)
		Expect(framework.Deploy()).To(BeNil())

		// Log message data
		//Expect(framework.WriteMessagesToApplicationLog("hello world", 10)).To(BeNil())
		for _, log := range NonJsonAppLogs {
			log = strings.ReplaceAll(log, "\"", "\\\"")
			Expect(framework.WriteMessagesToApplicationLog(log, 1)).To(BeNil())
		}
		// Read line from Syslog output
		outputlogs, err := framework.ReadApplicationLogsFrom(logging.OutputTypeSyslog)
		fields := strings.Split(outputlogs[0], " ")
		Expect(outputlogs).ToNot(BeEmpty())
		Expect(getAppName(fields)).To(Equal("myapp"))
		Expect(getProcID(fields)).To(Equal("myproc"))
		Expect(getMsgID(fields)).To(Equal("mymsg"))
		Expect(err).To(BeNil(), "Expected no errors reading the logs")
	})
	It("should take values of appname, procid, messageid from record", func() {
		functional.NewClusterLogForwarderBuilder(framework.Forwarder).
			FromInput(logging.InputNameApplication).
			ToSyslogOutputWithVisitor(join(setDefaultValues, func(spec *logging.OutputSpec) {
				spec.Syslog.AppName = "$.message.appname_key"
				spec.Syslog.ProcID = "$.message.procid_key"
				spec.Syslog.MsgID = "$.message.msgid_key"
			}))
		Expect(framework.Deploy()).To(BeNil())

		// Log message data
		for _, log := range JSONApplicationLogs {
			log = strings.ReplaceAll(log, "\"", "\\\"")
			Expect(framework.WriteMessagesToApplicationLog(log, 1)).To(BeNil())
		}
		// Read line from Syslog output
		outputlogs, err := framework.ReadApplicationLogsFrom(logging.OutputTypeSyslog)
		Expect(err).To(BeNil(), "Expected no errors reading the logs")
		fields := strings.Split(outputlogs[0], " ")
		Expect(getAppName(fields)).To(Equal("rec_appname"))
		Expect(getProcID(fields)).To(Equal("rec_procid"))
		Expect(getMsgID(fields)).To(Equal("rec_msgid"))
	})
	It("should take values from fluent tag", func() {
		functional.NewClusterLogForwarderBuilder(framework.Forwarder).
			FromInput(logging.InputNameApplication).
			ToSyslogOutputWithVisitor(join(setDefaultValues, func(spec *logging.OutputSpec) {
				spec.Syslog.AppName = "tag"
			}))
		Expect(framework.Deploy()).To(BeNil())

		// Log message data
		for _, log := range JSONApplicationLogs {
			log = strings.ReplaceAll(log, "\"", "\\\"")
			Expect(framework.WriteMessagesToApplicationLog(log, 1)).To(BeNil())
		}
		// Read line from Syslog output
		outputlogs, err := framework.ReadApplicationLogsFrom(logging.OutputTypeSyslog)
		Expect(err).To(BeNil(), "Expected no errors reading the logs")
		fields := strings.Split(outputlogs[0], " ")
		Expect(getAppName(fields)).To(HavePrefix("kubernetes."))
	})
})

func Logs(n int) []string {
	logs := []string{}
	for i := 0; i < n; i++ {
		log := map[string]string{
			"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
			"appname_key": "rec_appname",
			"procid_key":  "rec_procid",
			"msgid_key":   "rec_msgid",
			"msgcontent":  "My life is my message",
		}
		b, _ := json.Marshal(&log)
		logs = append(logs, string(b))
	}
	return logs
}
