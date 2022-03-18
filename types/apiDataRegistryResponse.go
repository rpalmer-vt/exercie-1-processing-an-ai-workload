package types

type APICreateDataRegistryResponse struct {
	Data struct {
		CreateDataRegistry struct {
			Id string `json:"id"`
		} `json:"createDataRegistry"`
	} `json:"data"`
}

type APICreateSchemaDraftResponse struct {
	Data struct {
		UpsertSchemaDraft struct {
			Id string `json:"id"`
		} `json:"upsertSchemaDraft"`
	} `json:"data"`
}

type APIPublishSchemaResponse struct {
	Data struct {
		UpdateSchemaState struct {
			Id string `json:"id"`
		} `json:"updateSchemaState"`
	} `json:"data"`
}
