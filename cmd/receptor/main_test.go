package main_test

import (
	"fmt"
	"net/http"

	"github.com/cloudfoundry-incubator/receptor"
	"github.com/cloudfoundry-incubator/receptor/cmd/receptor/testrunner"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter/storerunner/etcdstorerunner"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
)

const username = "username"
const password = "password"

var receptorBinPath string
var receptorAddress string
var etcdPort int

var _ = SynchronizedBeforeSuite(
	func() []byte {
		receptorConfig, err := gexec.Build("github.com/cloudfoundry-incubator/receptor/cmd/receptor", "-race")
		Ω(err).ShouldNot(HaveOccurred())
		return []byte(receptorConfig)
	},
	func(receptorConfig []byte) {
		receptorBinPath = string(receptorConfig)
		receptorAddress = fmt.Sprintf("127.0.0.1:%d", 6700+GinkgoParallelNode())
		etcdPort = 4001 + GinkgoParallelNode()
	},
)

var _ = SynchronizedAfterSuite(func() {
}, func() {
	gexec.CleanupBuildArtifacts()
})

var _ = Describe("Receptor API", func() {
	var etcdUrl string
	var etcdRunner *etcdstorerunner.ETCDClusterRunner
	var bbs *Bbs.BBS
	var receptorRunner *ginkgomon.Runner
	var receptorProcess ifrit.Process
	var client receptor.Client

	BeforeEach(func() {
		etcdUrl = fmt.Sprintf("http://127.0.0.1:%d", etcdPort)
		etcdRunner = etcdstorerunner.NewETCDClusterRunner(etcdPort, 1)
		etcdRunner.Start()

		logger := lager.NewLogger("bbs")
		logger.RegisterSink(lager.NewWriterSink(GinkgoWriter, lager.DEBUG))

		bbs = Bbs.NewBBS(etcdRunner.Adapter(), timeprovider.NewTimeProvider(), logger)

		client = receptor.NewClient(receptorAddress, username, password)

		receptorRunner = testrunner.New(receptorBinPath, receptorAddress, etcdUrl, username, password)
		receptorProcess = ginkgomon.Invoke(receptorRunner)
	})

	AfterEach(func() {
		defer etcdRunner.Stop()
		ginkgomon.Kill(receptorProcess)
	})

	Describe("Basic Auth", func() {
		var res *http.Response
		var httpClient *http.Client

		BeforeEach(func() {
			httpClient = new(http.Client)
		})

		Context("when the username and password are blank", func() {
			BeforeEach(func() {
				var err error
				ginkgomon.Kill(receptorProcess)
				receptorRunner = testrunner.New(receptorBinPath, receptorAddress, etcdUrl, "", "")
				receptorProcess = ginkgomon.Invoke(receptorRunner)

				res, err = httpClient.Get("http://" + receptorAddress)
				Ω(err).ShouldNot(HaveOccurred())
				res.Body.Close()
			})

			It("does not return 401", func() {
				Ω(res.StatusCode).Should(Equal(http.StatusNotFound))
			})
		})

		Context("when the username and password are required but not sent", func() {
			BeforeEach(func() {
				var err error
				res, err = httpClient.Get("http://" + receptorAddress)
				Ω(err).ShouldNot(HaveOccurred())
				res.Body.Close()
			})

			It("returns 401 for all requests", func() {
				Ω(res.StatusCode).Should(Equal(http.StatusUnauthorized))
			})
		})
	})

	Describe("POST /task", func() {
		var taskToCreate receptor.CreateTaskRequest
		var err error

		BeforeEach(func() {
			taskToCreate = receptor.CreateTaskRequest{
				TaskGuid: "task-guid-1",
				Domain:   "test-domain",
				Stack:    "some-stack",
				Actions: []models.ExecutorAction{
					{Action: models.RunAction{Path: "/bin/bash", Args: []string{"echo", "hi"}}},
				},
			}

			err = client.CreateTask(taskToCreate)
		})

		It("responds without an error", func() {
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("desires the task in the BBS", func() {
			Eventually(bbs.GetAllPendingTasks).Should(HaveLen(1))
		})

		Context("when trying to create a task with a GUID that already exists", func() {
			BeforeEach(func() {
				err = client.CreateTask(taskToCreate)
			})

			It("returns an error indicating that the key already exists", func() {
				Ω(err.(receptor.Error).Type).Should(Equal(receptor.TaskGuidAlreadyExists))
			})
		})
	})
})
