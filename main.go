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
var ADHOC_CORE_JOB_ID string
var TOKEN string

//
// --- GENERAL FUNCTIONS ---
//

func fatal(err error) {
	fmt.Println(time.Now().Format(time.StampMicro), "[ERROR]", err.Error())
	deleteScheduledCoreJob()
	os.Exit(1)
}

func info(msg string) {
	fmt.Println(time.Now().Format(time.StampMicro), "[INFO]", msg)
}

func cleanupOnInterrupt() {
	deleteScheduledCoreJob()
}

func fetch(resource string, requestBody map[string]string) (responseBody string) {
	client := &http.Client{}
	postBody, _ := json.Marshal(requestBody)
	info(fmt.Sprint("[DEBUG]", "Post Body:", string(postBody)))
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

func handleErrResponse(errResponse string) {
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
		fatal(errors.New(fmt.Sprint("returned from api.", errsMsg)))
	}
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
	fmt.Println("noop")
}

//
// --- ADHOC CORE JOB FUNCTIONS ---
//
func createAdhocCoreJob() {
	// Set start time to two minutes in the past and stop time to 15 minutes in the future
	startTime := time.Now().Add(time.Minute * -2).Unix()
	stopTime := time.Now().Add(time.Minute * 15).Unix()
	// TODO: graphql call to create adhoc job
	adhocCoreJobResponse := fetch(BASE_API_URL, map[string]string{
		"query": fmt.Sprintf(`
			mutation createWSAJobV3JobDAGForNewTDO{ 
				createJob(
					input: { 
						clusterId: %q 
						organizationId: 7682 
						target: { 
							startDateTime:%v 
							stopDateTime:%v 
						} 
						tasks: [ 
							{ 
								engineId: "9e611ad7-2d3b-48f6-a51b-0a1ba40fe255" 
								payload: { 
									url:"https://s3.amazonaws.com/src-veritone-tests/stage/20190505/0_40_Eric%%20Knox%%20BWC%%20Video_40secs.mp4" 
								} 
								ioFolders: [ 
									{ 
										referenceId: "wsaOutputFolder" 
										mode: stream 
										type: output 
									}
								] 
								executionPreferences: { 
									priority: -150
								} 
							} 
							{ 
								engineId: "352556c7-de07-4d55-b33f-74b1cf237f25" 
								ioFolders: [ 
									{ 
										referenceId: "playbackInputFolder" 
										mode: stream 
										type: input 
									} 
								] 
								executionPreferences: { 
									parentCompleteBeforeStarting: true, 
									priority: -150 
								} 
							} 
							{ 
								engineId: "8bdb0e3b-ff28-4f6e-a3ba-887bd06e6440" 
								payload:{ 
									ffmpegTemplate: "audio" 
									customFFMPEGProperties:{ 
										chunkSizeInSeconds: "20" 
									} 
								} 
								ioFolders: [ 
									{ 
										referenceId: "chunkAudioInputFolder" 
										mode: stream 
										type: input 
									}, 
									{ 
										referenceId: "chunkAudioOutputFolder" 
										mode: chunk 
										type: output 
									} 
								], 
								executionPreferences: { 
									parentCompleteBeforeStarting: true, 
									priority: -150 
								} 
							} 
							{ 
								engineId: "c0e55cde-340b-44d7-bb42-2e0d65e98255" 
								ioFolders: [ 
									{ 
										referenceId: "transcriptionInputFolder" 
										mode: chunk 
										type: input 
									}, 
									{ 
										referenceId: "transcriptionOutputFolder" 
										mode: chunk 
										type: output 
									} 
								], 
								executionPreferences: { 
									priority: -150
								} 
							} 
							{ 
								engineId: "8eccf9cc-6b6d-4d7d-8cb3-7ebf4950c5f3" 
								ioFolders: [ 
									{ 
										referenceId: "owInputFolderFromTranscription" 
										mode: chunk 
										type: input 
									} 
								] 
								executionPreferences: { 
									parentCompleteBeforeStarting: true, 
									priority: -150
								}  
							} 
						] 
						routes: [
							{
								parentIoFolderReferenceId: "wsaOutputFolder"
								childIoFolderReferenceId: "playbackInputFolder"
								options: {
								}
							},
							{
								parentIoFolderReferenceId: "wsaOutputFolder"
								childIoFolderReferenceId: "chunkAudioInputFolder"
								options: {
								}
							}
							{
								parentIoFolderReferenceId: "chunkAudioOutputFolder"
								childIoFolderReferenceId: "transcriptionInputFolder"
								options: {
								}
							}
							{
								parentIoFolderReferenceId: "transcriptionOutputFolder"
								childIoFolderReferenceId: "owInputFolderFromTranscription"
								options: {
								}
							}
						]
					}
				) 
				{
					id 
				}
			}`,
			VALIDATE_CLUSTER_ID,
			startTime,
			stopTime,
		),
		"operationName": "createWSAJobV3JobDAGForNewTDO",
	})

	info(fmt.Sprint("[DEBUG] Core job response:", adhocCoreJobResponse))

	var adhocCoreJobResponseJson types.APIAdhocCoreJobResponse
	err := json.Unmarshal([]byte(adhocCoreJobResponse), &adhocCoreJobResponseJson)
	if err == nil && adhocCoreJobResponseJson.Data.CreateJob.Id != "" {
		ADHOC_CORE_JOB_ID = adhocCoreJobResponseJson.Data.CreateJob.Id
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
				// TODO: graphql request to query adhoc core job.
				if time.Now().After(coreTimeout) {
					close(quit)
					fatal(
						errors.New(
							fmt.Sprint("Core Jobs still pending after", coreTimeout, ", failing environment validation for Environment=", env),
						),
					)
				}
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()
}

//
// --- ADHOC EDGE JOB FUNCTIONS ---
//
func createAdhocEdgeJob() {

}

func queryAdhocEdgeJob() {

}

func validateEdgeJobOutput() {

}

//
// --- SCHEDULED CORE/EDGE JOB FUNCTIONS ---
//
func createScheduledCoreJob() {

}

func queryEdgeScheduledJob() {

}

func queryCoreScheduledJobInstance() {

}

// This function is called from fatal to clean up jobs before exiting so don't call fatal from here as well
func deleteScheduledCoreJob() {

}

//
// -- JOB TEMPLATES --
//
func createScheduledJobTemplate() {

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

	// Validate that the job has arrived in edge
	queryEdgeScheduledJob()

	// Query core to validate that edge has created job instances and poll until one is marked as completed
	queryCoreScheduledJobInstance()

	// delete the scheduled job
	deleteScheduledCoreJob()

	info("scheduled job validated successfully")
}

func testAdhocCoreJob() {
	info("testing adhoc core job creation...")

	// Create an adhoc job through core
	createAdhocCoreJob()

	// Validate job has completed
	queryAdhocCoreJob()

	info("adhoc core job creation validated successfully")
}

func testAdhocEdgeJob() {
	info("testing adhoc edge job creation...")

	// Create an adhoc job through core
	createAdhocEdgeJob()

	// Validate job has completed
	queryAdhocEdgeJob()

	// Validate the job had an output chunk
	validateEdgeJobOutput()

	info("adhoc edge job creation validated successfully")
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

	testAdhocCoreJob()
	//testAdhocEdgeJob()
	//testScheduledJobs()

	info("deployment validated successfully, exiting")
	os.Exit(0)
}
