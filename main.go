package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"rpalmer/exercise-1-processing-an-ai-workload/types"
)

// --- Purpose ---
// This script is run during edge deployments to validate that a stage is healthy after it is deployed to.
// The script will have exit code 0 if an AdHoc job is successfully created and completed through both Edge and Core and a Scheduled job can be created through core
// Otherwise it will have exit code 1
// --- Usage ---
// To run this script locally/manually you will need to...
//  1) choose the controller URL to run against (local, dev or stage)
//  2) Uncomment fetch token from the scripts main body (at the bottom) and input valid credentials to fetch_token for the stage you chose
//       OR set the environment variable API_VALIDATION_TOKEN to a valid core api token you have already fetched
//  3) Set EDGE_VALIDATION_TOKEN to be either a token from the `edge.token` table with proper permissions (if running against your local controller)
//       OR a token from the dev/stage table. (Jenkins credential edge-validation-token holds a valid token if you have access)
//  4) In validate_env() set VALIDATE_CLUSTER_ID to your personal test cluster if needed.
//
// Example use for running against local controller:
// ./validate-deployment.sh dev "https://controller-v3f.aws-dev-rt.veritone.com" "https://api.dev.us-1.veritone.com/v3/graphql"

// --- ARGS ---
var env, controller_url, base_api_url string

// env: 			Example for local use: http://localhost:9000
// controller_url: 	Example value at jenkins runtime (env=dev): https://controller-v3f.aws-dev-rt.veritone.com
// base_api_url: 	Example for local use and jenkins runtime (env=dev): https://api.dev.us-1.veritone.com/v3/graphql

//
// --- GLOBALS ---
//
var QUERY_JOB_TIMEOUT time.Duration
var VALIDATE_CLUSTER_ID string
var BASE_API_URL string
var TOKEN string
var ADHOC_CORE_JOB_ID string
var ADHOC_CORE_VALIDATED bool
var SCHEDULED_JOB_NAME string
var SCHEDULED_CORE_JOB_ID string
var SCHEDULED_JOB_TEMPLATE_ID string

var ADHOC_CORE_TARGET_ID string

var DATA_REGISTRY_ID string
var SCHEMA_DRAFT_ID string
var PUBLISHED_SCHEMA_ID string
var ROOT_FOLDER_ID string
var ROOT_FOLDER_TYPE_ID int
var TDO_FOLDER_ID string

var wg sync.WaitGroup
var adhocCoreJobWg sync.WaitGroup
var scheduledJobWg sync.WaitGroup

//
// --- GENERAL FUNCTIONS ---
//

func fatal(err error) {
	fmt.Println(time.Now().Format(time.StampMicro), "[ERROR]", err.Error())
	//deleteScheduledCoreJob()
	os.Exit(1)
}

func info(msg string) {
	fmt.Println(time.Now().Format(time.StampMicro), "[INFO]", msg)
}

func cleanupOnInterrupt() {
	// deleteScheduledCoreJob()
	os.Exit(int(syscall.SIGINT))
}

func fetch(resource string, requestBody map[string]string) (responseBody string) {
	client := &http.Client{}
	postBody, _ := json.Marshal(requestBody)
	//info(fmt.Sprint("[DEBUG]", "Post Body:", string(postBody)))
	req, err := http.NewRequest(http.MethodPost, resource, bytes.NewBuffer(postBody))
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", TOKEN))
	req.Header.Add("Content-Type", "application/json")
	if err == nil {
		response, error := client.Do(req)

		if error != nil {
			fatal(errors.New(fmt.Sprint("when posting to ", resource, "err:", error)))
		}

		defer response.Body.Close()

		body, error := ioutil.ReadAll(response.Body)
		if error != nil {
			fatal(errors.New(fmt.Sprint("reading response body. msg:", error)))
		}

		responseBody = string(body)
	}

	return
}

func handleErrResponseCore(errResponse string, errHandler func(err error)) {
	var apiErrorResponseJson types.APIErrorResponse
	err := json.Unmarshal([]byte(errResponse), &apiErrorResponseJson)
	if err == nil {
		var errsMsg string
		for _, e := range apiErrorResponseJson.Errors {
			errsMsg += fmt.Sprintf("\n%s", e.Message)
			for _, l := range e.Locations {
				errsMsg += fmt.Sprintf("\tL%d:%d\n\t\t\t\t\t", l.Line, l.Column)
			}
		}
		//info(fmt.Sprint("[DEBUG] post body:", string(postBody)))
		errHandler(errors.New(fmt.Sprint("returned from api.", errsMsg)))
	} else {
		info(fmt.Sprint("[DEBUG] errResponse:", errResponse))
		panic(err)
	}
}

func handleErrResponse(errResponse string) {
	handleErrResponseCore(errResponse, fatal)
}

