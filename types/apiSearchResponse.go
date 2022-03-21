package types

type APISearchContentTemplateResponse struct {
	Data struct {
		TemporalDataObject struct {
			Assets struct {
				Records []struct {
					Id        string `json:"id"`
					SignedUri string `json:"signedUri"`
				} `json:"records"`
			} `json:"assets"`
		} `json:"temporalDataObject"`
	} `json:"data"`
}
