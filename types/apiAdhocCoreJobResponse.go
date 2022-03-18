package types

type ResponseAdhocCoreJobCreateJob struct {
	Id       string `json:"id"`
	TargetId string `json:"targetId"`
}

type ResponseAdhocCoreJobData struct {
	CreateJob ResponseAdhocCoreJobCreateJob `json:"createJob"`
}

type APIAdhocCoreJobResponse struct {
	Data ResponseAdhocCoreJobData `json:"data"`
}

type QueryAdhocCoreJobResponse struct {
	Data struct {
		Job struct {
			Id              string                 `json:"id"`
			TargetId        string                 `json:"targetId"`
			ClusterId       string                 `json:"clusterId"`
			Status          string                 `json:"status"`
			CreatedDateTime string                 `json:"createdDateTime"`
			Tasks           map[string]interface{} `json:"tasks"`
		} `json:"job"`
	} `json:"data"`
}