func validateEnv() {
	// init to cmd argument with possible override by env in the future?
	BASE_API_URL = base_api_url

	fmt.Sprintln(env, "----", controller_url, "----", base_api_url)

	if env == "" {
		fatal(
			errors.New(
				fmt.Sprint("Usage:", os.Args[0], "[dev|stage] -u=<username> -p=<password> <controller-url> <base-api-url>"),
			),
		)
	}

	if env != "dev" && env != "stage" {
		fatal(
			errors.New("validate-deployment should not be run on stages other than 'dev' or 'stage"),
		)
	}

	QUERY_JOB_TIMEOUT = time.Minute * 30

	if env == "dev" {
		VALIDATE_CLUSTER_ID = "rt-9d7a5d1b-ffe0-4d71-a982-190522cdf273"
		BASE_API_URL = "https://api.dev.us-1.veritone.com/v3/graphql"
	}

	if env == "stage" {
		VALIDATE_CLUSTER_ID = "rt-9d7a5d1b-ffe0-4d71-a982-190522cdf272"
		BASE_API_URL = "https://api.stage.us-1.veritone.com/v3/graphql"
	}

	// Uncomment for testing against personal cluster
	// VALIDATE_CLUSTER_ID=<your cluster id>
	// QUERY_JOB_TIMEOUT=<your timeout>

}

// For Local/Manual Testing - in the pipeline these values will be set in /vars/validateDeployment.groovy
func setToken(username string, password string) {
	if username == "" || password == "" {
		fatal(errors.New("no user credentials provided"))
	}
	response := fetch(BASE_API_URL, map[string]string{
		"query": fmt.Sprintf(`
		mutation {
			userLogin(
				input: { 
					userName: "%s", 
					password: "%s" 
				}
			) 
			{
				token
				organization {
					id
					name
				}
			}
		}`,
			username,
			password,
		),
	},
	)

	var userLoginResponse types.UserLoginResponse
	err := json.Unmarshal([]byte(response), &userLoginResponse)

	if err == nil && userLoginResponse.Data.UserLogin.Token != "" {
		TOKEN = userLoginResponse.Data.UserLogin.Token
	} else {
		fmt.Println()
		handleErrResponse(response)
	}

	// If you have already fetched tokens you would like to use instead, set them here
	// EDGE_VALIDATION_TOKEN=<your token here>
	// API_VALIDATION_TOKEN=<your token here>
	// fmt.Println("noop")
}

//
// --- ADHOC CORE JOB FUNCTIONS ---
//
func createAdhocCoreJob() {
	// Set start time to two minutes in the past and stop time to 15 minutes in the future
	// startTime := time.Now().Add(time.Minute * -2).Unix()
	// stopTime := time.Now().Add(time.Minute * 15).Unix()
	// TODO: graphql call to create adhoc job
	adhocCoreJobResponse := fetch(BASE_API_URL, map[string]string{
		"query": `
		mutation create_translate_job($clusterId: ID!) {
			createJob(
				input: {
					target: { status: "downloaded" }
					clusterId: $clusterId
					## Tasks
					tasks: [
						{
							# Chunk engine
							engineId: "8bdb0e3b-ff28-4f6e-a3ba-887bd06e6440"
							payload: {
								url: "https://aaudi-test-media.s3.us-east-2.amazonaws.com/Transcribe_Test_02.m4a"
								ffmpegTemplate: "rawchunk"
							}
							executionPreferences: {
								priority: -92
								parentCompleteBeforeStarting: true
							}
							ioFolders: [{ referenceId: "si-out", mode: chunk, type: output }]
						}
						{
							# Transcription - E - English V3
							engineId: "a8c4a9f4-d6c4-4524-ab98-c0de560abf9b"
							payload: { 
					  keywords: "",
					  diarization: "",
					  advancedPunctuation: "true",
					  speakerChangeSensitivity: "0.4"
				  }
							executionPreferences: {
								priority: -92
								parentCompleteBeforeStarting: true
							}
							ioFolders: [
								{ referenceId: "engine-in", mode: chunk, type: input }
								{ referenceId: "engine-out", mode: chunk, type: output }
							]
						}
						{
							# output writer
							engineId: "8eccf9cc-6b6d-4d7d-8cb3-7ebf4950c5f3"
							executionPreferences: {
								priority: -92
								parentCompleteBeforeStarting: true
							}
							ioFolders: [{ referenceId: "ow-in", mode: chunk, type: input }]
						}
					]
					##Routes
					routes: [
						{
							## chunkAudio --> translation
							parentIoFolderReferenceId: "si-out"
							childIoFolderReferenceId: "engine-in"
							options: {}
						}
						{
							## sampleChunkOutputFolderA  --> ow1
							parentIoFolderReferenceId: "engine-out"
							childIoFolderReferenceId: "ow-in"
							options: {}
						}
					]
				}
			) {
				id
				targetId
				createdDateTime
			}
		}		
		`,
		"variables": fmt.Sprintf(`{
			"clusterId": "%s"
		}`, VALIDATE_CLUSTER_ID),
	})

	// info(fmt.Sprint("[DEBUG] Core job response:", adhocCoreJobResponse))

	var adhocCoreJobResponseJson types.APIAdhocCoreJobResponse
	err := json.Unmarshal([]byte(adhocCoreJobResponse), &adhocCoreJobResponseJson)
	if err == nil && adhocCoreJobResponseJson.Data.CreateJob.Id != "" {
		ADHOC_CORE_JOB_ID = adhocCoreJobResponseJson.Data.CreateJob.Id
		ADHOC_CORE_TARGET_ID = adhocCoreJobResponseJson.Data.CreateJob.TargetId
		info(fmt.Sprint("AdHoc core job id:", ADHOC_CORE_JOB_ID))
	} else {
		handleErrResponse(adhocCoreJobResponse)
	}
}

