package types

type ResponseAdhocCoreJobCreateJob struct {
	Id string `json:"id"`
}

type ResponseAdhocCoreJobData struct {
	CreateJob ResponseAdhocCoreJobCreateJob `json:"createJob"`
}

type APIAdhocCoreJobResponse struct {
	Data ResponseAdhocCoreJobData `json:"data"`
}
