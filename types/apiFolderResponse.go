package types

type APIGetRootFoldersResponse struct {
	Data struct {
		RootFolders []struct {
			Id               string `json:"id"`
			RootFolderTypeId int    `json:"rootFolderTypeId"`
		} `json:"rootFolders"`
	} `json:"data"`
}

type APICreateTDOFolderResponse struct {
	Data struct {
		CreateFolder struct {
			Id string `json:"id"`
		} `json:"createFolder"`
	} `json:"data"`
}