func queryAdhocCoreJob() {
	coreTimeout := time.Now().Add(QUERY_JOB_TIMEOUT)
	pollInterval := time.Minute
	//adhocCoreValidated := false

	ticker := time.NewTicker(pollInterval)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				{
					queryAdhocCoreJobResponse := fetch(BASE_API_URL, map[string]string{
						"query": fmt.Sprintf(`
						{
							job(id: "%s") {
								id
								targetId
								clusterId
								status
								createdDateTime
									tasks {
										records {
											id
											engine {
												name
											}
											status
										}
									}
							}
						}`, ADHOC_CORE_JOB_ID),
					})

					// info(fmt.Sprint("[DEBUG] Query Core job response:", queryAdhocCoreJobResponse))

					var queryAdhocCoreJobResponseJson types.QueryAdhocCoreJobResponse
					err := json.Unmarshal([]byte(queryAdhocCoreJobResponse), &queryAdhocCoreJobResponseJson)
					if err == nil && queryAdhocCoreJobResponseJson.Data.Job.Status != "" {
						adhocCoreJobStatus := queryAdhocCoreJobResponseJson.Data.Job.Status
						info(fmt.Sprint("Core job status:", adhocCoreJobStatus))

						if adhocCoreJobStatus == "complete" {
							ADHOC_CORE_VALIDATED = true
						}

						if adhocCoreJobStatus == "failed" || adhocCoreJobStatus == "aborted" {
							fatal(fmt.Errorf("AdHoc core job failed ID=%s Status=%s", ADHOC_CORE_JOB_ID, adhocCoreJobStatus))
						}

						if ADHOC_CORE_VALIDATED {
							info(fmt.Sprintf("AdHoc core job succeeded ID=%s Status=%s", ADHOC_CORE_JOB_ID, adhocCoreJobStatus))
							close(quit)
						}
					} else {
						handleErrResponse(queryAdhocCoreJobResponse)
					}

					if time.Now().After(coreTimeout) {
						fatal(
							errors.New(
								fmt.Sprint("Core Jobs still pending after", coreTimeout, ", failing environment validation for Environment=", env),
							),
						)
						close(quit)
					}
				}

			case <-quit:
				{
					ticker.Stop()
					adhocCoreJobWg.Done()
					return
				}
			}
		}
	}()
}

//
// --- ADHOC EDGE JOB FUNCTIONS ---
//
// func createAdhocEdgeJob() {

// }

// func queryAdhocEdgeJob() {

// }

// func validateEdgeJobOutput() {

// }

//
// --- SCHEDULED CORE/EDGE JOB FUNCTIONS ---
//
func createScheduledCoreJob() {
	// Set start time to two minutes in the past and stop time to 15 minutes in the future
	startTime := time.Now().Add(time.Minute * -2).Unix()
	stopTime := time.Now().Add(time.Hour).Unix()
	SCHEDULED_JOB_NAME = fmt.Sprintf("scheduled_job_test_%s", fmt.Sprint(time.Now().Unix()))
	// TODO: graphql call to create adhoc job
	scheduledCoreJobResponse := fetch(BASE_API_URL, map[string]string{
		"query": `
		mutation createScheduledJob(
			$name: String!,
			$startDateTime: DateTime,
			$stopDateTime: DateTime,
			$jobTemplateId: ID!
			) {
				createScheduledJob(
					input: {
						organizationId: 7682
						name: $name
						runMode: Recurring,
						startDateTime: $startDateTime
						stopDateTime: $stopDateTime
						recurringScheduleParts: [{repeatInterval: 5, repeatIntervalUnit: Minutes}]
						jobTemplateIds: [$jobTemplateId]
					}
				)
				{
					id
				}
			}
		`,
		"variables": fmt.Sprintf(`{
			"name": "%s",
			"startDateTime": "%s",
			"stopDateTime": "%s",
			"jobTemplateId": "%s"
		}`, SCHEDULED_JOB_NAME, strconv.FormatInt(startTime, 10), strconv.FormatInt(stopTime, 10), SCHEDULED_JOB_TEMPLATE_ID),
	})

	// info(fmt.Sprint("[DEBUG] Scheduled Core job response:", scheduledCoreJobResponse))

	var scheduledCoreJobResponseJson types.APIScheduledCoreJobResponse
	err := json.Unmarshal([]byte(scheduledCoreJobResponse), &scheduledCoreJobResponseJson)
	if err == nil && scheduledCoreJobResponseJson.Data.CreateScheduledJob.Id != "" {
		SCHEDULED_CORE_JOB_ID = scheduledCoreJobResponseJson.Data.CreateScheduledJob.Id
		info(fmt.Sprint("Scheduled core job id:", ADHOC_CORE_JOB_ID))
	} else {
		handleErrResponse(scheduledCoreJobResponse)
	}
}

