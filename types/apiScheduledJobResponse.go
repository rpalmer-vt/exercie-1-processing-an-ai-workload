package types

type APIScheduledJobTemplateResponse struct {
	Data struct {
		CreateJobTemplate struct {
			Id string `json:"id"`
		} `json:"createJobTemplate"`
	} `json:"data"`
}

type APIScheduledCoreJobResponse struct {
	Data struct {
		CreateScheduledJob struct {
			Id string `json:"id"`
		} `json:"createScheduledJob"`
	} `json:"data"`
}

type QueryCoreScheduledJobResponse struct {
	Data struct {
		ScheduledJob struct {
			Jobs struct {
				Count int `json:"count"`
			} `json:"jobs"`
		} `json:"scheduledJob"`
	} `json:"data"`
}

type QueryEdgeScheduledJobResponseResult struct {
	ScheduledJobName string `json:"scheduledJobName"`
	ScheduledJobId   string `json:"scheduledJobId"`
}
type QueryEdgeScheduledJobResponse struct {
	Result []QueryEdgeScheduledJobResponseResult `json:"result"`
}

type APIDeleteScheduledCoreJobResponse struct {
	Data struct {
		DeleteScheduledJob struct {
			Id string `json:"id"`
		} `json:"deleteScheduledJob"`
	} `json:"data"`
}
