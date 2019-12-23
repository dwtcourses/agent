package jiracommonapi

func ServerVersion(qc QueryContext) (serverVersion string, err error) {

	objectPath := "serverInfo"

	var serverInfo struct {
		Version string `json:"version"`
	}

	err = qc.Request(objectPath, nil, &serverInfo)
	if err != nil {
		return
	}

	serverVersion = serverInfo.Version

	return
}