func queryEdgeScheduledJob() {
	// This does not use QUERY_JOB_TIMEOUT because jobs are pulled every 5 minutes
	scheduleTimeout := time.Now().Add(time.Minute * 15)
	pollInterval := time.Minute

	ticker := time.NewTicker(pollInterval)
	quit := make(chan struct{})
	go func() {
		var retrievedScheduledJobId string
		for {
			select {
			case <-ticker.C:
				{
					queryEdgeScheduledJobResponse, err := http.Get(fmt.Sprintf("%s/edge/v1/proc/scheduled_jobs", controller_url))

					if err == nil {

						defer queryEdgeScheduledJobResponse.Body.Close()

						body, error := ioutil.ReadAll(queryEdgeScheduledJobResponse.Body)
						if error != nil {
							fatal(errors.New(fmt.Sprint("reading response body. msg:", error)))
						}

						bodyStr := string(body)

						// info(fmt.Sprint("[DEBUG] Query Edge Scheduled job response:", bodyStr))

						var queryEdgeScheduledJobResponseJson types.QueryEdgeScheduledJobResponse
						err := json.Unmarshal([]byte(bodyStr), &queryEdgeScheduledJobResponseJson)
						if err == nil && len(queryEdgeScheduledJobResponseJson.Result) > 0 {

							for _, retrievedScheduledJob := range queryEdgeScheduledJobResponseJson.Result {
								if retrievedScheduledJob.ScheduledJobName == SCHEDULED_JOB_NAME {
									retrievedScheduledJobId = retrievedScheduledJob.ScheduledJobId
									break
								}
							}
							info(fmt.Sprint("Scheduled job query retrieved id:", retrievedScheduledJobId))

							if retrievedScheduledJobId == SCHEDULED_CORE_JOB_ID {
								info(fmt.Sprintf("Found shceduled job ID (%s) in edge", retrievedScheduledJobId))
							} else {
								info("Scheduled job not found in edge yet...")
								close(quit)
							}

						} else {
							handleErrResponse(bodyStr)
						}
					} else {
						fatal(fmt.Errorf("timeout! Failed to find the scheduled job in edge. Looked for ID=%s", SCHEDULED_CORE_JOB_ID))
					}

					if time.Now().After(scheduleTimeout) {
						fatal(fmt.Errorf("failed to find the scheduled job in edge. Looked for ID=%s but found %s", SCHEDULED_CORE_JOB_ID, retrievedScheduledJobId))
						close(quit)
					}
				}

			case <-quit:
				{
					ticker.Stop()
					scheduledJobWg.Done()
					return
				}
			}
		}
	}()
}

func queryCoreScheduledJobInstance() {
	scheduleTimeout := time.Now().Add(QUERY_JOB_TIMEOUT)
	pollInterval := time.Minute

	ticker := time.NewTicker(pollInterval)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				{
					queryCoreScheduledJobResponse := fetch(BASE_API_URL, map[string]string{
						"query": fmt.Sprintf(`query { scheduledJob(id: %s) { jobs { count records { id status } }}}`, SCHEDULED_CORE_JOB_ID),
					})

					// info(fmt.Sprint("[DEBUG] Query Core Scheduled job response:", queryCoreScheduledJobResponse))

					var queryCoreScheduledJobResponseJson types.QueryCoreScheduledJobResponse
					err := json.Unmarshal([]byte(queryCoreScheduledJobResponse), &queryCoreScheduledJobResponseJson)
					if err == nil && queryCoreScheduledJobResponseJson.Data.ScheduledJob.Jobs.Count > 0 {
						info("Scheduled job creation validated...")
						close(quit)
					} else {
						handleErrResponse(queryCoreScheduledJobResponse)
					}

					if time.Now().After(scheduleTimeout) {
						fatal(fmt.Errorf("no instances of the scheduled job %s were created", SCHEDULED_CORE_JOB_ID))
						close(quit)
					}
				}

			case <-quit:
				{
					ticker.Stop()
					scheduledJobWg.Done()
					return
				}
			}
		}
	}()
}

