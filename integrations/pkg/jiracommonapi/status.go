package jiracommonapi

type StatusDetail struct {
	Name           string `json:"name"`
	StatusCategory struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"statusCategory"`
	ID          string `json:"id"`
	Description string `json:"description"`
	IconURL     string `json:"iconUrl"`
}

func StatusWithDetail(qc QueryContext) (_ []StatusDetail, names []string, _ error) {
	objectPath := "status"

	var detail []StatusDetail
	err := qc.Req.Get(objectPath, nil, &detail)
	if err != nil {
		return nil, nil, err
	}

	// we dedup names, but not the []StatusDetail array
	m := map[string]bool{}
	for _, status := range detail {
		m[status.Name] = true
	}
	var res []string
	for k := range m {
		res = append(res, k)
	}

	return detail, res, nil
}