// This function is called from fatal to clean up jobs before exiting so don't call fatal from here as well
func deleteScheduledCoreJob() {
	deleteScheduledCoreJobResponse := fetch(BASE_API_URL, map[string]string{
		"query": `mutation deleteScheduledJob($scheduledJobId: ID!){ 
			deleteScheduledJob(id: "%s") { id message }}`,
	})

	// info(fmt.Sprint("[DEBUG] Delete Scheduled Core job response:", deleteScheduledCoreJobResponse))

	var deleteScheduledCoreJobResponseJson types.APIDeleteScheduledCoreJobResponse
	err := json.Unmarshal([]byte(deleteScheduledCoreJobResponse), &deleteScheduledCoreJobResponseJson)
	if err == nil && deleteScheduledCoreJobResponseJson.Data.DeleteScheduledJob.Id != "" {
		deleteScheduledJobId := deleteScheduledCoreJobResponseJson.Data.DeleteScheduledJob.Id
		if deleteScheduledJobId == SCHEDULED_CORE_JOB_ID {
			info(fmt.Sprint("Deleted scheduled core job id:", deleteScheduledJobId))
		} else {
			panic(fmt.Errorf("failed to delete scheduled core job: %s", SCHEDULED_CORE_JOB_ID))
		}
	} else {
		handleErrResponseCore(deleteScheduledCoreJobResponse, func(err error) { panic(err) })
	}
}

//
// -- JOB TEMPLATES --
//
func createScheduledJobTemplate() {
	scheduledJobTemplateResponse := fetch(BASE_API_URL, map[string]string{
		"query": `
		mutation createJobTemplate($clusterId: ID!) {
			createJobTemplate(
				input: {
					clusterId: $clusterId
					applicationId: "ed075985-bc94-406b-8639-44d1da42c3fb"
					jobConfig: {
						createTDOInput: { details: { tags: "aiwareV3-70927-interval" } }
						maxTDODuraion: 5
						sourceData: { sourceId: 70927 }
					}
					taskTemplates: [
					{
						# Chunk engine
						engineId: "8bdb0e3b-ff28-4f6e-a3ba-887bd06e6440"
						payload: {
							url: "https://vtn-test-files.s3.amazonaws.com/text/txt/tardis.txt"
							ffmpegTemplate: "rawchunk"
						}
						executionPreferences: {
							priority: -92
							parentCompleteBeforeStarting: true
						}
						ioFolders: [{ referenceId: "si-out", mode: chunk, type: output }]
					}
					{
						# Amazon translate
						engineId: "1fc4d3d4-54ab-42d1-882c-cfc9df42f386"
						payload: { target: "de" }
						executionPreferences: {
							priority: -92
							parentCompleteBeforeStarting: true
						}
						ioFolders: [
						{ referenceId: "engine-in", mode: chunk, type: input }
						{ referenceId: "engine-out", mode: chunk, type: output }
						]
					}
					{
						# output writer
						engineId: "8eccf9cc-6b6d-4d7d-8cb3-7ebf4950c5f3"
						executionPreferences: {
							priority: -92
							parentCompleteBeforeStarting: true
						}
						ioFolders: [{ referenceId: "ow-in", mode: chunk, type: input }]
					}
					]
					##Routes
					routes: [
					{
						## chunkAudio --> translation
						parentIoFolderReferenceId: "si-out"
						childIoFolderReferenceId: "engine-in"
						options: {}
					}
					{
						## sampleChunkOutputFolderA  --> ow1
						parentIoFolderReferenceId: "engine-out"
						childIoFolderReferenceId: "ow-in"
						options: {}
					}
					]
				}
			) {
				id
			}
		}`,
		"variables": fmt.Sprintf(`{
			"clusterId": "%s"
		}`, VALIDATE_CLUSTER_ID),
	})

	// info(fmt.Sprint("[DEBUG] Create Job Template response:", scheduledJobTemplateResponse))

	var scheduledJobTemplateResponseJson types.APIScheduledJobTemplateResponse
	err := json.Unmarshal([]byte(scheduledJobTemplateResponse), &scheduledJobTemplateResponseJson)
	if err == nil && scheduledJobTemplateResponseJson.Data.CreateJobTemplate.Id != "" {
		SCHEDULED_JOB_TEMPLATE_ID = scheduledJobTemplateResponseJson.Data.CreateJobTemplate.Id
		info(fmt.Sprint("Created scheduled job template:", SCHEDULED_JOB_TEMPLATE_ID))
	} else {
		handleErrResponse(scheduledJobTemplateResponse)
	}
}

//
// -- RECHECK STATUS --
//
// This function is used to work around known issues with long running/pending jobs
func recheckJobStatus() {

}

//
// -- TESTS ---
//
func testScheduledJobs() {
	info("testing scheduled job creation...")

	// Create Template
	createScheduledJobTemplate()

	// Create a scheduled job using core
	createScheduledCoreJob()

	scheduledJobWg.Add(2)
	// Validate that the job has arrived in edge
	queryEdgeScheduledJob()

	// Query core to validate that edge has created job instances and poll until one is marked as completed
	queryCoreScheduledJobInstance()
	scheduledJobWg.Wait()
	// delete the scheduled job
	deleteScheduledCoreJob()

	info("scheduled job validated successfully")
	wg.Done()
}

func testAdhocCoreJob() {
	info("testing adhoc core job creation...")

	// Create an adhoc job through core
	createAdhocCoreJob()

	// Validate job has completed
	adhocCoreJobWg.Add(1)
	queryAdhocCoreJob()
	adhocCoreJobWg.Wait()

	info("adhoc core job creation validated successfully")
	wg.Done()
}

func testAdhocEdgeJob() {
	info("testing adhoc edge job creation...")

	// Create an adhoc job through core
	// createAdhocEdgeJob()

	// // Validate job has completed
	// queryAdhocEdgeJob()

	// // Validate the job had an output chunk
	// validateEdgeJobOutput()

	info("adhoc edge job creation validated successfully")
}

//
// --- Exercise 2 ---
//

func createDataRegistry() {
	createDataRegistryResponse := fetch(BASE_API_URL, map[string]string{
		"query": `
		mutation createDataRegistry {
			createDataRegistry(input: {
				name: "Vehicle"
				description: "RMS metadata pertaining to a Vehicle record type."
				source: "field deprecated"
			}) 
			{
				id
			}
		}
		`,
	})

	info(fmt.Sprint("[DEBUG]", "createDataRegistryResponse:", createDataRegistryResponse))

	var createDataRegistryResponseJson types.APICreateDataRegistryResponse
	err := json.Unmarshal([]byte(createDataRegistryResponse), &createDataRegistryResponseJson)
	if err == nil && createDataRegistryResponseJson.Data.CreateDataRegistry.Id != "" {
		DATA_REGISTRY_ID = createDataRegistryResponseJson.Data.CreateDataRegistry.Id
		info(fmt.Sprint("Created data registry id:", DATA_REGISTRY_ID))
	} else {
		handleErrResponse(createDataRegistryResponse)
	}
}

func createSchemaDraft() {
	/* Example jsonData output from a transcription service (to base the schema def on)
		{
	            "sourceEngineId": "a8c4a9f4-d6c4-4524-ab98-c0de560abf9b",
	            "taskId": "22031117_3SSTa2DhV4RX43A",
	            "internalTaskId": "67053cbc-a944-49b1-8327-319d3b9f7e2d",
	            "generatedDateUTC": "2022-03-17T19:35:03.302770354Z",
	            "series": [
	              {
	                "startTimeMs": 900,
	                "stopTimeMs": 1500,
	                "words": [
	                  {
	                    "word": "She",
	                    "confidence": 1,
	                    "bestPath": true,
	                    "utteranceLength": 1
	                  }
	                ],
	                "language": "en"
	              },
	              {
	                "startTimeMs": 1680,
	                "stopTimeMs": 2400,
	                "words": [
	                  {
	                    "word": "sells",
	                    "confidence": 1,
	                    "bestPath": true,
	                    "utteranceLength": 1
	                  }
	                ],
	                "language": "en"
	              },
	              {
	                "startTimeMs": 2970,
	                "stopTimeMs": 4110,
	                "words": [
	                  {
	                    "word": "seashells",
	                    "confidence": 1,
	                    "bestPath": true,
	                    "utteranceLength": 1
	                  }
	                ],
	                "language": "en"
	              },
	              {
	                "startTimeMs": 4440,
	                "stopTimeMs": 4950,
	                "words": [
	                  {
	                    "word": "by",
	                    "confidence": 1,
	                    "bestPath": true,
	                    "utteranceLength": 1
	                  }
	                ],
	                "language": "en"
	              },
	              {
	                "startTimeMs": 4980,
	                "stopTimeMs": 5190,
	                "words": [
	                  {
	                    "word": "the",
	                    "confidence": 1,
	                    "bestPath": true,
	                    "utteranceLength": 1
	                  }
	                ],
	                "language": "en"
	              },
	              {
	                "startTimeMs": 5190,
	                "stopTimeMs": 5910,
	                "words": [
	                  {
	                    "word": "seashore",
	                    "confidence": 1,
	                    "bestPath": true,
	                    "utteranceLength": 1
	                  }
	                ],
	                "language": "en"
	              },
	              {
	                "startTimeMs": 5910,
	                "stopTimeMs": 5910,
	                "words": [
	                  {
	                    "word": ".",
	                    "confidence": 1,
	                    "bestPath": true,
	                    "utteranceLength": 1
	                  }
	                ],
	                "language": "en"
	              }
	            ],
	            "modifiedDateTime": 1647545775000
	          }
	*/

	createSchemaDraftResponse := fetch(BASE_API_URL, map[string]string{
		"query": `
			# Note: Use the 'dataRegistryId' value from the 'createDataRegistry' mutation.
			mutation createSchemaDraft($dataRegistryId: ID!) {
			upsertSchemaDraft(input: {
				dataRegistryId: $dataRegistryId
				majorVersion: 1
				schema: {
				type: "object",
				title: "Vehicle",
				required: [
					"caseId",
					"vehicleId"
				],
				properties: {
					caseId: {
					type: "string"
					},
					vehicleId: {
					type: "string"
					},
					licensePlateNumber: {
					type: "string"
					},
					stateOfIssue: {
					type: "string"
					},
					vehicleYear: {
					type: "string"
					},
					vehicleMake: {
					type: "string"
					},
					vehicleModel: {
					type: "string"
					},
					vehicleStyle: {
					type: "string"
					},
					vehicleColor: {
					type: "string"
					}
				},
				description: "RMS metadata pertaining to a Vehicle record type."      
				}
			}) {
				id
				majorVersion
				minorVersion
				status
				definition
			}
			}
		`,
		"variables": fmt.Sprintf(`{
			"dataRegistryId": "%s"
		}`, DATA_REGISTRY_ID),
	})

	info(fmt.Sprint("[DEBUG]", "createSchemaDraftResponse:", createSchemaDraftResponse))

	var createSchemaDraftResponseJson types.APICreateSchemaDraftResponse
	err := json.Unmarshal([]byte(createSchemaDraftResponse), &createSchemaDraftResponseJson)
	if err == nil && createSchemaDraftResponseJson.Data.UpsertSchemaDraft.Id != "" {
		SCHEMA_DRAFT_ID = createSchemaDraftResponseJson.Data.UpsertSchemaDraft.Id
		info(fmt.Sprint("Created schema draft id:", SCHEMA_DRAFT_ID))
	} else {
		handleErrResponse(createSchemaDraftResponse)
	}
}

func publishSchema() {
	publishSchemaResponse := fetch(BASE_API_URL, map[string]string{
		"query": `
			# Note: Pass in the Schema ID for the 'id' value
			mutation publishSchemaDraft($schemaDraftId: ID!) {
			updateSchemaState(input: {
				id: $schemaDraftId
				status: published
			}) {
				id
				majorVersion
				minorVersion
				status
			}
			}
		`,
		"variables": fmt.Sprintf(`{
			"schemaDraftId": "%s"
		}`, SCHEMA_DRAFT_ID),
	})

	info(fmt.Sprint("[DEBUG]", "publishSchemaResponse:", publishSchemaResponse))

	var publishSchemaResponseJson types.APIPublishSchemaResponse
	err := json.Unmarshal([]byte(publishSchemaResponse), &publishSchemaResponseJson)
	if err == nil && publishSchemaResponseJson.Data.UpdateSchemaState.Id != "" {
		PUBLISHED_SCHEMA_ID = publishSchemaResponseJson.Data.UpdateSchemaState.Id
		info(fmt.Sprint("Published schema id:", PUBLISHED_SCHEMA_ID))
	} else {
		handleErrResponse(publishSchemaResponse)
	}
}

func getRootFolders() {
	getRootFoldersResponse := fetch(BASE_API_URL, map[string]string{
		"query": `
		  query listRootFolders {
			rootFolders {
			  id
			  rootFolderTypeId
			  name
			  subfolders {
				id
				name
				childTDOs {
				  count
				  records {
					id
				  }
				}
			  }
			  childTDOs {
				count
				records {
				  id
				}
			  }
			}
		  }
		`,
	})

	info(fmt.Sprint("[DEBUG]", "getRootFoldersResponse:", getRootFoldersResponse))

	var getRootFoldersResponseJson types.APIGetRootFoldersResponse
	err := json.Unmarshal([]byte(getRootFoldersResponse), &getRootFoldersResponseJson)
	if err == nil {
		if len(getRootFoldersResponseJson.Data.RootFolders) > 0 {
			ROOT_FOLDER_ID = getRootFoldersResponseJson.Data.RootFolders[0].Id
			ROOT_FOLDER_TYPE_ID = getRootFoldersResponseJson.Data.RootFolders[0].RootFolderTypeId
			info(fmt.Sprint("Got root folder id:", ROOT_FOLDER_ID))
		} else {
			ROOT_FOLDER_ID = ""
			info("No root folders found!")
		}
	} else {
		handleErrResponse(getRootFoldersResponse)
	}
}

func createTDOFolder() {
	createTDOFolderResponse := fetch(BASE_API_URL, map[string]string{
		"query": `
		  mutation createFolder($parentId: ID!, $rootFolderType: RootFolderType) {
			createFolder(input: {
			  name: "Case 1"
			  description: ""
			  rootFolderType: $rootFolderType
			  parentId: $parentId
			}) {
			  id
			  name
			}
		  }
		`,
		"variables": fmt.Sprintf(`{
			"parentId": "%s",
			"rootFolderType": "%s"
		}`, ROOT_FOLDER_ID, "collection"),
	})

	info(fmt.Sprint("[DEBUG]", "createTDOFolderResponse:", createTDOFolderResponse))

	var createTDOFolderResponseJson types.APICreateTDOFolderResponse
	err := json.Unmarshal([]byte(createTDOFolderResponse), &createTDOFolderResponseJson)
	if err == nil && createTDOFolderResponseJson.Data.CreateFolder.Id != "" {
		TDO_FOLDER_ID = createTDOFolderResponseJson.Data.CreateFolder.Id
		info(fmt.Sprint("TDO folder id:", TDO_FOLDER_ID))
	} else {
		handleErrResponse(createTDOFolderResponse)
	}
}

func moveTDO() {
	info(fmt.Sprint("[DEBUG]", "moveTDO response:", fetch(BASE_API_URL, map[string]string{
		"query": `
			# Note: Use when a TDO is currently filed in a non-root folder and needs to be moved to another folder.
			mutation moveTdo($tdoId: ID!, $folderId: ID!) {
			fileTemporalDataObject(input: {
				tdoId: $tdoId
				folderId: $folderId
			}) {
				id
			}
			}
		`,
		"variables": fmt.Sprintf(`{
			"tdoId": "%s",
			"folderId": "%s"
		}`, ADHOC_CORE_TARGET_ID, TDO_FOLDER_ID),
	})))
}

func addContentTemplateAssetToTDO() {
	info(fmt.Sprint("[DEBUG]", "addContentTemplateAssetToTDO response:", fetch(BASE_API_URL, map[string]string{
		"query": `
		  mutation updateTdoWithSdo($tdoId: ID!, $schemaId: ID!) {
			updateTDO(
			  input: {
				id: $tdoId
				contentTemplates: [
				  {
					schemaId: $schemaId
					data: {
					  caseId: "521023"
					  vehicleId: "412591"
					  licensePlateNumber: "6XDN071"
					  stateOfIssue: "CA"
					  vehicleYear: "2012"
					  vehicleMake: "Chevrolet"
					  vehicleModel: "Tahoe"
					  vehicleStyle: "CARRY-ALL (e.g. BLAZER,JEEP,BRONCO)"
					  vehicleColor: "WHI/"
					}
				  }
				]
			  }
			)
			{
			  id
			  status
			}
		  }
		`,
		"variables": fmt.Sprintf(`{
			"tdoId": "%s",
			"schemaId": "%s"
		}`, ADHOC_CORE_TARGET_ID, PUBLISHED_SCHEMA_ID),
	})))

	info(fmt.Sprint("[DEBUG]", "Added content to tdo:", ADHOC_CORE_TARGET_ID, "contentID:", PUBLISHED_SCHEMA_ID))
}

func searchContentTemplate() {
	searchContentTemplateResponse := fetch(BASE_API_URL, map[string]string{
		"query": `
		query myTDO($tdoId: ID!){
			temporalDataObject(id:$tdoId){
			  name
			  id
			  organizationId
			  startDateTime
			  stopDateTime
			  assets(assetType: "content-template") {
				records {
				  id
				  assetType
				  contentType
				  jsondata
				  jsonstring
				  signedUri
				  details
				}
			  }
			  folders {
				id
				folderPath {
				  id
				}
			  }
			  jobs {
				records{
				  id
				  sourceAssetId
				  scheduledJobId
				  targetId
				  name
				  description
				  clusterId
				  createdDateTime
				  status
				}
			  }
			}
		  }
		`,
		"variables": fmt.Sprintf(`{
			"tdoId": "%s"
		}`, ADHOC_CORE_TARGET_ID),
	})

	info(fmt.Sprint("[DEBUG]", "searchContentTemplateResponse:", searchContentTemplateResponse))

	var searchContentTemplateResponseJson types.APISearchContentTemplateResponse
	err := json.Unmarshal([]byte(searchContentTemplateResponse), &searchContentTemplateResponseJson)
	if err == nil && len(searchContentTemplateResponseJson.Data.TemporalDataObject.Assets.Records) > 0 {
		info(fmt.Sprint("Vehicle SDO signed URI: ", searchContentTemplateResponseJson.Data.TemporalDataObject.Assets.Records[0].SignedUri))
	} else {
		handleErrResponse(searchContentTemplateResponse)
	}
}

//
// --- MAIN ---
//
func main() {

	// setup interupt handler for ctrl-c ect
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for sig := range c {
			if sig == syscall.SIGINT {
				cleanupOnInterrupt()
			}
		}
	}()

	// get comand arguments

	username := flag.String("u", "", "user account with api access.")
	password := flag.String("p", "", "password for user account.")

	flag.Parse()

	argsLen := len(flag.Args())
	if argsLen > 0 {
		env = flag.Args()[0]
	}
	if argsLen > 1 {
		controller_url = flag.Args()[1]
	}
	if argsLen > 2 {
		base_api_url = flag.Args()[2]
	}

	info(fmt.Sprint("Starting deployment validation for Environment=", env))
	validateEnv()

	// uncomment for local use
	setToken(*username, *password)

	wg.Add(1)
	go func() {
		testAdhocCoreJob()
	}()
	//testAdhocEdgeJob()
	// go func() {
	// 	testScheduledJobs()
	// }()

	wg.Wait()

	createDataRegistry()
	createSchemaDraft()
	publishSchema()
	getRootFolders()
	createTDOFolder()
	moveTDO()
	addContentTemplateAssetToTDO()
	searchContentTemplate()

	info("deployment validated successfully, exiting")
	os.Exit(0)
}
